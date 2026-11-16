#include "gateway.h"

#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <WiFiClientSecure.h>
#include <NimBLEDevice.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>

#include "gateway_config.h"
#include "packet.h"

namespace mesh::gateway {
namespace {

constexpr char kServiceUuid[] = "7b2f0001-5a56-4d61-7661-4d6176610001";
constexpr char kRxUuid[]      = "7b2f0002-5a56-4d61-7661-4d6176610001";
constexpr char kTxUuid[]      = "7b2f0003-5a56-4d61-7661-4d6176610001";

constexpr uint32_t kWifiRetryMs = 10000;

struct RelayItem {
  uint16_t len;
  uint8_t data[Gateway::kMaxPacket];
};

Gateway* g_gateway = nullptr;
NimBLECharacteristic* g_tx = nullptr;

class RxCallbacks final : public NimBLECharacteristicCallbacks {
 public:
  void onWrite(NimBLECharacteristic* c, NimBLEConnInfo&) override {
    std::string v = c->getValue();
    if (!g_gateway || v.empty()) return;
    g_gateway->push_ble(reinterpret_cast<const uint8_t*>(v.data()), v.size());
  }
};

class ServerCallbacks final : public NimBLEServerCallbacks {
 public:
  void onConnect(NimBLEServer*, NimBLEConnInfo&) override {}
  void onDisconnect(NimBLEServer*, NimBLEConnInfo&, int) override {
    NimBLEDevice::startAdvertising();
  }
};

RxCallbacks g_rx_callbacks;
ServerCallbacks g_server_callbacks;

static bool relay_url_valid() {
  return MAVA_RELAY_URL[0] != '\0';
}

static bool is_https(const char* url) {
  return strncmp(url, "https://", 8) == 0;
}

static size_t frame_packet(const uint8_t* raw, size_t n, uint8_t* out, size_t cap) {
  if (n > mesh::kMaxSize || cap < n + 4) return 0;
  out[0] = uint8_t(n >> 24);
  out[1] = uint8_t(n >> 16);
  out[2] = uint8_t(n >> 8);
  out[3] = uint8_t(n);
  memcpy(out + 4, raw, n);
  return n + 4;
}

}  // namespace

bool Gateway::begin(node::Node& node) {
#if !defined(ESP32)
  (void)node;
  return false;
#else
  node_ = &node;
  g_gateway = this;

  // BLE uses NimBLE because it is the lighter BLE-only host on ESP32-S3.
  NimBLEDevice::init(MAVA_BLE_NAME);
  NimBLEServer* server = NimBLEDevice::createServer();
  server->setCallbacks(&g_server_callbacks);
  NimBLEService* svc = server->createService(kServiceUuid);

  NimBLECharacteristic* rx = svc->createCharacteristic(
      kRxUuid, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR);
  g_tx = svc->createCharacteristic(kTxUuid, NIMBLE_PROPERTY::NOTIFY);
  rx->setCallbacks(&g_rx_callbacks);

  svc->start();
  NimBLEAdvertising* adv = NimBLEDevice::getAdvertising();
  adv->addServiceUUID(kServiceUuid);
  adv->setName(MAVA_BLE_NAME);
  adv->start();
  ble_ready_ = true;

  // The relay worker owns Wi-Fi/HTTP so a slow Internet path never blocks
  // LoRa receive/transmit processing in the main Arduino loop.
  if (MAVA_WIFI_SSID[0] != '\0') {
    WiFi.mode(WIFI_STA);
    WiFi.begin(MAVA_WIFI_SSID, MAVA_WIFI_PASSWORD);
  }

  relay_queue_ = xQueueCreate(kQueueDepth, sizeof(RelayItem));
  if (!relay_queue_) return false;

  BaseType_t ok = xTaskCreate(
      [](void* arg) { static_cast<Gateway*>(arg)->relay_task(); },
      "mava-relay", 8192, this, 1, reinterpret_cast<TaskHandle_t*>(&relay_task_handle_));
  if (ok != pdPASS) return false;

  return true;
#endif
}

bool Gateway::push_ble(const uint8_t* data, size_t len) {
  if (!data || len == 0 || len > kMaxPacket) {
    ble_rx_dropped_++;
    return false;
  }

  uint8_t next = uint8_t((rx_head_ + 1) % kQueueDepth);
  if (next == rx_tail_) {
    ble_rx_dropped_++;
    return false;
  }

  // Validate at the transport boundary. BLE carries a raw Mava packet,
  // not a Meshtastic packet and not the one-byte LoRa link header.
  Packet p;
  if (decode(data, len, p) != Err::Ok || p.no_lora()) {
    ble_rx_dropped_++;
    return false;
  }

  memcpy(rx_[rx_head_].data, data, len);
  rx_[rx_head_].len = uint16_t(len);
  rx_head_ = next;
  return true;
}

bool Gateway::forward_lora(const Item& item, node::Millis now) {
  if (!node_) return false;
  if (!node_->send_raw(item.data, item.len, now)) return false;
  lora_forwarded_++;
  return true;
}

void Gateway::loop(node::Millis now) {
  while (rx_tail_ != rx_head_) {
    Item item = rx_[rx_tail_];
    rx_tail_ = uint8_t((rx_tail_ + 1) % kQueueDepth);

    // Fan out: the exact same encrypted Mava packet goes to LoRa and relay.
    // Neither transport gets to rewrite the Mava packet.
    forward_lora(item, now);

    if (relay_queue_) {
      RelayItem ri{};
      ri.len = item.len;
      memcpy(ri.data, item.data, item.len);
      if (xQueueSend(reinterpret_cast<QueueHandle_t>(relay_queue_), &ri, 0) == pdPASS) {
        relay_queued_++;
      } else {
        relay_failed_++;
      }
    }
  }

  if (WiFi.status() == WL_CONNECTED) wifi_ready_ = true;
  else wifi_ready_ = false;
}

void Gateway::relay_task() {
  uint32_t last_wifi_attempt = 0;

  for (;;) {
    if (MAVA_WIFI_SSID[0] != '\0' && WiFi.status() != WL_CONNECTED) {
      uint32_t now = millis();
      if (now - last_wifi_attempt >= kWifiRetryMs) {
        last_wifi_attempt = now;
        WiFi.disconnect();
        WiFi.begin(MAVA_WIFI_SSID, MAVA_WIFI_PASSWORD);
      }
    }

    RelayItem item{};
    if (xQueueReceive(reinterpret_cast<QueueHandle_t>(relay_queue_), &item,
                      pdMS_TO_TICKS(250)) != pdPASS) {
      continue;
    }

    if (!relay_url_valid() || WiFi.status() != WL_CONNECTED) {
      relay_failed_++;
      continue;
    }

    if (MAVA_RELAY_POW_BITS > 0) {
      // The production relay currently protects anonymous POST /v1/packets
      // with hashcash. This worker deliberately does not fake an auth header:
      // add an async PoW worker or use an allowlisted gateway endpoint before
      // enabling a non-zero PoW requirement.
      relay_failed_++;
      continue;
    }

    uint8_t body[Gateway::kMaxPacket + 4];
    size_t body_len = frame_packet(item.data, item.len, body, sizeof body);
    if (!body_len) {
      relay_failed_++;
      continue;
    }

    HTTPClient http;
    WiFiClientSecure secure;
    WiFiClient plain;

    bool ok = false;
    if (is_https(MAVA_RELAY_URL)) {
      // Framework default. Production builds should replace this with the
      // relay CA/certificate pin instead of accepting every certificate.
      secure.setInsecure();
      ok = http.begin(secure, String(MAVA_RELAY_URL) + "/v1/packets");
    } else {
      ok = http.begin(plain, String(MAVA_RELAY_URL) + "/v1/packets");
    }

    if (!ok) {
      relay_failed_++;
      continue;
    }

    http.addHeader("Content-Type", "application/octet-stream");
    http.addHeader("X-Origin", MAVA_RELAY_ORIGIN);
    int code = http.POST(body, body_len);
    http.end();

    if (code < 200 || code >= 300) relay_failed_++;
  }
}

}  // namespace mesh::gateway

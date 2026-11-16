#include "handheld.h"

#include <Arduino.h>
#include <NimBLEDevice.h>
#include <NimBLEServer.h>
#include <NimBLEUtils.h>

#include "packet.h"

namespace mesh::handheld {
namespace {

constexpr char kServiceUuid[] = "7b2f1001-5a56-4d61-7661-4d6176610001";
constexpr char kRxUuid[]      = "7b2f1002-5a56-4d61-7661-4d6176610001";
constexpr char kTxUuid[]      = "7b2f1003-5a56-4d61-7661-4d6176610001";

Handheld* g_self = nullptr;
NimBLECharacteristic* g_tx = nullptr;

class RxCallbacks final : public NimBLECharacteristicCallbacks {
 public:
  void onWrite(NimBLECharacteristic* c, NimBLEConnInfo&) override {
    if (!g_self) return;
    std::string v = c->getValue();
    if (!v.empty()) g_self->push_ble_to_node(reinterpret_cast<const uint8_t*>(v.data()), v.size());
  }
};

class ServerCallbacks final : public NimBLEServerCallbacks {
 public:
  void onDisconnect(NimBLEServer*, NimBLEConnInfo&, int) override {
    NimBLEDevice::startAdvertising();
  }
};

RxCallbacks g_rx;
ServerCallbacks g_server;

}  // namespace

bool Handheld::begin(node::Node& node) {
  node_ = &node;
  g_self = this;

  NimBLEDevice::init("Mava-Handheld");
  NimBLEServer* server = NimBLEDevice::createServer();
  server->setCallbacks(&g_server);

  NimBLEService* service = server->createService(kServiceUuid);
  NimBLECharacteristic* rx = service->createCharacteristic(
      kRxUuid, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR);
  g_tx = service->createCharacteristic(kTxUuid, NIMBLE_PROPERTY::NOTIFY);
  rx->setCallbacks(&g_rx);

  service->start();
  NimBLEAdvertising* adv = NimBLEDevice::getAdvertising();
  adv->addServiceUUID(kServiceUuid);
  adv->setName("Mava-Handheld");
  adv->start();

  ble_ready_ = true;
  meshtastic_.begin(node.pub());
  meshtastic_ready_ = true;
  return true;
}

bool Handheld::push_ble_to_node(const uint8_t* data, size_t len) {
  if (!data || len == 0 || len > kBlePacketMax) return false;
  uint8_t next = uint8_t((ble_head_ + 1) % 8);
  if (next == ble_tail_) return false;
  memcpy(ble_rx_[ble_head_].data, data, len);
  ble_rx_[ble_head_].len = uint16_t(len);
  ble_head_ = next;
  return true;
}

void Handheld::notify_phone(const uint8_t* data, size_t len) {
  if (!g_tx || !data || len == 0) return;
  g_tx->setValue(data, len);
  g_tx->notify();
}

void Handheld::loop(node::Millis now) {
  if (!node_) return;

  // BLE callbacks only enqueue. All node/router access stays in loop().
  while (ble_tail_ != ble_head_) {
    BleItem item = ble_rx_[ble_tail_];
    ble_tail_ = uint8_t((ble_tail_ + 1) % 8);
    node_->host_rx(item.data, item.len, now);
  }

  // Drain Mava packets/events produced by the node and expose them to the
  // phone over BLE. This makes the handheld bidirectional rather than the
  // old gateway's RX-only BLE bridge.
  uint8_t out[512];
  size_t n = node_->host_tx(out, sizeof out);
  if (n) notify_phone(out, n);

  (void)now;
}

}  // namespace mesh::handheld

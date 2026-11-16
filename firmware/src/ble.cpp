#include "ble.h"

#include <Arduino.h>
#include <NimBLEDevice.h>
#include <freertos/FreeRTOS.h>
#include <freertos/stream_buffer.h>

namespace ble {
namespace {

constexpr char kService[] = "6d657368-6c72-0000-8000-00805f9b34fb";
constexpr char kRx[] = "6d657368-6c72-0001-8000-00805f9b34fb";
constexpr char kTx[] = "6d657368-6c72-0002-8000-00805f9b34fb";
constexpr size_t kRxBuffer = 4096;
constexpr int kNotifiesPerLoop = 4;

StreamBufferHandle_t g_rx = nullptr;  // NimBLE task → main loop
NimBLECharacteristic* g_tx = nullptr;
volatile bool g_connected = false;
volatile bool g_subscribed = false;
volatile bool g_dropped = false;  // a link went away: detach the host port
volatile uint16_t g_mtu = 23;

// One notification that could not be sent yet (stack buffers full).
uint8_t g_pending[512];
size_t g_pending_len = 0;

class Server : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer* s, NimBLEConnInfo& info) override {
    g_connected = true;
    g_mtu = 23;
    // One phone at a time: the host port is a single stream.
    (void)s;
    (void)info;
    NimBLEDevice::stopAdvertising();
  }
  void onDisconnect(NimBLEServer*, NimBLEConnInfo&, int) override {
    g_connected = false;
    g_subscribed = false;
    g_dropped = true;
    NimBLEDevice::startAdvertising();
  }
  void onMTUChange(uint16_t mtu, NimBLEConnInfo&) override { g_mtu = mtu; }
  void onAuthenticationComplete(NimBLEConnInfo& info) override {
    if (!info.isEncrypted()) NimBLEDevice::getServer()->disconnect(info.getConnHandle());
  }
};

class Rx : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic* c, NimBLEConnInfo&) override {
    const NimBLEAttValue& v = c->getValue();
    if (v.size() && g_rx) xStreamBufferSend(g_rx, v.data(), v.size(), 0);  // full: dropped, COBS resyncs
  }
};

class Tx : public NimBLECharacteristicCallbacks {
  void onSubscribe(NimBLECharacteristic*, NimBLEConnInfo&, uint16_t sub) override { g_subscribed = sub != 0; }
};

Server g_server_cb;
Rx g_rx_cb;
Tx g_tx_cb;

}  // namespace

bool begin(const char* name, uint32_t passkey) {
  g_rx = xStreamBufferCreate(kRxBuffer, 1);
  if (!g_rx) return false;
  NimBLEDevice::init(name);
  NimBLEDevice::setMTU(247);
  NimBLEDevice::setSecurityAuth(true, true, true);  // bonding, MITM, secure connections
  NimBLEDevice::setSecurityPasskey(passkey);
  NimBLEDevice::setSecurityIOCap(BLE_HS_IO_DISPLAY_ONLY);
  NimBLEServer* server = NimBLEDevice::createServer();
  server->setCallbacks(&g_server_cb);
  NimBLEService* svc = server->createService(kService);
  NimBLECharacteristic* rx = svc->createCharacteristic(
      kRx, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR | NIMBLE_PROPERTY::WRITE_ENC |
               NIMBLE_PROPERTY::WRITE_AUTHEN);
  rx->setCallbacks(&g_rx_cb);
  g_tx = svc->createCharacteristic(kTx, NIMBLE_PROPERTY::NOTIFY);
  g_tx->setCallbacks(&g_tx_cb);
  server->start();
  NimBLEAdvertising* adv = NimBLEDevice::getAdvertising();
  adv->addServiceUUID(kService);
  adv->setName(name);
  adv->enableScanResponse(true);
  return adv->start();
}

bool connected() { return g_connected; }

void loop(mesh::node::Node& node, mesh::route::Millis now) {
  if (g_dropped) {
    g_dropped = false;
    g_pending_len = 0;
    xStreamBufferReset(g_rx);
    node.host_detach(kPort);
  }
  uint8_t buf[512];
  size_t n;
  while ((n = xStreamBufferReceive(g_rx, buf, sizeof buf, 0)) > 0) node.host_rx(kPort, buf, n, now);

  if (!g_connected || !g_subscribed || !node.host_attached(kPort)) return;
  size_t chunk = g_mtu > 3 ? size_t(g_mtu) - 3 : 20;
  if (chunk > sizeof g_pending) chunk = sizeof g_pending;
  for (int i = 0; i < kNotifiesPerLoop; i++) {
    if (g_pending_len == 0) g_pending_len = node.host_tx(kPort, g_pending, chunk);
    if (g_pending_len == 0) return;
    if (!g_tx->notify(g_pending, g_pending_len)) return;  // stack busy: same bytes next loop
    g_pending_len = 0;
  }
}

}  // namespace ble

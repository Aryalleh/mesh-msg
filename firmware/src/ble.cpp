#include "ble.h"

#include <Arduino.h>
#include <NimBLEDevice.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>

#include <cstring>

#include "blemesh.h"

namespace ble {
namespace {

// Host link: the apps' "LoRa node" stream (one phone at a time, host port 1).
constexpr char kService[] = "6d657368-6c72-0000-8000-00805f9b34fb";
constexpr char kRx[] = "6d657368-6c72-0001-8000-00805f9b34fb";
constexpr char kTx[] = "6d657368-6c72-0002-8000-00805f9b34fb";
// The phones' own Bluetooth mesh (android/app/.../ble/BleProto.kt): the board
// joins it like another phone.
constexpr char kMeshService[] = "6d657368-6d73-6700-8000-00805f9b34fb";
constexpr char kMeshRx[] = "6d657368-6d73-6701-8000-00805f9b34fb";
constexpr char kMeshTx[] = "6d657368-6d73-6702-8000-00805f9b34fb";
constexpr uint16_t kManufacturer = 0xFFFF;  // "no company": the advert carries no vendor identity

constexpr int kMaxConns = 3;  // NimBLE's default connection limit
constexpr int kNotifiesPerLoop = 4;
constexpr size_t kMaxWrite = 244;  // MTU 247 − 3

// NimBLE's task → main loop: everything that happens on a connection.
enum class Ev : uint8_t { Connect, Disconnect, Mtu, HostWrite, MeshWrite, HostSub, MeshSub };
struct Msg {
  Ev ev;
  uint16_t conn;
  uint16_t n;  // bytes, MTU, or subscribe value
  uint8_t d[kMaxWrite];
};
QueueHandle_t g_q = nullptr;

NimBLECharacteristic* g_tx = nullptr;
NimBLECharacteristic* g_mesh_tx = nullptr;
volatile int g_conn_count = 0;

void post(Ev ev, uint16_t conn, uint16_t n = 0, const uint8_t* d = nullptr) {
  Msg m;
  m.ev = ev;
  m.conn = conn;
  m.n = n;
  if (d && n) memcpy(m.d, d, n > kMaxWrite ? kMaxWrite : n);
  xQueueSend(g_q, &m, 0);  // full: dropped (a write is lost; the stream resyncs or the phone reconnects)
}

class Server : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer*, NimBLEConnInfo& info) override {
    g_conn_count = g_conn_count + 1;
    post(Ev::Connect, info.getConnHandle(), info.getMTU());
    // Room for more phones: keep advertising.
    if (g_conn_count < kMaxConns) NimBLEDevice::startAdvertising();
  }
  void onDisconnect(NimBLEServer*, NimBLEConnInfo& info, int) override {
    g_conn_count = g_conn_count > 0 ? g_conn_count - 1 : 0;
    post(Ev::Disconnect, info.getConnHandle());
    NimBLEDevice::startAdvertising();
  }
  void onMTUChange(uint16_t mtu, NimBLEConnInfo& info) override { post(Ev::Mtu, info.getConnHandle(), mtu); }
};

class Rx : public NimBLECharacteristicCallbacks {
 public:
  explicit Rx(Ev ev) : ev_(ev) {}
  void onWrite(NimBLECharacteristic* c, NimBLEConnInfo& info) override {
    const NimBLEAttValue& v = c->getValue();
    if (v.size()) post(ev_, info.getConnHandle(), uint16_t(v.size()), v.data());
  }

 private:
  Ev ev_;
};

class Tx : public NimBLECharacteristicCallbacks {
 public:
  explicit Tx(Ev ev) : ev_(ev) {}
  void onSubscribe(NimBLECharacteristic*, NimBLEConnInfo& info, uint16_t sub) override {
    post(ev_, info.getConnHandle(), sub);
  }

 private:
  Ev ev_;
};

Server g_server_cb;
Rx g_rx_cb(Ev::HostWrite), g_mesh_rx_cb(Ev::MeshWrite);
Tx g_tx_cb(Ev::HostSub), g_mesh_tx_cb(Ev::MeshSub);

// ---- main-loop state ----

struct Conn {
  bool used = false;
  uint16_t handle = 0;
  uint16_t mtu = 23;
  bool host_sub = false, mesh_sub = false;
  int seat = -1;  // Bluetooth mesh seat (0..2) once the phone said hello
};
Conn g_conns[kMaxConns];
int g_host_conn = -1;  // the connection using the host link (port 1)

mesh::blemesh::Peer g_peers[mesh::node::Node::kMeshPorts];
bool g_seat_used[mesh::node::Node::kMeshPorts] = {};

// Bytes that could not be notified yet (stack buffers full): host link, then one per seat.
struct Pending {
  uint8_t d[kMaxWrite];
  size_t n = 0;
};
Pending g_host_pending;
Pending g_seat_pending[mesh::node::Node::kMeshPorts];

Conn* find(uint16_t h) {
  for (auto& c : g_conns)
    if (c.used && c.handle == h) return &c;
  return nullptr;
}

void free_seat(mesh::node::Node& node, Conn& c) {
  if (c.seat < 0) return;
  g_peers[c.seat].reset();
  g_seat_pending[c.seat].n = 0;
  g_seat_used[c.seat] = false;
  node.host_detach(mesh::node::Node::kMeshPort0 + size_t(c.seat));
  c.seat = -1;
}

void drop_conn(uint16_t h) {
  if (NimBLEServer* s = NimBLEDevice::getServer()) s->disconnect(h);
}

void handle(mesh::node::Node& node, const Msg& m, mesh::route::Millis now) {
  Conn* c = find(m.conn);
  switch (m.ev) {
    case Ev::Connect:
      if (!c) {
        for (auto& x : g_conns)
          if (!x.used) {
            x = Conn();
            x.used = true;
            x.handle = m.conn;
            x.mtu = m.n ? m.n : 23;
            break;
          }
      }
      return;
    case Ev::Disconnect:
      if (!c) return;
      free_seat(node, *c);
      if (g_host_conn >= 0 && &g_conns[g_host_conn] == c) {
        g_host_conn = -1;
        g_host_pending.n = 0;
        node.host_detach(kPort);
      }
      c->used = false;
      return;
    case Ev::Mtu:
      if (c) c->mtu = m.n;
      return;
    case Ev::HostSub:
      if (c) c->host_sub = m.n != 0;
      return;
    case Ev::MeshSub:
      if (c) c->mesh_sub = m.n != 0;
      return;
    case Ev::HostWrite:
      if (!c) return;
      if (g_host_conn < 0) g_host_conn = int(c - g_conns);
      if (&g_conns[g_host_conn] != c) return;  // one host-link phone at a time
      node.host_rx(kPort, m.d, m.n, now);
      return;
    case Ev::MeshWrite: {
      if (!c) return;
      if (c->seat < 0) {
        for (int i = 0; i < int(mesh::node::Node::kMeshPorts); i++)
          if (!g_seat_used[i]) {
            g_seat_used[i] = true;
            c->seat = i;
            g_peers[i].reset();
            break;
          }
        if (c->seat < 0) return drop_conn(m.conn);
      }
      size_t port = mesh::node::Node::kMeshPort0 + size_t(c->seat);
      if (!g_peers[c->seat].rx(node, port, m.d, m.n, now)) {
        free_seat(node, *c);
        drop_conn(m.conn);  // not a mesh phone, or a corrupt stream: it reconnects
      }
      return;
    }
  }
}

// Notifies up to kNotifiesPerLoop pieces; false when the stack is busy.
template <typename Next>
void pump(NimBLECharacteristic* chr, uint16_t h, size_t chunk, Pending& p, Next next) {
  if (chunk > sizeof p.d) chunk = sizeof p.d;
  for (int i = 0; i < kNotifiesPerLoop; i++) {
    if (p.n == 0) p.n = next(p.d, chunk);
    if (p.n == 0) return;
    if (!chr->notify(p.d, p.n, h)) return;  // stack busy: same bytes next loop
    p.n = 0;
  }
}

}  // namespace

bool begin(const char* name) {
  g_q = xQueueCreate(32, sizeof(Msg));
  if (!g_q) return false;
  NimBLEDevice::init(name);
  NimBLEDevice::setMTU(247);
  // No pairing: phones connect directly (owner's choice). The host-link port
  // is therefore untrusted over Bluetooth: packets and status only; settings,
  // Wi-Fi, reboot and wipe stay on USB and the setup panel.
  NimBLEServer* server = NimBLEDevice::createServer();
  server->setCallbacks(&g_server_cb);

  NimBLEService* svc = server->createService(kService);
  NimBLECharacteristic* rx = svc->createCharacteristic(kRx, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR);
  rx->setCallbacks(&g_rx_cb);
  g_tx = svc->createCharacteristic(kTx, NIMBLE_PROPERTY::NOTIFY);
  g_tx->setCallbacks(&g_tx_cb);

  NimBLEService* mesh = server->createService(kMeshService);
  NimBLECharacteristic* mrx = mesh->createCharacteristic(kMeshRx, NIMBLE_PROPERTY::WRITE);
  mrx->setCallbacks(&g_mesh_rx_cb);
  g_mesh_tx = mesh->createCharacteristic(kMeshTx, NIMBLE_PROPERTY::NOTIFY);
  g_mesh_tx->setCallbacks(&g_mesh_tx_cb);
  server->start();

  // Advert as the phones advertise each other: their mesh service and a
  // session ID. The board's is the lowest (0), so every phone connects to it
  // (the larger ID connects). The host-link service and the name go in the
  // scan response (3 + 18 + 8 = 29 of 31 bytes in the advert).
  NimBLEAdvertisementData adv;
  adv.setFlags(BLE_HS_ADV_F_DISC_GEN | BLE_HS_ADV_F_BREDR_UNSUP);
  adv.addServiceUUID(NimBLEUUID(kMeshService));
  const uint8_t manu[] = {uint8_t(kManufacturer), uint8_t(kManufacturer >> 8), 0, 0, 0, 0};
  adv.setManufacturerData(manu, sizeof manu);
  NimBLEAdvertisementData scan;
  scan.addServiceUUID(NimBLEUUID(kService));
  scan.setName(name);
  NimBLEAdvertising* a = NimBLEDevice::getAdvertising();
  a->setAdvertisementData(adv);
  a->setScanResponseData(scan);
  return a->start();
}

bool connected() { return g_conn_count > 0; }

int mesh_peers() {
  int n = 0;
  for (bool u : g_seat_used) n += u;
  return n;
}

void loop(mesh::node::Node& node, mesh::route::Millis now) {
  Msg m;
  while (g_q && xQueueReceive(g_q, &m, 0) == pdTRUE) handle(node, m, now);

  // Host link → its phone.
  if (g_host_conn >= 0) {
    Conn& c = g_conns[g_host_conn];
    if (c.used && c.host_sub && node.host_attached(kPort)) {
      pump(g_tx, c.handle, c.mtu > 3 ? size_t(c.mtu) - 3 : 20, g_host_pending,
           [&](uint8_t* out, size_t cap) { return node.host_tx(kPort, out, cap); });
    }
  }
  // The node's packets → each mesh phone.
  for (auto& c : g_conns) {
    if (!c.used || c.seat < 0 || !c.mesh_sub) continue;
    size_t port = mesh::node::Node::kMeshPort0 + size_t(c.seat);
    mesh::blemesh::Peer& p = g_peers[c.seat];
    pump(g_mesh_tx, c.handle, c.mtu > 3 ? size_t(c.mtu) - 3 : 20, g_seat_pending[c.seat],
         [&](uint8_t* out, size_t cap) { return p.tx(node, port, out, cap); });
  }
}

}  // namespace ble

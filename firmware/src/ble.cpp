#include "ble.h"

#include <Arduino.h>
#include <NimBLEDevice.h>
#include <freertos/FreeRTOS.h>
#include <freertos/idf_additions.h>
#include <freertos/queue.h>
#include <freertos/stream_buffer.h>
#include <freertos/task.h>

#include <cstdio>
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
enum class Ev : uint8_t { Connect, Disconnect, Mtu, HostWrite, MeshWrite, HostSub, MeshSub, ClientUp };
struct Msg {
  Ev ev;
  uint16_t conn;
  uint16_t n;  // bytes, MTU, or subscribe value
  uint8_t d[kMaxWrite];
};
QueueHandle_t g_q = nullptr;

// Mesh session ID: 0x00 then 3 random bytes, new each boot. Lower than almost
// any phone's (random 4 bytes), so phones connect to the board; between two
// boards (or the rare phone below it) the larger one connects (BleProto rule).
uint8_t g_session[4] = {};

// The one link this board opens itself (GATT client): candidates from the
// scan, bytes to write to the peer, its connection handle.
QueueHandle_t g_cand = nullptr;
StreamBufferHandle_t g_out = nullptr;
volatile uint16_t g_client_conn = 0xFFFF;
volatile bool g_client_kill = false;

NimBLECharacteristic* g_tx = nullptr;
NimBLECharacteristic* g_mesh_tx = nullptr;
volatile int g_conn_count = 0;

// For the board log (status()): what happened on Bluetooth since boot.
volatile uint32_t g_connects = 0, g_disconnects = 0, g_hellos = 0;
volatile int g_last_reason = 0;

volatile uint32_t g_lost = 0;
uint32_t g_corrupt = 0;  // phones dropped for a stream that made no sense
char g_corrupt_why[48] = {};  // events that could not be queued (for the log)

// Never drop silently: a lost write shifts the phone's byte stream (every
// packet after it is garbage until the link is dropped), and a lost
// subscribe or disconnect leaves a connection half dead. When the queue is
// full this waits (in NimBLE's task: the phone's write is answered late, so
// it slows down) for the main loop to catch up.
void post(Ev ev, uint16_t conn, uint16_t n = 0, const uint8_t* d = nullptr) {
  Msg m;
  m.ev = ev;
  m.conn = conn;
  m.n = n;
  if (d && n) {
    if (n > kMaxWrite) m.n = n = kMaxWrite;  // MTU is 247: never larger
    memcpy(m.d, d, n);
  }
  if (xQueueSend(g_q, &m, pdMS_TO_TICKS(2000)) != pdTRUE) g_lost = g_lost + 1;
}

class Server : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer*, NimBLEConnInfo& info) override {
    g_conn_count = g_conn_count + 1;
    g_connects = g_connects + 1;
    post(Ev::Connect, info.getConnHandle(), info.getMTU());
    // Room for more phones: keep advertising.
    if (g_conn_count < kMaxConns) NimBLEDevice::startAdvertising();
  }
  void onDisconnect(NimBLEServer*, NimBLEConnInfo& info, int reason) override {
    g_disconnects = g_disconnects + 1;
    g_last_reason = reason;
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

// ---- scan: other boards (and phones) with a lower session ID ----

class ScanCb : public NimBLEScanCallbacks {
  void onResult(const NimBLEAdvertisedDevice* d) override {
    if (!d->haveManufacturerData() || !d->isAdvertisingService(NimBLEUUID(kMeshService))) return;
    std::string m = d->getManufacturerData();
    if (m.size() != 2 + 4 || uint8_t(m[0]) != uint8_t(kManufacturer) || uint8_t(m[1]) != uint8_t(kManufacturer >> 8)) return;
    if (memcmp(g_session, m.data() + 2, 4) <= 0) return;  // theirs is larger: they connect to us
    ble_addr_t a = *d->getAddress().getBase();
    xQueueSend(g_cand, &a, 0);
  }
};
ScanCb g_scan_cb;

// Connects to one candidate at a time and keeps that link going: hello,
// notifications in (queued to the main loop like a phone's writes), the main
// loop's bytes out as writes with response.
void central_task(void*) {
  NimBLEScan* scan = NimBLEDevice::getScan();
  scan->setScanCallbacks(&g_scan_cb, false);
  scan->setActiveScan(false);
  scan->setInterval(1000);
  scan->setWindow(150);
  ble_addr_t last = {};
  uint32_t last_at = 0;
  for (;;) {
    if (!scan->isScanning()) scan->start(30000, false, true);
    ble_addr_t a;
    if (xQueueReceive(g_cand, &a, pdMS_TO_TICKS(1000)) != pdTRUE) continue;
    if (g_conn_count >= kMaxConns) continue;
    if (memcmp(&a, &last, sizeof a) == 0 && millis() - last_at < 30000) continue;  // tried just now
    last = a;
    last_at = millis();
    scan->stop();
    NimBLEClient* cl = NimBLEDevice::createClient();
    cl->setConnectTimeout(5000);
    if (!cl->connect(NimBLEAddress(a))) {
      NimBLEDevice::deleteClient(cl);
      continue;
    }
    NimBLERemoteService* svc = cl->getService(kMeshService);
    NimBLERemoteCharacteristic* rx = svc ? svc->getCharacteristic(kMeshRx) : nullptr;
    NimBLERemoteCharacteristic* tx = svc ? svc->getCharacteristic(kMeshTx) : nullptr;
    if (!rx || !tx) {
      cl->disconnect();
      NimBLEDevice::deleteClient(cl);
      continue;
    }
    g_conn_count = g_conn_count + 1;
    g_client_kill = false;
    xStreamBufferReset(g_out);
    g_client_conn = cl->getConnHandle();
    g_hellos = g_hellos + 1;
    post(Ev::ClientUp, g_client_conn, cl->getMTU());
    uint8_t hello[1 + 4] = {mesh::blemesh::kHelloMagic, g_session[0], g_session[1], g_session[2], g_session[3]};
    bool ok = tx->subscribe(true, [](NimBLERemoteCharacteristic*, uint8_t* d, size_t n, bool) {
      post(Ev::MeshWrite, g_client_conn, uint16_t(n), d);
    }) && rx->writeValue(hello, sizeof hello, true);
    while (ok && cl->isConnected() && !g_client_kill) {
      uint8_t buf[kMaxWrite];
      size_t chunk = cl->getMTU() > 3 ? size_t(cl->getMTU()) - 3 : 20;
      if (chunk > sizeof buf) chunk = sizeof buf;
      size_t n = xStreamBufferReceive(g_out, buf, chunk, pdMS_TO_TICKS(200));
      if (n && !rx->writeValue(buf, n, true)) break;
    }
    post(Ev::Disconnect, g_client_conn);
    g_client_conn = 0xFFFF;
    if (cl->isConnected()) cl->disconnect();
    NimBLEDevice::deleteClient(cl);
    g_conn_count = g_conn_count > 0 ? g_conn_count - 1 : 0;
  }
}

// ---- main-loop state ----

struct Conn {
  bool used = false;
  bool client = false;  // this board connected (writes out instead of notifications)
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
    case Ev::ClientUp: {
      Conn* x = c;  // the server callbacks may have seen this connection too
      if (x && x->seat >= 0) free_seat(node, *x);
      for (auto& y : g_conns) {
        if (x) break;
        if (!y.used) x = &y;
      }
      int seat = -1;
      for (int i = 0; i < int(mesh::node::Node::kMeshPorts); i++)
        if (!g_seat_used[i]) {
          seat = i;
          break;
        }
      if (!x || seat < 0) {
        g_client_kill = true;
        return;
      }
      *x = Conn();
      x->used = true;
      x->client = true;
      x->handle = m.conn;
      x->mtu = m.n ? m.n : 23;
      x->mesh_sub = true;
      x->seat = seat;
      g_seat_used[seat] = true;
      g_peers[seat].reset();
      g_peers[seat].start(node, mesh::node::Node::kMeshPort0 + size_t(seat), now);
      return;
    }
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
      const bool had_hello = g_peers[c->seat].up();
      const bool ok = g_peers[c->seat].rx(node, port, m.d, m.n, now);
      if (ok && !had_hello && g_peers[c->seat].up()) g_hellos = g_hellos + 1;
      if (!ok) {
        g_corrupt++;
        std::snprintf(g_corrupt_why, sizeof g_corrupt_why, "%s", g_peers[c->seat].error());
        free_seat(node, *c);
        if (c->client) g_client_kill = true;  // the central task closes it
        else drop_conn(m.conn);               // not a mesh phone, or a corrupt stream: it reconnects
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

bool begin(const char* name, const uint8_t id[6]) {
  // Room for bursts (a phone that passes on its relay traffic writes several
  // packets a second while the main loop draws the screen): PSRAM.
  g_q = xQueueCreateWithCaps(256, sizeof(Msg), MALLOC_CAP_SPIRAM);
  if (!g_q) g_q = xQueueCreate(32, sizeof(Msg));
  g_cand = xQueueCreate(4, sizeof(ble_addr_t));
  g_out = xStreamBufferCreate(2048, 1);
  if (!g_q || !g_cand || !g_out) return false;
  uint32_t r = esp_random();
  g_session[0] = 0;
  g_session[1] = uint8_t(r >> 16);
  g_session[2] = uint8_t(r >> 8);
  g_session[3] = uint8_t(r) | 1;  // never 0 0 0 0
  NimBLEDevice::init(name);
  // A random static address made from the node's identity, not the chip's
  // public MAC: stable across restarts, new after a wipe. Phones that paired
  // with this chip under its MAC in the pairing-code builds (beta10–beta13)
  // still hold that bond and try to encrypt with keys the board no longer
  // has: the link drops right after connecting ("PIN or key missing") and the
  // mesh never forms. Under a new address the phone sees a new device.
  uint8_t addr[6];
  memcpy(addr, id, 6);
  addr[5] |= 0xC0;  // random static: the two top bits set (val[] is little-endian)
  bool addr_ok = NimBLEDevice::setOwnAddr(addr) && NimBLEDevice::setOwnAddrType(BLE_OWN_ADDR_RANDOM);
  if (!addr_ok) NimBLEDevice::setOwnAddrType(BLE_OWN_ADDR_PUBLIC);
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
  // session ID (g_session: phones connect to the board; boards between
  // themselves: the larger ID connects, central_task). The host-link service and the name go in the
  // scan response (3 + 18 + 8 = 29 of 31 bytes in the advert).
  NimBLEAdvertisementData adv;
  adv.setFlags(BLE_HS_ADV_F_DISC_GEN | BLE_HS_ADV_F_BREDR_UNSUP);
  adv.addServiceUUID(NimBLEUUID(kMeshService));
  const uint8_t manu[] = {uint8_t(kManufacturer), uint8_t(kManufacturer >> 8), g_session[0], g_session[1], g_session[2],
                          g_session[3]};
  adv.setManufacturerData(manu, sizeof manu);
  NimBLEAdvertisementData scan;
  scan.addServiceUUID(NimBLEUUID(kService));
  scan.setName(name);
  NimBLEAdvertising* a = NimBLEDevice::getAdvertising();
  a->setAdvertisementData(adv);
  a->setScanResponseData(scan);
  if (!a->start()) return false;
  // Other boards nearby: connect to those with a lower session ID.
  return xTaskCreatePinnedToCore(central_task, "ble-central", 6144, nullptr, 1, nullptr, 0) == pdPASS;
}

bool connected() { return g_conn_count > 0; }

void status(char* out, size_t cap) {
  NimBLEAdvertising* a = NimBLEDevice::getAdvertising();
  const int r = g_last_reason;
  const int hci = r >= 0x200 && r < 0x300 ? r - 0x200 : -1;  // BLE_HS_ERR_HCI_BASE + HCI code
  const char* why = hci == 0x06   ? " (phone holds an old pairing: forget Mava in its Bluetooth settings)"
                    : hci == 0x3D ? " (encryption failed: forget Mava in the phone's Bluetooth settings)"
                    : hci == 0x13 ? " (phone closed it)"
                    : hci == 0x08 ? " (out of range)"
                    : hci == 0x3E ? " (connection failed)"
                                  : "";
  uint32_t pin = 0, pskip = 0, pout = 0;
  for (const auto& p : g_peers) {
    pin += p.packets_in();
    pskip += p.packets_skipped();
    pout += p.packets_out();
  }
  int k = snprintf(out, cap, "%s %s, advertising %s, links %d, connects %lu, hellos %lu, drops %lu, last drop 0x%x%s",
           NimBLEDevice::getAddress().toString().c_str(),
           NimBLEDevice::getAddress().getType() == BLE_ADDR_RANDOM ? "random" : "public",
           a && a->isAdvertising() ? "on" : "OFF", int(g_conn_count), (unsigned long)g_connects,
           (unsigned long)g_hellos, (unsigned long)g_disconnects, r, why);
  // Packets on the Bluetooth mesh since boot (in: from phones to the node).
  if (k > 0 && size_t(k) < cap)
    snprintf(out + k, cap - size_t(k), "; packets in %lu (too big for LoRa, skipped %lu), out %lu; queue %u waiting, lost %lu, corrupt streams %lu%s%s%s",
             (unsigned long)pin, (unsigned long)pskip, (unsigned long)pout, unsigned(uxQueueMessagesWaiting(g_q)),
             (unsigned long)g_lost, (unsigned long)g_corrupt, g_corrupt_why[0] ? " (last: " : "",
             g_corrupt_why, g_corrupt_why[0] ? ")" : "");
}

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
    if (c.client) {  // our own link: hand bytes to the central task, which writes them
      size_t room = xStreamBufferSpacesAvailable(g_out);
      uint8_t buf[kMaxWrite];
      while (room >= sizeof buf) {
        size_t k = p.tx(node, port, buf, sizeof buf);
        if (!k) break;
        xStreamBufferSend(g_out, buf, k, 0);
        room -= k;
      }
      continue;
    }
    pump(g_mesh_tx, c.handle, c.mtu > 3 ? size_t(c.mtu) - 3 : 20, g_seat_pending[c.seat],
         [&](uint8_t* out, size_t cap) { return p.tx(node, port, out, cap); });
  }
}

}  // namespace ble

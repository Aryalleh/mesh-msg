// Mava custom board firmware (docs/board.md). Everything that decides
// anything lives in lib/ (node::Node, meshtastic::Backend); this file boots
// them from flash and runs the loop:
//
//   radio:  Meshtastic frame received → backend.decode → node.air_rx();
//           node.radio_next() → CAD / backend.encode → transmit
//   USB:    serial bytes ↔ node host port 0 (meshd, or a phone on USB-OTG)
//   BLE:    GATT host link ↔ node host port 1 (phone app)
//   Wi-Fi:  setup switch → hotspot + panel, or Wi-Fi + relay worker (net.cpp)
//   timer:  node.tick() once a second
//
// Until a USB host sends its first frame, the serial port prints
// human-readable logs (what is heard, sent, the device password); once a
// host attaches the port carries only the COBS host-link stream.
#include <Arduino.h>
#include <freertos/FreeRTOS.h>
#include <freertos/semphr.h>

#include <cstdarg>
#include <cstdio>

#include "ble.h"
#include "board.h"
#include "meshtastic.h"
#include "net.h"
#include "node.h"
#include "radio.h"

using mesh::route::Millis;

namespace {

constexpr const char* kIdentityFile = "/id";
constexpr const char* kConfigFile = "/cfg";
constexpr Millis kTickEvery = 1000;
constexpr Millis kLedFlash = 30;
constexpr size_t kUsbPort = 0;

mesh::node::Node* g_node = nullptr;
mesh::meshtastic::Backend g_mt;
net::Settings g_net;
SemaphoreHandle_t g_lock = nullptr;
bool g_radio_ok = false;
Millis g_led_off = 0;
char g_line[160];

void slot_path(char* out, size_t cap, size_t slot) { snprintf(out, cap, "/s%02u", unsigned(slot)); }

void logf(const char* fmt, ...) {
  if (g_node && g_node->host_attached(kUsbPort)) return;
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(g_line, sizeof g_line, fmt, ap);
  va_end(ap);
  Serial.print(g_line);
  Serial.print("\r\n");
}

void flash_led() {
  board::led(true);
  g_led_off = board::now_ms() + kLedFlash;
}

mesh::meshtastic::Channel channel_of(const net::Settings& s) {
  mesh::meshtastic::Channel c;
  memcpy(c.name, s.channel, sizeof c.name - 1);
  c.name[sizeof c.name - 1] = 0;
  memcpy(c.psk, s.psk, sizeof c.psk);
  c.psk_len = s.psk_len;
  return c;
}

// ---- flash backends ----

struct FlashPersist : mesh::store::Persist {
  void save(size_t slot, const mesh::store::Record& r) override {
    char p[16];
    slot_path(p, sizeof p, slot);
    board::storage_write(p, &r, sizeof r);
  }
  void erase(size_t slot) override {
    char p[16];
    slot_path(p, sizeof p, slot);
    board::storage_remove(p);
  }
};

struct Board : mesh::node::Platform {
  void save_config(const mesh::host::NodeConfig& c) override {
    uint8_t tlv[mesh::host::kMaxConfigTLV];
    size_t n = c.encode_tlv(tlv, sizeof tlv);
    board::storage_write(kConfigFile, tlv, n);
    logf("config saved: %s %lu Hz %d dBm role %u", mesh::host::kRegions[c.region].name,
         (unsigned long)c.channel_hz(), c.tx_dbm, c.role);
  }
  void apply_radio(const mesh::host::NodeConfig& c) override {
    g_radio_ok = radio::configure(c);
    logf("radio retuned: %lu Hz %d dBm (%s)", (unsigned long)c.channel_hz(), c.tx_dbm, g_radio_ok ? "ok" : "FAILED");
  }
  void wipe() override {
    logf("wipe: erasing identity, config, Wi-Fi settings and store");
    board::storage_format();
  }
  void reboot() override { board::reboot(); }
  uint16_t battery_mv() override { return board::battery_mv(); }
};

FlashPersist g_persist;
Board g_board;

// ---- boot ----

void load_identity(uint8_t seed[mesh::kSeedSize]) {
  if (board::storage_read(kIdentityFile, seed, mesh::kSeedSize) == mesh::kSeedSize) return;
  board::random_bytes(seed, mesh::kSeedSize);
  board::storage_write(kIdentityFile, seed, mesh::kSeedSize);
  logf("new identity generated");
}

mesh::host::NodeConfig load_config() {
  mesh::host::NodeConfig c;
  c.region = MESH_DEFAULT_REGION;
  c.preset = 1;
  c.tx_dbm = 14;
  c.role = mesh::host::kRoleGateway;
  snprintf(c.name, sizeof c.name, "%s", BOARD_NAME);
  uint8_t tlv[mesh::host::kMaxConfigTLV];
  size_t n = board::storage_read(kConfigFile, tlv, sizeof tlv);
  if (n > 0 && !c.apply_tlv(tlv, n)) logf("stored config invalid, using defaults");
  return c;
}

void restore_store(mesh::node::Node& node, Millis now) {
  static mesh::store::Record rec;
  size_t restored = 0;
  for (size_t slot = 0; slot < mesh::store::kSlots; slot++) {
    char p[16];
    slot_path(p, sizeof p, slot);
    if (board::storage_read(p, &rec, sizeof rec) != sizeof rec) continue;
    if (node.restore(slot, rec, now))
      restored++;
    else
      board::storage_remove(p);
  }
  if (restored) logf("restored %u stored packets", unsigned(restored));
}

void describe(const char* what, const uint8_t* raw, size_t n, uint32_t from, int snr) {
  mesh::Packet p;
  if (mesh::decode(raw, n, p) != mesh::Err::Ok) return;
  logf("%s %s %u B via !%08lx hop %u/%u id %02x%02x%02x%02x dest %02x%02x%02x%02x snr %d", what,
       mesh::type_name(p.type), unsigned(n), (unsigned long)from, p.hop_count, p.max_hops, p.id[0], p.id[1], p.id[2],
       p.id[3], p.dest[0], p.dest[1], p.dest[2], p.dest[3], snr);
}

void on_channel_changed(const net::Settings& s) {
  bool ok = g_mt.set_channel(channel_of(s));
  logf("Meshtastic channel %s (hash %u) %s", s.channel, g_mt.channel_hash(), ok ? "" : "INVALID");
}

}  // namespace

void setup() {
  Serial.begin(115200);
  Serial.setTimeout(0);
  board::init();
  Millis t0 = board::now_ms();
  while (!Serial && board::now_ms() - t0 < 1500) delay(10);  // a monitor just opened?

  bool fs = board::storage_begin();
  logf("Mava board %s, firmware v%u, flash %s", BOARD_NAME, mesh::node::kFirmwareVersion, fs ? "ok" : "FAILED");

  uint8_t seed[mesh::kSeedSize];
  load_identity(seed);
  mesh::host::NodeConfig cfg = load_config();
  static mesh::node::Node node(cfg, seed, g_board, &g_persist, board::random32(), &mesh::mac::kMeshtasticLongFast);
  memset(seed, 0, sizeof seed);
  g_node = &node;
  g_lock = xSemaphoreCreateMutex();

  if (!net::load(g_net)) g_net = net::Settings();
  net::Settings before = g_net;
  net::init_secrets(g_net);
  if (memcmp(&before, &g_net, sizeof g_net) != 0) net::save(g_net);

  g_mt.begin(mesh::meshtastic::Backend::node_id_for(node.pub().x), board::random32(), channel_of(g_net));
  g_radio_ok = radio::begin(cfg);
  logf("%s: %s, %s %lu Hz %d dBm, Meshtastic !%08lx channel %s (hash %u)", radio::chip_name(),
       g_radio_ok ? "ok" : "NOT FOUND", mesh::host::kRegions[cfg.region].name, (unsigned long)cfg.channel_hz(),
       cfg.tx_dbm, (unsigned long)g_mt.node_id(), g_net.channel, g_mt.channel_hash());
  const mesh::PublicKey& pk = node.pub();
  logf("identity ed25519 %02x%02x%02x%02x%02x%02x%02x%02x…", pk.ed[0], pk.ed[1], pk.ed[2], pk.ed[3], pk.ed[4], pk.ed[5],
       pk.ed[6], pk.ed[7]);
  restore_store(node, board::now_ms());

  char name[24];
  snprintf(name, sizeof name, "Mava-%02X%02X", pk.ed[0], pk.ed[1]);
  bool ble_ok = ble::begin(name, g_net.ble_passkey);
  net::Hooks hooks;
  hooks.channel_changed = on_channel_changed;
  bool net_ok = net::begin(g_net, node, g_lock, hooks);
  logf("BLE %s as %s (passkey %06lu); Wi-Fi %s, setup switch %s", ble_ok ? "ok" : "FAILED", name,
       (unsigned long)g_net.ble_passkey, net_ok ? "ok" : "FAILED", board::setup_switch_on() ? "ON" : "off");
  logf("hotspot %s, device password: %s", name, g_net.password);
  logf("ready; attach meshd ([lora] port = this serial port), pair a phone, or watch the air");
}

void loop() {
  mesh::node::Node& node = *g_node;
  static uint8_t buf[512];
  static Millis last_tick = 0;
  Millis now = board::now_ms();
  xSemaphoreTake(g_lock, portMAX_DELAY);

  // ---- radio (Meshtastic backbone) ----
  if (g_radio_ok) {
    if (radio::tx_done()) {
      node.radio_tx_done(now);
    } else {
      int snr = 0;
      size_t n = radio::receive(buf, mesh::meshtastic::kRadioMax, &snr);
      if (n > 0) {
        flash_led();
        uint8_t mava[mesh::kLoRaMTU];
        mesh::meshtastic::RxMeta meta;
        size_t mn = g_mt.decode(buf, n, mava, sizeof mava, &meta);
        if (mn) {
          describe("rx", mava, mn, meta.from, snr);
          node.air_rx(mava, mn, meta.relay_node ? meta.relay_node : uint8_t(meta.from), snr, now);
        }
      }
    }
    if (!radio::transmitting()) {
      mesh::mac::Action a = node.radio_next(now);
      if (a.kind == mesh::mac::Kind::Cad) {
        node.radio_cad(radio::cad_busy(), now);
        a = node.radio_next(now);
      }
      if (a.kind == mesh::mac::Kind::Transmit) {
        // The router's frame is (short ID || Mava packet); Meshtastic carries the packet.
        const uint8_t* raw = a.frame->data + mesh::kLoRaLinkHeader;
        size_t rn = a.frame->len - mesh::kLoRaLinkHeader;
        uint8_t frame[mesh::meshtastic::kRadioMax];
        size_t fn = g_mt.encode(raw, rn, frame, sizeof frame);
        if (fn && radio::transmit(frame, fn)) {
          flash_led();
          describe("tx", raw, rn, g_mt.node_id(), 0);
        } else {
          node.radio_tx_done(now);  // not sent (hop limit used up, or radio busy)
          if (!fn) logf("tx skipped: Meshtastic hop limit used up");
        }
      }
    }
  }

  // ---- USB host (port 0) ----
  if (node.host_attached(kUsbPort) && !Serial) {
    node.host_detach(kUsbPort);
    logf("USB host detached");
  }
  int avail = Serial.available();
  if (avail > 0) {
    size_t n = Serial.readBytes(reinterpret_cast<char*>(buf), size_t(avail) < sizeof buf ? size_t(avail) : sizeof buf);
    bool was = node.host_attached(kUsbPort);
    node.host_rx(kUsbPort, buf, n, now);
    if (!was && node.host_attached(kUsbPort)) flash_led();
  }
  if (node.host_pending(kUsbPort) > 0) {
    int room = Serial.availableForWrite();
    if (room > 0) {
      size_t n = node.host_tx(kUsbPort, buf, size_t(room) < sizeof buf ? size_t(room) : sizeof buf);
      if (n > 0) Serial.write(buf, n);
    }
  }

  // ---- BLE host (port 1) ----
  ble::loop(node, now);

  // ---- Wi-Fi: setup switch, hotspot panel, NTP ----
  net::loop(now);

  // ---- timer ----
  if (now - last_tick >= kTickEvery) {
    last_tick = now;
    node.tick(now);
  }
  if (g_led_off && now >= g_led_off) {
    board::led(false);
    g_led_off = 0;
  }
  bool reboot = node.reboot_requested() && node.hosts_pending() == 0;
  xSemaphoreGive(g_lock);

  if (reboot) {
    Serial.flush();
    board::reboot();
  }
  delay(1);
}

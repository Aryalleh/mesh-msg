// LoRa node firmware entry point. Everything that decides anything lives in
// lib/ (node::Node); this file boots it from flash and runs the loop in
// node.h's comment:
//
//   radio:  frame received → node.radio_rx(); node.radio_next() → CAD / TX
//   host:   USB serial bytes ↔ node.host_rx() / node.host_tx()
//   timer:  node.tick() once a second
//
// Until a host (meshd, or the Android app over USB) sends its first frame,
// the serial port prints human-readable logs — decoded headers of every
// packet heard, transmissions, status — so a board on a bench shows what it
// hears (docs/firmware.md §7, milestone 1). Once a host attaches the port
// carries only the COBS host-link stream; logs resume when it detaches.
#include <Arduino.h>

#include <cstdarg>
#include <cstdio>

#include "board.h"
#include "node.h"
#include "radio.h"

using mesh::route::Millis;

namespace {

constexpr const char* kIdentityFile = "/id";
constexpr const char* kConfigFile = "/cfg";
constexpr Millis kTickEvery = 1000;
constexpr Millis kLedFlash = 30;

mesh::node::Node* g_node = nullptr;
bool g_radio_ok = false;
Millis g_led_off = 0;
char g_line[160];

void slot_path(char* out, size_t cap, size_t slot) { snprintf(out, cap, "/s%02u", unsigned(slot)); }

void logf(const char* fmt, ...) {
  if (g_node && g_node->host_attached()) return;
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
    logf("config saved: %s %s %lu Hz %d dBm role %u", mesh::host::kRegions[c.region].name,
         mesh::host::kPresets[c.preset].name, (unsigned long)c.channel_hz(), c.tx_dbm, c.role);
  }
  void apply_radio(const mesh::host::NodeConfig& c) override {
    g_radio_ok = radio::configure(c);
    logf("radio retuned: %lu Hz %s %d dBm (%s)", (unsigned long)c.channel_hz(), mesh::host::kPresets[c.preset].name,
         c.tx_dbm, g_radio_ok ? "ok" : "FAILED");
  }
  void wipe() override {
    logf("wipe: erasing identity, config and store");
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
  c.preset = 1;  // BALANCED
  c.tx_dbm = 14;
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

void describe(const char* what, const uint8_t* frame, size_t n, int snr) {
  mesh::Packet p;
  if (n < 2 || mesh::decode(frame + 1, n - 1, p) != mesh::Err::Ok) {
    logf("%s %u B from 0x%02x: undecodable", what, unsigned(n), n ? frame[0] : 0);
    return;
  }
  logf("%s %s %u B from 0x%02x hop %u/%u id %02x%02x%02x%02x dest %02x%02x%02x%02x snr %d", what,
       mesh::type_name(p.type), unsigned(n), frame[0], p.hop_count, p.max_hops, p.id[0], p.id[1], p.id[2], p.id[3],
       p.dest[0], p.dest[1], p.dest[2], p.dest[3], snr);
}

}  // namespace

void setup() {
  Serial.begin(115200);
  Serial.setTimeout(0);
  board::init();
  Millis t0 = board::now_ms();
  while (!Serial && board::now_ms() - t0 < 1500) delay(10);  // a monitor just opened?

  bool fs = board::storage_begin();
  logf("mesh-msg node %s, firmware v%u, flash %s", BOARD_NAME, mesh::node::kFirmwareVersion, fs ? "ok" : "FAILED");

  uint8_t seed[mesh::kSeedSize];
  load_identity(seed);
  mesh::host::NodeConfig cfg = load_config();
  static mesh::node::Node node(cfg, seed, g_board, &g_persist, board::random32());
  memset(seed, 0, sizeof seed);
  g_node = &node;

  g_radio_ok = radio::begin(cfg);
  logf("%s: %s, %s %s %lu Hz %d dBm, role %u, short id 0x%02x", radio::chip_name(), g_radio_ok ? "ok" : "NOT FOUND",
       mesh::host::kRegions[cfg.region].name, mesh::host::kPresets[cfg.preset].name, (unsigned long)cfg.channel_hz(),
       cfg.tx_dbm, cfg.role, node.short_id());
  const mesh::PublicKey& pk = node.pub();
  logf("identity ed25519 %02x%02x%02x%02x%02x%02x%02x%02x…", pk.ed[0], pk.ed[1], pk.ed[2], pk.ed[3], pk.ed[4], pk.ed[5],
       pk.ed[6], pk.ed[7]);
  restore_store(node, board::now_ms());
  logf("ready; attach meshd ([lora] port = this serial port) or watch the air");
}

void loop() {
  mesh::node::Node& node = *g_node;
  static uint8_t buf[512];
  static Millis last_tick = 0;
  Millis now = board::now_ms();

  // ---- radio ----
  if (g_radio_ok) {
    if (radio::tx_done()) {
      node.radio_tx_done(now);
    } else {
      int snr = 0;
      size_t n = radio::receive(buf, mesh::kLoRaRadioMax, &snr);
      if (n > 0) {
        flash_led();
        describe("rx", buf, n, snr);
        node.radio_rx(buf, n, snr, now);
      }
    }
    if (!radio::transmitting()) {
      mesh::mac::Action a = node.radio_next(now);
      if (a.kind == mesh::mac::Kind::Cad) {
        node.radio_cad(radio::cad_busy(), now);
        a = node.radio_next(now);
      }
      if (a.kind == mesh::mac::Kind::Transmit) {
        if (radio::transmit(a.frame->data, a.frame->len)) {
          flash_led();
          describe("tx", a.frame->data, a.frame->len, 0);
        } else {
          node.radio_tx_done(now);  // give the frame back to the router's bookkeeping
          logf("tx failed");
        }
      }
    }
  }

  // ---- host ----
  if (node.host_attached() && !Serial) {
    node.host_detach();
    logf("host detached");
  }
  int avail = Serial.available();
  if (avail > 0) {
    size_t n = Serial.readBytes(reinterpret_cast<char*>(buf), size_t(avail) < sizeof buf ? size_t(avail) : sizeof buf);
    bool was = node.host_attached();
    node.host_rx(buf, n, now);
    if (!was && node.host_attached()) flash_led();
  }
  if (node.host_pending() > 0) {
    int room = Serial.availableForWrite();
    if (room > 0) {
      size_t n = node.host_tx(buf, size_t(room) < sizeof buf ? size_t(room) : sizeof buf);
      if (n > 0) Serial.write(buf, n);
    }
  }

  // ---- timer ----
  if (now - last_tick >= kTickEvery) {
    last_tick = now;
    node.tick(now);
  }
  if (g_led_off && now >= g_led_off) {
    board::led(false);
    g_led_off = 0;
  }
  if (node.reboot_requested() && node.host_pending() == 0) {
    Serial.flush();
    board::reboot();
  }
  delay(1);
}

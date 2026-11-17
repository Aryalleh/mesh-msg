// The node application end to end on the host: host commands and events,
// own announces, store-and-forward to the host, inflight hand-back, receipt
// purging, and nodes passing a host's packet over a simulated radio.
#include <memory>
#include <random>
#include <vector>

#include "announce.h"
#include "check.h"
#include "hostlink.h"
#include "blemesh.h"
#include "node.h"

using check::Bytes;
using mesh::route::Millis;
namespace host = mesh::host;

namespace {

struct FakeBoard : mesh::node::Platform {
  int saves = 0, radios = 0, wipes = 0, reboots = 0;
  host::NodeConfig saved, radio;
  void save_config(const host::NodeConfig& c) override {
    saves++;
    saved = c;
  }
  void apply_radio(const host::NodeConfig& c) override {
    radios++;
    radio = c;
  }
  void wipe() override { wipes++; }
  void reboot() override { reboots++; }
  uint16_t battery_mv() override { return 3912; }
  bool has_wifi = true;
  int wifi_sets = 0, hotspots = 0;
  host::WifiConfig wifi;
  bool hotspot_on = false;
  uint8_t set_wifi(const host::WifiConfig& w) override {
    if (!has_wifi) return Platform::set_wifi(w);
    wifi = w;
    wifi_sets++;
    return host::kAckOk;
  }
  std::vector<std::pair<uint8_t, Bytes>> board_cmds;
  uint8_t board_command(size_t, uint8_t op, const uint8_t* a, size_t n) override {
    if (op != host::kOpScreen && op != host::kOpInput) return host::kAckUnknownOp;
    board_cmds.emplace_back(op, Bytes(a, a + n));
    return host::kAckOk;
  }
  uint8_t hotspot(bool on) override {
    if (!has_wifi) return Platform::hotspot(on);
    hotspot_on = on;
    hotspots++;
    return host::kAckOk;
  }
};

struct FakeFlash : mesh::store::Persist {
  mesh::store::Record rec[mesh::store::kSlots];
  bool live[mesh::store::kSlots] = {};
  void save(size_t i, const mesh::store::Record& r) override {
    rec[i] = r;
    live[i] = true;
  }
  void erase(size_t i) override { live[i] = false; }
};

struct Frame {
  uint8_t kind;
  Bytes payload;
};

// The host end of the stream.
struct Host {
  host::Decoder dec;
  std::vector<Frame> got;
  size_t port = 0;

  void send(mesh::node::Node& n, uint8_t kind, const Bytes& payload, Millis now) {
    uint8_t buf[host::kMaxEncoded];
    size_t e = host::encode(kind, payload.data(), payload.size(), buf, sizeof buf);
    n.host_rx(port, buf, e, now);
  }
  void cmd(mesh::node::Node& n, uint8_t op, uint8_t req, const Bytes& args, Millis now) {
    Bytes p = {op, req};
    p.insert(p.end(), args.begin(), args.end());
    send(n, host::kKindCommand, p, now);
  }
  void drain(mesh::node::Node& n, size_t chunk = 20) {
    uint8_t buf[64];
    for (;;) {
      size_t k = n.host_tx(port, buf, chunk < sizeof buf ? chunk : sizeof buf);
      if (k == 0) break;
      size_t off = 0;
      while (off < k) {
        off += dec.feed(buf + off, k - off);
        if (dec.frame()) got.push_back(Frame{dec.kind(), Bytes(dec.payload(), dec.payload() + dec.payload_len())});
      }
    }
  }
  // Events with op ev, removed from got.
  std::vector<Bytes> events(uint8_t ev) {
    std::vector<Bytes> out;
    std::vector<Frame> rest;
    for (auto& f : got) {
      if (f.kind == host::kKindEvent && f.payload.size() >= 2 && f.payload[0] == ev)
        out.push_back(f.payload);
      else
        rest.push_back(f);
    }
    got = rest;
    return out;
  }
  std::vector<Bytes> packets() {
    std::vector<Bytes> out;
    std::vector<Frame> rest;
    for (auto& f : got) {
      if (f.kind == host::kKindPacket)
        out.push_back(f.payload);
      else
        rest.push_back(f);
    }
    got = rest;
    return out;
  }
};

Bytes seed(uint8_t n) {
  Bytes s(64);
  for (int i = 0; i < 64; i++) s[i] = uint8_t(n * 31 + i);
  return s;
}

struct Rig {
  FakeBoard board;
  FakeFlash flash;
  std::unique_ptr<mesh::node::Node> node;
  Host host;
  Rig(uint8_t n, host::NodeConfig cfg = {}) {
    Bytes s = seed(n);
    node = std::make_unique<mesh::node::Node>(cfg, s.data(), board, &flash, 1000u + n);
  }
};

// Everyone hears everyone; no collisions. Steps until `until`.
struct Air {
  struct Tx {
    size_t from;
    Millis end;
    Bytes frame;
  };
  std::vector<Tx> txs;
  Millis now = 0;
  int frames = 0;

  void run(std::vector<mesh::node::Node*> nodes, Millis until, int snr = 0) {
    for (; now < until; now += 5) {
      for (size_t i = 0; i < txs.size();) {
        if (txs[i].end > now) {
          i++;
          continue;
        }
        Tx t = txs[i];
        txs.erase(txs.begin() + long(i));
        nodes[t.from]->radio_tx_done(now);
        for (size_t j = 0; j < nodes.size(); j++)
          if (j != t.from) nodes[j]->radio_rx(t.frame.data(), t.frame.size(), snr, now);
      }
      for (size_t i = 0; i < nodes.size(); i++) {
        bool busy = false;
        for (auto& t : txs) busy |= t.from == i;
        if (busy) continue;
        mesh::mac::Action a = nodes[i]->radio_next(now);
        if (a.kind == mesh::mac::Kind::Cad) {
          bool chan = !txs.empty();
          nodes[i]->radio_cad(chan, now);
          a = nodes[i]->radio_next(now);
        }
        if (a.kind == mesh::mac::Kind::Transmit) {
          txs.push_back(Tx{i, now + a.air_ms, Bytes(a.frame->data, a.frame->data + a.frame->len)});
          frames++;
        }
        if (now % 1000 == 0) nodes[i]->tick(now);
      }
    }
  }
};

Bytes data_packet(uint8_t id, uint8_t dest0 = 0xd0, uint8_t ctx = 0) {
  Bytes body(40, 0x42);
  mesh::Packet p;
  p.type = mesh::Type::Data;
  p.ctx = ctx;
  p.max_hops = 3;
  for (int i = 0; i < 8; i++) p.id[i] = uint8_t(id + i);
  for (int i = 0; i < 16; i++) p.dest[i] = uint8_t(dest0 + i);
  p.body = body.data();
  p.body_len = body.size();
  Bytes out(p.size());
  size_t n = 0;
  mesh::encode(p, out.data(), out.size(), &n);
  return out;
}

Bytes be32(uint32_t v) { return {uint8_t(v >> 24), uint8_t(v >> 16), uint8_t(v >> 8), uint8_t(v)}; }

void commands() {
  Rig r(1);
  auto& n = *r.node;
  auto& h = r.host;
  h.cmd(n, host::kOpHello, 7, {}, 1000);
  h.drain(n);
  auto st = h.events(host::kEvStatus);
  CHECK(st.size() == 1 && st[0].size() == 2 + host::kStatusSize && st[0][1] == 7);
  host::Status want = n.status(1000);
  uint8_t enc[host::kStatusSize];
  want.encode(enc);
  CHECK(Bytes(st[0].begin() + 2, st[0].end()) == Bytes(enc, enc + sizeof enc));
  CHECK(want.freq_hz == 869525000 && want.role == host::kRoleClient && want.battery_mv == 3912 &&
        want.air_budget_ms == 360000 && want.short_id == n.short_id());

  h.cmd(n, host::kOpGetConfig, 8, {}, 1000);
  h.drain(n);
  auto cf = h.events(host::kEvConfig);
  CHECK(cf.size() == 1);
  host::NodeConfig c;
  CHECK(c.apply_tlv(cf[0].data() + 2, cf[0].size() - 2) && c == n.config());

  // Bad config: rejected, nothing saved.
  h.cmd(n, host::kOpSetConfig, 9, {host::kTagTxDBm, 1, 40}, 1000);
  h.drain(n);
  auto acks = h.events(host::kEvAck);
  CHECK(acks.size() == 1 && acks[0][2] == host::kOpSetConfig && acks[0][3] == host::kAckBadArgs);
  CHECK(r.board.saves == 0);

  // Good config: saved, radio retuned, MAC uses the new preset.
  Bytes tlv = {host::kTagPreset, 1, 2, host::kTagName, 4, 'r', 'o', 'o', 'f', host::kTagRole, 1, host::kRoleRepeater};
  h.cmd(n, host::kOpSetConfig, 10, tlv, 1000);
  h.drain(n);
  acks = h.events(host::kEvAck);
  CHECK(acks.size() == 1 && acks[0][3] == host::kAckOk);
  CHECK(r.board.saves == 1 && r.board.saved.preset == 2 && std::string(r.board.saved.name) == "roof");
  CHECK(r.board.radios == 1 && n.mac().config().preset.sf == 11 && n.config().role == host::kRoleRepeater);
  // Name-only change does not retune.
  h.cmd(n, host::kOpSetConfig, 11, {host::kTagName, 1, 'x'}, 1000);
  h.drain(n);
  CHECK(r.board.saves == 2 && r.board.radios == 1);
  h.events(host::kEvAck);

  h.cmd(n, host::kOpGetIdentity, 12, {}, 1000);
  h.drain(n);
  auto idv = h.events(host::kEvIdentity);
  CHECK(idv.size() == 1 && idv[0].size() == 66);
  CHECK(Bytes(idv[0].begin() + 2, idv[0].begin() + 34) == Bytes(n.pub().x, n.pub().x + 32));

  h.cmd(n, 0x55, 13, {}, 1000);
  h.cmd(n, host::kOpWipe, 14, {'n', 'o'}, 1000);
  h.drain(n);
  acks = h.events(host::kEvAck);
  CHECK(acks.size() == 2 && acks[0][3] == host::kAckUnknownOp && acks[1][3] == host::kAckBadArgs);
  CHECK(r.board.wipes == 0 && !n.reboot_requested());
  h.cmd(n, host::kOpWipe, 15, {'W', 'I', 'P', 'E'}, 1000);
  h.drain(n);
  CHECK(r.board.wipes == 1 && n.reboot_requested());

  // Garbage on the stream is counted, not fatal.
  Rig g(2);
  uint8_t junk[] = {1, 2, 3, 0, 9, 9, 0, 0};
  g.node->host_rx(junk, sizeof junk, 0);
  CHECK(g.node->stats().host_bad_frames == 2);
  g.host.cmd(*g.node, host::kOpGetStatus, 1, {}, 0);
  g.host.drain(*g.node);
  CHECK(g.host.events(host::kEvStatus).size() == 1);
}

void announces() {
  Rig r(3);
  auto& n = *r.node;
  Air air;
  std::vector<mesh::node::Node*> one = {&n};
  air.run(one, 60000);
  CHECK(air.frames == 0 && n.stats().announces == 0);  // no clock yet: no announce

  // The host tells the time: announce 5–30 s later (random, so nodes started
  // together do not collide), queued for up to 10 s more, then every 30 min ±10 %.
  r.host.cmd(n, host::kOpHello, 1, be32(1767225600), air.now);
  Rig rx(4);
  std::vector<mesh::node::Node*> two = {&n, rx.node.get()};
  air.run(two, air.now + 4000);
  CHECK(n.stats().announces == 0 && air.frames == 0);
  air.run(two, air.now + 41000);
  CHECK(n.stats().announces == 1 && air.frames >= 1 && n.unix_time(air.now) >= 1767225600 + 44);
  // The receiver stored the announce; it verifies and comes from n.
  rx.host.cmd(*rx.node, host::kOpHello, 1, {}, air.now);
  rx.host.drain(*rx.node);
  auto pk = rx.host.packets();
  CHECK(pk.size() == 1);
  mesh::Packet p;
  mesh::Announce a;
  CHECK(mesh::decode(pk[0].data(), pk[0].size(), p) == mesh::Err::Ok && p.type == mesh::Type::Announce);
  CHECK(mesh::parse_announce(p, a) == mesh::AnnounceErr::Ok);
  CHECK(std::memcmp(a.pub.ed, n.pub().ed, 32) == 0 && (a.app.flags & mesh::kFlagNode) && a.time >= 1767225600);
  air.run(two, air.now + 26 * 60000);
  CHECK(n.stats().announces == 1);
  air.run(two, air.now + 8 * 60000);
  CHECK(n.stats().announces == 2);
  // Announces off.
  r.host.cmd(n, host::kOpSetConfig, 2, {host::kTagAnnounceMin, 2, 0, 0}, air.now);
  air.run(two, air.now + 61 * 60000);
  CHECK(n.stats().announces == 2);
}

void relay_through_nodes() {
  Rig a(5), b(6), c(7);
  std::vector<mesh::node::Node*> nodes = {a.node.get(), b.node.get(), c.node.get()};
  Air air;
  a.host.cmd(*a.node, host::kOpHello, 1, {}, 0);
  b.host.cmd(*b.node, host::kOpHello, 1, {}, 0);
  a.host.drain(*a.node);
  b.host.drain(*b.node);
  a.host.got.clear();
  b.host.got.clear();

  // Host A sends a packet; B's host gets it; C has no host and stores it.
  Bytes d = data_packet(0x10);
  a.host.send(*a.node, host::kKindPacket, d, 0);
  air.run(nodes, 30000);
  b.host.drain(*b.node);
  auto got = b.host.packets();
  CHECK(got.size() == 1 && got[0] == d);
  CHECK(a.node->stats().host_packets_in == 1 && a.node->stats().host_packets_rejected == 0);
  // A does not echo its own packet back to its host.
  a.host.drain(*a.node);
  CHECK(a.host.packets().empty());
  CHECK(c.node->store().size() == 1);
  // C's host attaches later and gets the backlog.
  c.host.cmd(*c.node, host::kOpHello, 1, {}, air.now);
  c.host.drain(*c.node);
  got = c.host.packets();
  CHECK(got.size() == 1 && got[0] == d);
  // Resync replays it.
  c.host.cmd(*c.node, host::kOpResync, 2, {}, air.now);
  c.host.drain(*c.node);
  CHECK(c.host.packets().size() == 1);

  // NO_LORA from the host is refused.
  a.host.send(*a.node, host::kKindPacket, data_packet(0x20, 0xd0, mesh::kCtxNoLoRa), air.now);
  CHECK(a.node->stats().host_packets_rejected == 1);

  // Inflight hand-back: emitted to the output buffer but the host left
  // before reading it → delivered on the next attach.
  b.node->host_detach();
  b.host = Host();
  b.host.cmd(*b.node, host::kOpHello, 1, {}, air.now);  // reattach, nothing pending
  b.host.drain(*b.node);
  b.host.got.clear();
  Bytes d2 = data_packet(0x30);
  a.host.send(*a.node, host::kKindPacket, d2, air.now);
  air.run(nodes, air.now + 30000);
  CHECK(b.node->host_pending() > 0);
  b.node->host_detach();
  b.host = Host();
  b.host.cmd(*b.node, host::kOpHello, 1, {}, air.now);
  b.host.drain(*b.node);
  got = b.host.packets();
  CHECK(got.size() == 1 && got[0] == d2);

  // Status counts the neighbours heard.
  CHECK(b.node->status(air.now).neighbours == 2);
}

void receipts_purge_store() {
  // Alice is a phone identity elsewhere in the mesh; node C stores data for
  // her, then hears her announce and her receipt and drops the data.
  Rig src(8), c(9);
  std::vector<mesh::node::Node*> nodes = {src.node.get(), c.node.get()};
  Air air;
  Bytes s = seed(50);
  mesh::Identity alice = mesh::Identity::from_seed(s.data());
  uint8_t dest[16];
  alice.pub().hash(dest);

  Bytes d = data_packet(0x40);
  std::memcpy(d.data() + 10, dest, 16);
  src.host.send(*src.node, host::kKindPacket, d, 0);
  air.run(nodes, 20000);
  CHECK(c.node->store().size() == 1);

  uint8_t abody[mesh::kMaxAnnounceBody];
  mesh::Packet an;
  uint8_t apid[8] = {0xaa, 1, 2, 3, 4, 5, 6, 7};
  mesh::AppData app;
  CHECK(mesh::build_announce(alice, 1767225600, app, 3, apid, abody, sizeof abody, an) == mesh::AnnounceErr::Ok);
  Bytes araw(an.size());
  size_t n = 0;
  mesh::encode(an, araw.data(), araw.size(), &n);
  src.host.send(*src.node, host::kKindPacket, araw, air.now);
  mesh::Packet dp;
  mesh::decode(d.data(), d.size(), dp);
  uint8_t rbody[mesh::kReceiptBody];
  mesh::Packet rc;
  CHECK(mesh::build_receipt(alice, dp.id, 3, rbody, sizeof rbody, rc) == mesh::AnnounceErr::Ok);
  Bytes rraw(rc.size());
  mesh::encode(rc, rraw.data(), rraw.size(), &n);
  air.run(nodes, air.now + 20000);
  src.host.send(*src.node, host::kKindPacket, rraw, air.now);
  air.run(nodes, air.now + 20000);

  c.host.cmd(*c.node, host::kOpHello, 1, {}, air.now);
  c.host.drain(*c.node);
  auto got = c.host.packets();
  // Receipt first, then the announce; the acknowledged data is gone.
  CHECKC(got.size() == 2 && got[0] == rraw && got[1] == araw, "n=" + std::to_string(got.size()) + " purged=" + std::to_string(c.node->router().stats().purged) + " stored=" + std::to_string(c.node->store().size()));
  CHECK(!c.node->store().has(mesh::route::Key::of(dp)));
}

void restore_after_reboot() {
  Rig a(10), b(11);
  std::vector<mesh::node::Node*> nodes = {a.node.get(), b.node.get()};
  Air air;
  for (uint8_t i = 0; i < 3; i++) a.host.send(*a.node, host::kKindPacket, data_packet(uint8_t(0x50 + i * 8), uint8_t(i)), 0);
  air.run(nodes, 30000);
  CHECK(b.node->store().size() == 3);
  // b reboots: a new Node restored from its flash still has the backlog.
  Bytes s = seed(11);
  auto b2 = std::make_unique<mesh::node::Node>(host::NodeConfig{}, s.data(), b.board, &b.flash, 77);
  size_t restored = 0;
  for (size_t i = 0; i < mesh::store::kSlots; i++)
    if (b.flash.live[i] && b2->restore(i, b.flash.rec[i], 0)) restored++;
  CHECK(restored == 3);
  Host h;
  h.cmd(*b2, host::kOpHello, 1, {}, 0);
  h.drain(*b2);
  CHECK(h.packets().size() == 3);
}

void invalid_config_falls_back() {
  host::NodeConfig bad;
  bad.region = 99;
  Rig r(12, bad);
  CHECK(r.node->config().region == 0 && r.node->config().valid());
}

void fuzz() {
  std::mt19937 rng(5);
  Rig r(13);
  auto& n = *r.node;
  Millis now = 0;
  for (int i = 0; i < 20000; i++) {
    now += rng() % 50;
    Bytes b(rng() % 300);
    for (auto& x : b) x = uint8_t(rng() % 4 == 0 ? 0 : rng());
    switch (rng() % 5) {
      case 0:
        n.host_rx(b.data(), b.size(), now);
        break;
      case 1:
        n.radio_rx(b.data(), b.size(), int(rng() % 30) - 20, now);
        break;
      case 2: {  // valid-looking command with random args
        Bytes args(b.begin(), b.begin() + long(std::min<size_t>(b.size(), 40)));
        uint8_t op = uint8_t(1 + rng() % 7);  // never wipe/reboot... except 7 (reboot) is harmless here
        r.host.cmd(n, op, uint8_t(i), args, now);
        break;
      }
      case 3: {
        mesh::mac::Action a = n.radio_next(now);
        if (a.kind == mesh::mac::Kind::Cad) n.radio_cad(rng() % 2, now);
        if (a.kind == mesh::mac::Kind::Transmit) n.radio_tx_done(now + a.air_ms);
        break;
      }
      default:
        if (rng() % 3 == 0) {
          uint8_t out[128];
          n.host_tx(out, sizeof out);
        }
        if (rng() % 50 == 0) n.host_detach();
        n.tick(now);
    }
    CHECK(n.host_pending() <= mesh::node::kHostOut);
  }
  CHECK(n.config().valid());
}

// USB and BLE hosts at once plus the internet uplink: whatever enters on
// one side reaches every other side exactly once.
void two_hosts_and_uplink() {
  Rig r(20), rx(21);
  auto& n = *r.node;
  Host usb, ble;
  ble.port = 1;
  usb.cmd(n, host::kOpHello, 1, be32(1767225600), 1000);
  ble.cmd(n, host::kOpHello, 1, {}, 1000);
  usb.drain(n);
  ble.drain(n);
  CHECK(usb.events(host::kEvStatus).size() == 1 && ble.events(host::kEvStatus).size() == 1);
  CHECK(n.host_attached(0) && n.host_attached(1) && n.any_host_attached());

  // USB → BLE + uplink + air, not back to USB.
  Bytes d1 = data_packet(0x10);
  usb.send(n, host::kKindPacket, d1, 1000);
  usb.drain(n);
  ble.drain(n);
  CHECK(usb.packets().empty());
  auto bp = ble.packets();
  CHECK(bp.size() == 1 && bp[0] == d1);
  uint8_t body[1024];
  uint32_t seqs[8];
  size_t k = 0;
  size_t bn = n.uplink_take(body, sizeof body, seqs, 8, &k);
  CHECK(k == 1 && bn == 4 + d1.size() && body[3] == d1.size() && Bytes(body + 4, body + bn) == d1);
  // Upload failed: handed back, taken again; then nothing left.
  n.uplink_return(seqs, k);
  CHECK(n.uplink_take(body, sizeof body, seqs, 8, &k) == bn && k == 1);
  CHECK(n.uplink_take(body, sizeof body, seqs, 8, &k) == 0 && k == 0);

  // Uplink → both hosts and the air, not back to the uplink; once only.
  Bytes d2 = data_packet(0x20, 0xa0);
  CHECK(n.uplink_rx(d2.data(), d2.size(), 2000));
  CHECK(!n.uplink_rx(d2.data(), d2.size(), 2000));
  usb.drain(n);
  ble.drain(n);
  auto up = usb.packets(), bl = ble.packets();
  CHECK(up.size() == 1 && up[0] == d2 && bl.size() == 1 && bl[0] == d2);
  CHECK(n.uplink_take(body, sizeof body, seqs, 8, &k) == 0);
  Air air;
  air.now = 2000;
  std::vector<mesh::node::Node*> two = {&n, rx.node.get()};
  air.run(two, 20000);
  rx.host.cmd(*rx.node, host::kOpHello, 1, {}, air.now);
  rx.host.drain(*rx.node);
  int heard_d1 = 0, heard_d2 = 0;
  for (auto& p : rx.host.packets()) {
    mesh::Packet q;
    if (mesh::decode(p.data(), p.size(), q) != mesh::Err::Ok) continue;
    heard_d1 += q.id[0] == 0x10;
    heard_d2 += q.id[0] == 0x20 && q.hop_count == 1;  // another tier: hop counted
  }
  CHECK(heard_d1 == 1 && heard_d2 == 1);

  // Air → both hosts and the uplink.
  Bytes d3 = data_packet(0x30, 0xb0);
  rx.host.send(*rx.node, host::kKindPacket, d3, air.now);
  air.run(two, air.now + 20000);
  usb.drain(n);
  ble.drain(n);
  CHECK(usb.packets().size() == 1 && ble.packets().size() == 1);
  CHECK(n.uplink_take(body, sizeof body, seqs, 8, &k) > 0 && k == 1);

  // An identity announced over BLE is served by the gateway fetch.
  Bytes s = seed(99);
  mesh::Identity id = mesh::Identity::from_seed(s.data());
  mesh::AppData app;
  uint8_t pid[8] = {1, 2, 3, 4, 5, 6, 7, 8}, abody[mesh::kMaxAnnounceBody];
  mesh::Packet ap;
  CHECK(mesh::build_announce(id, 1767225700, app, 3, pid, abody, sizeof abody, ap) == mesh::AnnounceErr::Ok);
  Bytes araw(ap.size());
  size_t an = 0;
  mesh::encode(ap, araw.data(), araw.size(), &an);
  ble.send(n, host::kKindPacket, araw, air.now);
  uint8_t served[4][16];
  CHECK(n.served(served, 4) == 1 && std::memcmp(served[0], ap.dest, 16) == 0);
  usb.drain(n);
  CHECK(usb.packets().size() == 1);

  // BLE drops mid-stream: what it did not receive comes again on reconnect.
  Bytes d4 = data_packet(0x40, 0xc0);
  usb.send(n, host::kKindPacket, d4, air.now);
  n.host_detach(1);
  CHECK(!n.host_attached(1) && n.host_attached(0));
  ble = Host();
  ble.port = 1;
  ble.cmd(n, host::kOpHello, 2, {}, air.now);
  ble.drain(n);
  auto again = ble.packets();
  CHECK(again.size() == 1 && again[0] == d4);
  // Resync on one port leaves the other alone.
  ble.cmd(n, host::kOpResync, 3, {}, air.now);
  ble.drain(n);
  usb.drain(n);
  CHECK(ble.packets().size() >= 4 && usb.packets().empty());
}

// Phones in the Bluetooth mesh: hello, then len3 || packet both ways, cut
// into MTU-sized pieces; a second phone gets what the first sent.
Bytes mesh_frames(const std::vector<Bytes>& pkts) {
  Bytes out;
  for (auto& p : pkts) {
    out.push_back(uint8_t(p.size() >> 16));
    out.push_back(uint8_t(p.size() >> 8));
    out.push_back(uint8_t(p.size()));
    out.insert(out.end(), p.begin(), p.end());
  }
  return out;
}

std::vector<Bytes> mesh_split(const Bytes& stream) {
  std::vector<Bytes> out;
  size_t i = 0;
  while (i + 3 <= stream.size()) {
    size_t n = size_t(stream[i]) << 16 | size_t(stream[i + 1]) << 8 | stream[i + 2];
    out.emplace_back(stream.begin() + long(i + 3), stream.begin() + long(i + 3 + n));
    i += 3 + n;
  }
  CHECK(i == stream.size());
  return out;
}

Bytes mesh_drain(mesh::blemesh::Peer& p, mesh::node::Node& n, size_t port) {
  Bytes got;
  uint8_t buf[20];  // a 23-byte MTU
  while (size_t k = p.tx(n, port, buf, sizeof buf)) got.insert(got.end(), buf, buf + k);
  return got;
}

void ble_mesh_peers() {
  Rig r(32);
  auto& n = *r.node;
  const size_t pa = mesh::node::Node::kMeshPort0, pb = pa + 1;
  mesh::blemesh::Peer a, b;
  Bytes junk = {1, 2, 3};
  CHECK(!a.rx(n, pa, junk.data(), junk.size(), 1000));  // not a hello
  uint8_t hello[] = {mesh::blemesh::kHelloMagic, 9, 9, 9, 9};
  CHECK(a.rx(n, pa, hello, sizeof hello, 1000) && a.up() && n.host_attached(pa));
  CHECK(b.rx(n, pb, hello, sizeof hello, 1000));

  // Phone A sends two packets in 7-byte pieces, plus a media frame too big for LoRa.
  Bytes d1 = data_packet(0x31), d2 = data_packet(0x32);
  Bytes big(3000, 0x55);
  Bytes stream = mesh_frames({d1, big, d2});
  for (size_t i = 0; i < stream.size(); i += 7) {
    size_t k = std::min<size_t>(7, stream.size() - i);
    CHECK(a.rx(n, pa, stream.data() + i, k, 1000));
  }
  // Phone B gets both LoRa-sized packets (not the media frame), A gets nothing back.
  auto got = mesh_split(mesh_drain(b, n, pb));
  CHECK(got.size() == 2 && got[0] == d1 && got[1] == d2);
  CHECK(mesh_drain(a, n, pa).empty());
  // And a USB host sees them too; its own packet reaches both phones.
  Host usb;
  usb.cmd(n, host::kOpHello, 1, {}, 1000);
  usb.drain(n);
  auto up = usb.packets();
  CHECK(up.size() == 2);
  Bytes d3 = data_packet(0x33);
  usb.send(n, host::kKindPacket, d3, 1000);
  got = mesh_split(mesh_drain(a, n, pa));
  CHECK(got.size() == 1 && got[0] == d3);
  CHECK(mesh_split(mesh_drain(b, n, pb)).size() == 1);

  // A seat where the board connected out (another board): no hello from the
  // other side, the stream starts at once.
  mesh::blemesh::Peer c;
  const size_t pc = pa + 2;
  c.start(n, pc, 1000);
  CHECK(c.up() && n.host_attached(pc));
  got = mesh_split(mesh_drain(c, n, pc));
  CHECK(got.size() == 3);  // d1, d2, d3: everything stored
  Bytes d4 = data_packet(0x34);
  Bytes s4 = mesh_frames({d4});
  CHECK(c.rx(n, pc, s4.data(), s4.size(), 1000));
  CHECK(mesh_split(mesh_drain(a, n, pa)).size() == 1);

  // Untrusted: no WIPE through a mesh seat. A zero length frame is corrupt.
  uint8_t zero[] = {0, 0, 0};
  CHECK(!a.rx(n, pa, zero, sizeof zero, 1000));
  CHECK(r.board.wipes == 0);
}

// The board as a relay between all layers: a packet from a Bluetooth phone
// goes out on LoRa, to the relay and to the other phones; one heard on LoRa
// reaches the phones and the relay; one from the relay reaches the phones and LoRa.
void all_layers() {
  Rig r(33), far(34);
  auto& n = *r.node;
  const size_t pa = mesh::node::Node::kMeshPort0, pb = pa + 1;
  mesh::blemesh::Peer a, b;
  uint8_t hello[] = {mesh::blemesh::kHelloMagic, 1, 2, 3, 4};
  CHECK(a.rx(n, pa, hello, sizeof hello, 1000) && b.rx(n, pb, hello, sizeof hello, 1000));
  // The handheld's own messenger (its display), as ui/client_messenger.cpp attaches it.
  Host local;
  local.port = mesh::node::Node::kLocalPort;
  local.cmd(n, host::kOpHello, 0, {}, 1000);
  auto local_has = [&](const Bytes& d) {
    local.drain(n, 64);
    bool hit = false;
    for (auto& p : local.packets()) hit |= p == d;
    return hit;
  };
  Air air;
  air.now = 1000;
  std::vector<mesh::node::Node*> both = {&n, far.node.get()};
  uint8_t body[2048];
  uint32_t seqs[8];
  size_t k = 0;

  // Bluetooth → LoRa + relay + other phone.
  Bytes d1 = data_packet(0x41);
  Bytes s1 = mesh_frames({d1});
  CHECK(a.rx(n, pa, s1.data(), s1.size(), air.now));
  air.run(both, air.now + 20000);
  far.host.cmd(*far.node, host::kOpHello, 1, {}, air.now);
  far.host.drain(*far.node);
  bool on_air = false;
  for (auto& p : far.host.packets()) on_air |= p == d1;
  CHECK(on_air);
  size_t bn = n.uplink_take(body, sizeof body, seqs, 8, &k);
  CHECK(k == 1 && Bytes(body + 4, body + bn) == d1);
  auto got = mesh_split(mesh_drain(b, n, pb));
  CHECK(got.size() == 1 && got[0] == d1);
  CHECK(local_has(d1));

  // LoRa → both phones + relay.
  Bytes d2 = data_packet(0x42);
  far.host.send(*far.node, host::kKindPacket, d2, air.now);
  air.run(both, air.now + 20000);
  CHECK(mesh_split(mesh_drain(a, n, pa)).size() == 1);
  CHECK(mesh_split(mesh_drain(b, n, pb)).size() == 1);
  bn = n.uplink_take(body, sizeof body, seqs, 8, &k);
  CHECK(k == 1 && Bytes(body + 4, body + bn) == d2);
  CHECK(local_has(d2));

  // Relay → both phones + LoRa.
  Bytes d3 = data_packet(0x43);
  CHECK(n.uplink_rx(d3.data(), d3.size(), air.now));
  CHECK(mesh_split(mesh_drain(a, n, pa)).size() == 1 && mesh_split(mesh_drain(b, n, pb)).size() == 1);
  CHECK(local_has(d3));
  air.run(both, air.now + 20000);
  far.host.drain(*far.node);
  on_air = false;  // the router counts the hop into LoRa: same packet ID, hop count + 1
  mesh::Packet want, got3;
  CHECK(mesh::decode(d3.data(), d3.size(), want) == mesh::Err::Ok);
  for (auto& p : far.host.packets())
    on_air |= mesh::decode(p.data(), p.size(), got3) == mesh::Err::Ok && std::memcmp(got3.id, want.id, 8) == 0;
  CHECK(on_air);

  // The handheld's own message → both phones + LoRa + relay: it is a gateway
  // like any phone or meshd, not only a modem.
  Bytes d4 = data_packet(0x44);
  local.send(n, host::kKindPacket, d4, air.now);
  auto ga = mesh_split(mesh_drain(a, n, pa)), gb = mesh_split(mesh_drain(b, n, pb));
  CHECK(ga.size() == 1 && ga[0] == d4 && gb.size() == 1 && gb[0] == d4);
  bn = n.uplink_take(body, sizeof body, seqs, 8, &k);
  CHECK(k == 1 && Bytes(body + 4, body + bn) == d4);
  air.run(both, air.now + 20000);
  far.host.drain(*far.node);
  on_air = false;
  for (auto& p : far.host.packets()) on_air |= p == d4;
  CHECK(on_air);
}

// SET_WIFI / HOTSPOT from a trusted host reach the board; bad arguments and
// boards without Wi-Fi are answered with an ack status.
void wifi_commands() {
  Rig r(31);
  auto& n = *r.node;
  Host usb;
  Bytes tlv = {host::kWifiTagSsid, 4, 'h', 'o', 'm', 'e', host::kWifiTagPass, 8, 's', 'e', 'c', 'r', 'e', 't', '1', '2'};
  usb.cmd(n, host::kOpSetWifi, 1, tlv, 1000);
  usb.cmd(n, host::kOpSetWifi, 2, {host::kWifiTagPass, 3, 'a', 'b', 'c'}, 1000);           // WPA2 needs 8+
  usb.cmd(n, host::kOpSetWifi, 3, {host::kWifiTagRelay, 5, 'f', 't', 'p', ':', '/'}, 1000);  // not http(s)
  usb.cmd(n, host::kOpSetWifi, 4, {host::kWifiTagSsid, 3, 'a', 0, 'b'}, 1000);              // NUL
  usb.cmd(n, host::kOpSetWifi, 5, {host::kWifiTagSsid, 1, 'a', host::kWifiTagSsid, 1, 'b'}, 1000);  // twice
  usb.cmd(n, host::kOpHotspot, 6, {1}, 1000);
  usb.cmd(n, host::kOpHotspot, 7, {2}, 1000);
  usb.drain(n);
  auto acks = usb.events(host::kEvAck);
  CHECK(acks.size() == 7);
  CHECK(acks[0][2] == host::kOpSetWifi && acks[0][3] == host::kAckOk);
  for (int i = 1; i <= 4; i++) CHECK(acks[i][3] == host::kAckBadArgs);
  CHECK(acks[5][2] == host::kOpHotspot && acks[5][3] == host::kAckOk && acks[6][3] == host::kAckBadArgs);
  CHECK(r.board.wifi_sets == 1 && std::string(r.board.wifi.ssid) == "home" && std::string(r.board.wifi.pass) == "secret12");
  CHECK(r.board.wifi.has_ssid && r.board.wifi.has_pass && !r.board.wifi.has_relay);
  CHECK(r.board.hotspots == 1 && r.board.hotspot_on);

  // Round trip of the TLV codec, and an empty SSID (forget the network).
  host::WifiConfig w;
  CHECK(w.parse(tlv.data(), tlv.size()));
  uint8_t out[host::kMaxWifiTLV];
  size_t on = w.encode(out, sizeof out);
  CHECK(Bytes(out, out + on) == tlv);
  host::WifiConfig e;
  uint8_t forget[] = {host::kWifiTagSsid, 0};
  CHECK(e.parse(forget, sizeof forget) && e.has_ssid && e.ssid[0] == 0);

  // SCREEN / INPUT go to the board; board events reach the host.
  usb.cmd(n, host::kOpScreen, 20, {1}, 1000);
  usb.cmd(n, host::kOpInput, 21, {0, 0, 10, 0, 20, 1}, 1000);
  usb.drain(n);
  acks = usb.events(host::kEvAck);
  CHECK(acks.size() == 2 && acks[0][3] == host::kAckOk && acks[1][3] == host::kAckOk);
  CHECK(r.board.board_cmds.size() == 2 && r.board.board_cmds[0].first == host::kOpScreen);
  uint8_t tile[] = {1, 2, 3};
  CHECK(n.host_room(0) == mesh::node::kHostOut);
  CHECK(n.host_event(0, host::kEvScreen, tile, sizeof tile));
  CHECK(n.host_room(0) < mesh::node::kHostOut);
  CHECK(!n.host_event(1, host::kEvScreen, tile, sizeof tile));  // BLE not attached
  usb.drain(n);
  auto scr = usb.events(host::kEvScreen);
  CHECK(scr.size() == 1 && scr[0] == Bytes({host::kEvScreen, 0, 1, 2, 3}));

  // A node without Wi-Fi says so.
  r.board.has_wifi = false;
  usb.cmd(n, host::kOpHotspot, 8, {1}, 1000);
  usb.drain(n);
  acks = usb.events(host::kEvAck);
  CHECK(acks.size() == 1 && acks[0][3] == host::kAckUnknownOp);
}

// The web port (anyone on the Wi-Fi) exchanges packets and reads status but
// cannot change, reboot or wipe the node.
void web_port_untrusted() {
  Rig r(30);
  auto& n = *r.node;
  Host web, usb;
  web.port = mesh::node::Node::kWebPort;
  web.cmd(n, host::kOpHello, 1, be32(1767225600), 1000);
  web.cmd(n, host::kOpWipe, 2, {'W', 'I', 'P', 'E'}, 1000);
  web.cmd(n, host::kOpSetConfig, 3, {host::kTagAnnounceMin, 2, 0, 0}, 1000);
  web.cmd(n, host::kOpReboot, 4, {}, 1000);
  web.cmd(n, host::kOpSetWifi, 5, {host::kWifiTagSsid, 1, 'x'}, 1000);
  web.cmd(n, host::kOpHotspot, 6, {1}, 1000);
  web.drain(n);
  auto acks = web.events(host::kEvAck);
  CHECK(web.events(host::kEvStatus).size() == 1 && acks.size() == 5);
  for (auto& a : acks) CHECK(a.size() == 4 && a[3] == host::kAckNotAllowed);
  CHECK(r.board.wipes == 0 && r.board.saves == 0 && !n.reboot_requested());
  CHECK(r.board.wifi_sets == 0 && r.board.hotspots == 0);
  // Packets still flow both ways.
  Bytes d = data_packet(0x51);
  web.send(n, host::kKindPacket, d, 1000);
  usb.cmd(n, host::kOpHello, 1, {}, 1000);
  usb.drain(n);
  auto got = usb.packets();
  CHECK(got.size() == 1 && got[0] == d);
}

}  // namespace

void run_node() {
  web_port_untrusted();
  wifi_commands();
  ble_mesh_peers();
  all_layers();
  two_hosts_and_uplink();
  commands();
  announces();
  relay_through_nodes();
  receipts_purge_store();
  restore_after_reboot();
  invalid_config_falls_back();
  fuzz();
}

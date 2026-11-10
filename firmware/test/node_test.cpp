// The node application end to end on the host: host commands and events,
// own announces, store-and-forward to the host, inflight hand-back, receipt
// purging, and nodes passing a host's packet over a simulated radio.
#include <memory>
#include <random>
#include <vector>

#include "announce.h"
#include "check.h"
#include "hostlink.h"
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

  void send(mesh::node::Node& n, uint8_t kind, const Bytes& payload, Millis now) {
    uint8_t buf[host::kMaxEncoded];
    size_t e = host::encode(kind, payload.data(), payload.size(), buf, sizeof buf);
    n.host_rx(buf, e, now);
  }
  void cmd(mesh::node::Node& n, uint8_t op, uint8_t req, const Bytes& args, Millis now) {
    Bytes p = {op, req};
    p.insert(p.end(), args.begin(), args.end());
    send(n, host::kKindCommand, p, now);
  }
  void drain(mesh::node::Node& n, size_t chunk = 20) {
    uint8_t buf[64];
    for (;;) {
      size_t k = n.host_tx(buf, chunk < sizeof buf ? chunk : sizeof buf);
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

}  // namespace

void run_node() {
  commands();
  announces();
  relay_through_nodes();
  receipts_purge_store();
  restore_after_reboot();
  invalid_config_falls_back();
  fuzz();
}

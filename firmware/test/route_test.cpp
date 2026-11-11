// Managed-flood router: dedup, SNR-ordered rebroadcast, cancellation, hop
// limit, implicit-ACK retries, receipt purging (run under ASan/UBSan).
#include <memory>
#include <random>

#include "announce.h"
#include "check.h"
#include "identity.h"
#include "packet.h"
#include "router.h"

using check::Bytes;
using mesh::route::Frame;
using mesh::route::Key;
using mesh::route::Millis;
using mesh::route::Priority;
using mesh::route::Router;
using mesh::route::Verdict;

namespace {

constexpr Millis kHour = 3600ull * 1000;

mesh::route::Config quiet() {  // deterministic delays
  mesh::route::Config c;
  c.short_id = 0x42;
  c.delay_jitter = 0;
  c.ack_jitter = 0;
  return c;
}

Bytes packet(mesh::Type t, uint8_t idbyte, uint8_t hop = 0, uint8_t max_hops = 3, uint8_t ctx = 0,
             size_t body = 20) {
  static uint8_t buf[300];
  for (size_t i = 0; i < body; i++) buf[i] = uint8_t(i * 7);
  mesh::Packet p;
  p.type = t;
  p.ctx = ctx;
  p.hop_count = hop;
  p.max_hops = max_hops;
  for (int i = 0; i < 8; i++) p.id[i] = uint8_t(idbyte + i);
  for (int i = 0; i < 16; i++) p.dest[i] = uint8_t(0xd0 + i);
  p.body = buf;
  p.body_len = body;
  Bytes out(p.size() + 4);
  size_t n = 0;
  if (mesh::encode(p, out.data(), out.size(), &n) != mesh::Err::Ok) return {};
  out.resize(n);
  return out;
}

Bytes frame(const Bytes& raw, uint8_t sender = 7) {
  Bytes f(raw.size() + 1);
  size_t n = 0;
  if (mesh::encode_link_frame(sender, raw.data(), raw.size(), f.data(), f.size(), &n) != mesh::Err::Ok) return {};
  f.resize(n);
  return f;
}

Bytes enc(const mesh::Packet& p) {
  Bytes out(p.size());
  size_t n = 0;
  if (mesh::encode(p, out.data(), out.size(), &n) != mesh::Err::Ok) return {};
  out.resize(n);
  return out;
}

mesh::route::RxResult rx(Router& r, const Bytes& raw, int snr, Millis now) {
  Bytes f = frame(raw);
  return r.on_receive(f.data(), f.size(), snr, now);
}

Key key_of(const Bytes& raw) {
  mesh::Packet p;
  mesh::decode(raw.data(), raw.size(), p);
  return Key::of(p);
}

void seen_cache() {
  mesh::route::SeenCache c(10 * 1000);
  Key a, b;
  a.id[0] = 1;
  b.id[0] = 1;
  b.frag = 1;
  c.insert(a, 0);
  CHECK(c.contains(a, 0) && !c.contains(b, 0));
  c.insert(a, 5000);  // no refresh: still expires 10 s after first sight
  CHECK(c.size() == 1);
  CHECK(c.contains(a, 9999) && !c.contains(a, 10000));
  c.expire(10000);
  CHECK(c.size() == 0);

  // Capacity: oldest evicted first.
  for (size_t i = 0; i < mesh::route::kSeenCapacity + 5; i++) {
    Key k;
    std::memcpy(k.id, &i, sizeof i < 8 ? sizeof i : 8);
    c.insert(k, 1000);
  }
  CHECK(c.size() == mesh::route::kSeenCapacity);
  Key first, last;
  size_t i0 = 0, il = mesh::route::kSeenCapacity + 4;
  std::memcpy(first.id, &i0, sizeof i0 < 8 ? sizeof i0 : 8);
  std::memcpy(last.id, &il, sizeof il < 8 ? sizeof il : 8);
  CHECK(!c.contains(first, 1000) && c.contains(last, 1000));
}

void flood_basics() {
  auto r = std::make_unique<Router>(quiet());
  Bytes d = packet(mesh::Type::Data, 0x10, 1);
  auto res = rx(*r, d, 0, 1000);
  CHECK(res.verdict == Verdict::New && res.scheduled && res.sender == 7);
  CHECK(res.packet.type == mesh::Type::Data && res.packet.hop_count == 1);
  CHECK(r->pending() == 1);

  // Not due yet; next_due reports when.
  Frame f;
  Millis next = 0;
  CHECK(!r->poll(1000, f, &next));
  CHECK(next == 1000 + 200 + 3000 * 20 / 30);  // snr 0 → 2/3 of the window

  CHECK(r->poll(next, f, &next));
  CHECK(next == mesh::route::kNever && r->pending() == 0);
  CHECK(f.prio == Priority::Data && f.key == key_of(d));
  uint8_t sender = 0;
  const uint8_t* raw = nullptr;
  size_t rn = 0;
  CHECK(mesh::decode_link_frame(f.data, f.len, &sender, &raw, &rn) == mesh::Err::Ok);
  mesh::Packet q;
  CHECK(sender == 0x42 && mesh::decode(raw, rn, q) == mesh::Err::Ok && q.hop_count == 2);
  // Only the hop nibble changed.
  Bytes want = d;
  want[1] = uint8_t((2 << 4) | 3);
  CHECK(Bytes(raw, raw + rn) == want);

  // Heard again later: duplicate, not rebroadcast again.
  res = rx(*r, d, 0, 9000);
  CHECK(res.verdict == Verdict::Duplicate && !res.scheduled && r->pending() == 0);

  // A copy with a different hop count is the same packet.
  res = rx(*r, packet(mesh::Type::Data, 0x10, 2), 0, 9000);
  CHECK(res.verdict == Verdict::Duplicate);

  // Garbage.
  uint8_t junk[3] = {1, 2, 3};
  CHECK(r->on_receive(junk, sizeof junk, 0, 9000).verdict == Verdict::Invalid);
  CHECK(r->on_receive(junk, 0, 0, 9000).verdict == Verdict::Invalid);
  CHECK(r->stats().invalid == 2 && r->stats().dup == 2 && r->stats().fresh == 1 && r->stats().sent == 1);

  // Forgotten after the seen TTL: heard again it is new.
  r->tick(1000 + 24 * kHour);
  CHECK(r->seen().size() == 0);
  CHECK(rx(*r, d, 0, 1000 + 24 * kHour).verdict == Verdict::New);
}

void snr_ordering_and_roles() {
  auto r = std::make_unique<Router>(quiet());
  Frame f;
  Millis weak = 0, strong = 0, mid = 0;
  rx(*r, packet(mesh::Type::Data, 0x20), -30, 0);  // below snr_low: clamps to min delay
  r->poll(0, f, &weak);
  CHECK(weak == 200);
  CHECK(r->poll(weak, f));
  rx(*r, packet(mesh::Type::Data, 0x30), 25, 0);  // above snr_high: full window
  r->poll(0, f, &strong);
  CHECK(strong == 3200);
  CHECK(r->poll(strong, f));
  rx(*r, packet(mesh::Type::Data, 0x40), -5, 0);
  r->poll(0, f, &mid);
  CHECK(weak < mid && mid < strong && mid == 200 + 1500);

  auto cfg = quiet();
  cfg.role = mesh::route::Role::Client;
  auto c = std::make_unique<Router>(cfg);
  Millis cl = 0;
  rx(*c, packet(mesh::Type::Data, 0x20), -30, 0);
  c->poll(0, f, &cl);
  CHECK(cl == 200 + 3000);  // clients wait for repeaters

  // Jitter stays within its bound and differs between seeds.
  auto j = quiet();
  j.delay_jitter = 500;
  Millis seen_min = mesh::route::kNever, seen_max = 0;
  for (uint32_t seed = 1; seed < 50; seed++) {
    j.seed = seed;
    auto jr = std::make_unique<Router>(j);
    rx(*jr, packet(mesh::Type::Data, 0x50), -30, 0);
    Millis due = 0;
    jr->poll(0, f, &due);
    CHECK(due >= 200 && due < 700);
    if (due < seen_min) seen_min = due;
    if (due > seen_max) seen_max = due;
  }
  CHECK(seen_max > seen_min);
}

void cancellation() {
  auto r = std::make_unique<Router>(quiet());
  Bytes d = packet(mesh::Type::Data, 0x10);
  rx(*r, d, 0, 0);
  rx(*r, d, 0, 100);  // one neighbour rebroadcast: keep ours (cancel_after = 2)
  CHECK(r->pending() == 1);
  rx(*r, d, 0, 200);
  CHECK(r->pending() == 0 && r->stats().cancelled == 1);
  Frame f;
  CHECK(!r->poll(kHour, f));

  auto cfg = quiet();
  cfg.cancel_after = 1;
  auto r1 = std::make_unique<Router>(cfg);
  rx(*r1, d, 0, 0);
  rx(*r1, d, 0, 100);
  CHECK(r1->pending() == 0);
}

void hop_limit_and_flags() {
  auto r = std::make_unique<Router>(quiet());
  // Already at its limit: delivered locally, never rebroadcast.
  auto res = rx(*r, packet(mesh::Type::Data, 0x10, 3, 3), 0, 0);
  CHECK(res.verdict == Verdict::New && !res.scheduled && r->stats().hop_limit == 1);
  // NO_LORA heard on air anyway: not rebroadcast.
  res = rx(*r, packet(mesh::Type::Data, 0x20, 0, 3, mesh::kCtxNoLoRa), 0, 0);
  CHECK(res.verdict == Verdict::New && !res.scheduled);
  // LINK is hop-local and not deduplicated.
  Bytes link = packet(mesh::Type::Link, 0x30);
  CHECK(rx(*r, link, 0, 0).verdict == Verdict::Local);
  CHECK(rx(*r, link, 0, 0).verdict == Verdict::Local);
  // SUMMARY / PATH_REQ: new, for the caller, not flooded.
  res = rx(*r, packet(mesh::Type::Summary, 0x40), 0, 0);
  CHECK(res.verdict == Verdict::New && !res.scheduled);
  res = rx(*r, packet(mesh::Type::PathReq, 0x50), 0, 0);
  CHECK(res.verdict == Verdict::New && !res.scheduled);
  CHECK(r->pending() == 0);

  // Fragments of one ID are distinct packets.
  Bytes body(400, 0x5a);
  mesh::Packet big;
  big.type = mesh::Type::Data;
  big.max_hops = 3;
  big.id[0] = 0x77;
  big.body = body.data();
  big.body_len = body.size();
  size_t nf = mesh::fragment_count(big, mesh::kLoRaMTU);
  CHECK(nf == 2);
  for (size_t i = 0; i < nf; i++) {
    mesh::Packet fr;
    CHECK(mesh::fragment(big, mesh::kLoRaMTU, i, fr) == mesh::Err::Ok);
    res = rx(*r, enc(fr), 0, 0);
    CHECK(res.verdict == Verdict::New && res.scheduled);
  }
  CHECK(r->pending() == 2);
}

void priorities() {
  auto r = std::make_unique<Router>(quiet());
  uint8_t seed[64];
  for (int i = 0; i < 64; i++) seed[i] = uint8_t(i);
  const mesh::Identity id = mesh::Identity::from_seed(seed);
  const uint8_t acked[8] = {9, 9, 9, 9, 9, 9, 9, 9};
  uint8_t rbody[mesh::kReceiptBody];
  mesh::Packet rc;
  CHECK(mesh::build_receipt(id, acked, 3, rbody, sizeof rbody, rc) == mesh::AnnounceErr::Ok);

  // All heard at once from the same SNR: all due together.
  rx(*r, packet(mesh::Type::Data, 0x10), 0, 0);
  rx(*r, packet(mesh::Type::Data, 0x20, 0, 3, mesh::kCtxPriority), 0, 0);
  rx(*r, enc(rc), 0, 0);
  Frame f;
  Priority order[3];
  for (auto& o : order) {
    CHECK(r->poll(kHour, f));
    o = f.prio;
  }
  CHECK(order[0] == Priority::Receipt && order[1] == Priority::DataPriority && order[2] == Priority::Data);
}

void own_packets_and_retries() {
  auto r = std::make_unique<Router>(quiet());
  Bytes d = packet(mesh::Type::Data, 0x10, 0);
  CHECK(r->send(d.data(), d.size(), true, 0));
  CHECK(!r->send(d.data(), d.size(), true, 0));  // already queued / seen
  Frame f;
  CHECK(r->poll(0, f));  // immediately due
  mesh::Packet q;
  CHECK(mesh::decode(f.data + 1, f.len - 1, q) == mesh::Err::Ok && q.hop_count == 0);
  CHECK(!r->poll(kHour, f));  // waits for the MAC to report the transmission
  r->on_transmitted(f.key, 1000);
  Millis next = 0;
  CHECK(!r->poll(1000, f, &next) && next == 21000);
  CHECK(r->poll(21000, f) && r->stats().retries == 1);
  r->on_transmitted(f.key, 22000);
  CHECK(r->poll(42000, f) && r->stats().retries == 2);
  CHECK(r->pending() == 0 && r->stats().gave_up == 1 && r->stats().sent == 3);

  // A neighbour's rebroadcast is the implicit ACK.
  Bytes e = packet(mesh::Type::Data, 0x20, 0);
  CHECK(r->send(e.data(), e.size(), true, 0));
  CHECK(r->poll(0, f));
  r->on_transmitted(f.key, 500);
  auto res = rx(*r, packet(mesh::Type::Data, 0x20, 1), 0, 3000);
  CHECK(res.verdict == Verdict::Duplicate && r->pending() == 0 && r->stats().acks == 1);

  // Never transmitted (MAC dropped it): forgotten after handed_timeout.
  Bytes g = packet(mesh::Type::Data, 0x30, 0);
  CHECK(r->send(g.data(), g.size(), true, 0));
  CHECK(r->poll(0, f));
  r->tick(kHour - 1);
  CHECK(r->pending() == 1);
  r->tick(kHour);
  CHECK(r->pending() == 0);

  // From another tier (relay via gateway): hop incremented, no retries.
  Bytes up = packet(mesh::Type::Data, 0x40, 1);
  CHECK(r->send(up.data(), up.size(), false, 0));
  CHECK(r->poll(0, f) && r->pending() == 0);
  CHECK(mesh::decode(f.data + 1, f.len - 1, q) == mesh::Err::Ok && q.hop_count == 2);
  Bytes spent = packet(mesh::Type::Data, 0x50, 3, 3);
  CHECK(!r->send(spent.data(), spent.size(), false, 0));
  CHECK(r->send(spent.data(), spent.size(), true, 0));  // own: sent as is
  Bytes nolora = packet(mesh::Type::Data, 0x60, 0, 3, mesh::kCtxNoLoRa);
  CHECK(!r->send(nolora.data(), nolora.size(), true, 0));
  Bytes huge = packet(mesh::Type::Data, 0x70, 0, 3, 0, 240);
  CHECK(!r->send(huge.data(), huge.size(), true, 0));
  CHECK(!r->send(huge.data(), 3, true, 0));
}

void pending_full() {
  auto r = std::make_unique<Router>(quiet());
  for (size_t i = 0; i < mesh::route::kPendingCapacity; i++)
    CHECK(rx(*r, packet(mesh::Type::Data, uint8_t(i * 8)), 0, 0).scheduled);
  auto res = rx(*r, packet(mesh::Type::Data, 0xf0), 0, 0);
  CHECK(res.verdict == Verdict::New && !res.scheduled && r->stats().pending_full == 1);
  // Our own packet displaces a queued rebroadcast.
  Bytes own = packet(mesh::Type::Data, 0xf8, 0);
  CHECK(r->send(own.data(), own.size(), true, 0));
  CHECK(r->pending() == mesh::route::kPendingCapacity);
  Frame f;
  CHECK(r->poll(0, f) && f.key == key_of(own));
}

void announces_and_receipts() {
  auto r = std::make_unique<Router>(quiet());
  uint8_t seed[64];
  for (int i = 0; i < 64; i++) seed[i] = uint8_t(100 + i);
  const mesh::Identity alice = mesh::Identity::from_seed(seed);
  seed[40] ^= 1;  // Ed25519 half of the seed: a different signing key
  const mesh::Identity mallory = mesh::Identity::from_seed(seed);

  // Data for alice, queued for rebroadcast.
  Bytes d = packet(mesh::Type::Data, 0x10);
  mesh::Packet dp;
  CHECK(mesh::decode(d.data(), d.size(), dp) == mesh::Err::Ok);
  CHECK(rx(*r, d, 10, 0).scheduled);

  // Receipt from an identity we have no key for: forwarded, no purge.
  uint8_t rbody[mesh::kReceiptBody];
  mesh::Packet rc;
  CHECK(mesh::build_receipt(alice, dp.id, 3, rbody, sizeof rbody, rc) == mesh::AnnounceErr::Ok);
  Bytes rcraw = enc(rc);
  auto res = rx(*r, rcraw, 10, 0);
  CHECK(res.verdict == Verdict::New && res.scheduled && !res.purged && r->pending() == 2);

  // Learn alice's key from her announce (and forward it).
  uint8_t abody[mesh::kMaxAnnounceBody];
  mesh::Packet an;
  const uint8_t apid[8] = {0xa1, 2, 3, 4, 5, 6, 7, 8};
  mesh::AppData app;
  CHECK(mesh::build_announce(alice, 1000, app, 3, apid, abody, sizeof abody, an) == mesh::AnnounceErr::Ok);
  Bytes anraw = enc(an);
  res = rx(*r, anraw, 10, 0);
  CHECK(res.verdict == Verdict::New && res.scheduled && r->keys().size() == 1);

  // A tampered announce is dropped and does not shadow the real one.
  auto r2 = std::make_unique<Router>(quiet());
  Bytes bad = anraw;
  bad[mesh::kHeaderSize + 66] ^= 1;
  CHECK(rx(*r2, bad, 0, 0).verdict == Verdict::Invalid && r2->keys().size() == 0);
  CHECK(rx(*r2, anraw, 0, 0).verdict == Verdict::New && r2->keys().size() == 1);

  // Second data packet for alice, then her receipt for it: verified → purged.
  Bytes d2 = packet(mesh::Type::Data, 0x20);
  mesh::Packet dp2;
  CHECK(mesh::decode(d2.data(), d2.size(), dp2) == mesh::Err::Ok);
  CHECK(rx(*r, d2, 10, 0).scheduled);
  size_t before = r->pending();

  // Forged receipt (mallory signs as alice's destination): invalid.
  mesh::Packet forged;
  uint8_t fbody[mesh::kReceiptBody];
  CHECK(mesh::build_receipt(mallory, dp2.id, 3, fbody, sizeof fbody, forged) == mesh::AnnounceErr::Ok);
  std::memcpy(forged.dest, an.dest, 16);
  res = rx(*r, enc(forged), 10, 0);
  CHECKC(res.verdict == Verdict::Invalid && r->pending() == before, std::string(mesh::route::verdict_name(res.verdict)) + " pending=" + std::to_string(r->pending()) + " before=" + std::to_string(before));

  mesh::Packet rc2;
  uint8_t rbody2[mesh::kReceiptBody];
  CHECK(mesh::build_receipt(alice, dp2.id, 3, rbody2, sizeof rbody2, rc2) == mesh::AnnounceErr::Ok);
  res = rx(*r, enc(rc2), 10, 0);
  CHECK(res.verdict == Verdict::New && res.purged && res.scheduled);
  CHECK(r->pending() == before);  // d2 purged, receipt queued
  CHECK(r->stats().purged == 1);

  // Late copies (other fragments, other hop counts) of acked data are not forwarded.
  res = rx(*r, packet(mesh::Type::Data, 0x20, 1), 10, 0);
  CHECK(res.verdict == Verdict::Duplicate);
  mesh::Packet frag = dp2;
  frag.ctx = mesh::kCtxFragmented;
  frag.frag_index = 1;
  frag.frag_count = 2;
  res = rx(*r, enc(frag), 10, 0);
  CHECK(res.verdict == Verdict::Acked && !res.scheduled);

  // Malformed receipt (ID is not receipt_id(acked)).
  mesh::Packet mal = rc2;
  mal.id[0] ^= 1;
  CHECK(rx(*r, enc(mal), 10, 0).verdict == Verdict::Invalid);
}

void fuzz() {
  std::mt19937 rng(7);
  auto cfg = quiet();
  cfg.delay_jitter = 300;
  auto r = std::make_unique<Router>(cfg);
  Bytes base = packet(mesh::Type::Data, 0x10, 0, 5);
  Frame f;
  Millis now = 0;
  for (int i = 0; i < 50000; i++) {
    now += rng() % 400;
    Bytes b;
    switch (rng() % 3) {
      case 0: {  // random bytes
        b.resize(rng() % 256);
        for (auto& x : b) x = uint8_t(rng());
        r->on_receive(b.data(), b.size(), int(rng() % 40) - 25, now);
        break;
      }
      case 1: {  // mutated valid packet, few distinct IDs → many duplicates
        b = base;
        b[2] = uint8_t(rng() % 32);
        b[1] = uint8_t(((rng() % 6) << 4) | 5);
        b[0] = uint8_t(rng() % 6) << 5 | uint8_t(rng() % 8) << 2 | 1;
        if (rng() % 4 == 0) b[rng() % b.size()] = uint8_t(rng());
        rx(*r, b, int(rng() % 40) - 25, now);
        break;
      }
      default:
        while (r->poll(now, f)) {
          CHECK(f.len >= 1 + mesh::kHeaderSize && f.len <= mesh::kLoRaRadioMax);
          if (rng() % 2) r->on_transmitted(f.key, now);
        }
        if (rng() % 50 == 0) {
          b = base;
          b[2] = uint8_t(0x80 | (rng() % 64));
          r->send(b.data(), b.size(), rng() % 2, now);
        }
        r->tick(now);
    }
    CHECK(r->pending() <= mesh::route::kPendingCapacity);
  }
  CHECK(r->stats().dup > 1000 && r->stats().sent > 100);
}

// Announces handed to send() wait a random 0..announce_jitter, so two nodes
// announcing at the same moment do not collide at a hidden repeater; DATA does
// not wait.
void announce_jitter() {
  Millis first_due = 0;
  bool differ = false;
  for (uint32_t seed = 1; seed <= 8; seed++) {
    auto c = quiet();
    c.seed = seed;
    auto r = std::make_unique<Router>(c);
    Bytes an = packet(mesh::Type::Announce, 0x30);
    Bytes d = packet(mesh::Type::Data, 0x40);
    CHECK(r->send(an.data(), an.size(), true, 1000));
    CHECK(r->send(d.data(), d.size(), true, 1000));
    Frame f;
    Millis next = 0;
    CHECK(r->poll(1000, f, &next) && f.prio == Priority::Data);
    Millis due = mesh::route::kNever;
    for (Millis t = 1000; t < 1000 + c.announce_jitter; t += 50)
      if (r->poll(t, f, &next)) {
        CHECK(f.prio == Priority::Announce);
        due = t;
        break;
      }
    CHECK(due != mesh::route::kNever);
    if (seed == 1)
      first_due = due;
    else if (due != first_due)
      differ = true;
  }
  CHECK(differ);
  // Jitter 0: immediate.
  auto c = quiet();
  c.announce_jitter = 0;
  Router r(c);
  Bytes an = packet(mesh::Type::Announce, 0x50);
  CHECK(r.send(an.data(), an.size(), true, 1000));
  Frame f;
  CHECK(r.poll(1000, f, nullptr) && f.prio == Priority::Announce);
}

}  // namespace

void run_route() {
  seen_cache();
  flood_basics();
  snr_ordering_and_roles();
  cancellation();
  hop_limit_and_flags();
  priorities();
  own_packets_and_retries();
  pending_full();
  announces_and_receipts();
  announce_jitter();
  fuzz();
}

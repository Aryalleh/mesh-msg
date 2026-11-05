// Node packet store: dedup, eviction order, quotas, per-sink backlog,
// persistence records and restore.
#include <memory>
#include <random>

#include "check.h"
#include "packet.h"
#include "store.h"

using check::Bytes;
using mesh::store::Item;
using mesh::store::Origin;
using mesh::store::Put;
using mesh::store::Store;

namespace {

constexpr uint32_t kTTL = 3600;

Bytes pkt(mesh::Type t, uint16_t id, uint8_t dest = 0, uint8_t ctx = 0, uint8_t frag = 0) {
  Bytes body(30, uint8_t(id));
  mesh::Packet p;
  p.type = t;
  p.ctx = ctx;
  p.max_hops = 3;
  p.id[0] = uint8_t(id);
  p.id[1] = uint8_t(id >> 8);
  p.dest[0] = dest;
  if (ctx & mesh::kCtxFragmented) {
    p.frag_index = frag;
    p.frag_count = 4;
  }
  p.body = body.data();
  p.body_len = body.size();
  Bytes out(p.size());
  size_t n = 0;
  mesh::encode(p, out.data(), out.size(), &n);
  out.resize(n);
  return out;
}

mesh::route::Key key(const Bytes& raw) {
  mesh::Packet p;
  mesh::decode(raw.data(), raw.size(), p);
  return mesh::route::Key::of(p);
}

Put put(Store& s, const Bytes& b, uint32_t now = 0, uint8_t sinks = 0, Origin o = Origin::Air) {
  return s.put(b.data(), b.size(), o, sinks, now, kTTL);
}

struct FakeFlash : mesh::store::Persist {
  mesh::store::Record rec[mesh::store::kSlots];
  bool live[mesh::store::kSlots] = {};
  int writes = 0;
  void save(size_t i, const mesh::store::Record& r) override {
    rec[i] = r;
    live[i] = true;
    writes++;
  }
  void erase(size_t i) override {
    live[i] = false;
    writes++;
  }
};

void basics() {
  auto s = std::make_unique<Store>();
  Bytes a = pkt(mesh::Type::Data, 1);
  CHECK(put(*s, a) == Put::Stored);
  CHECK(put(*s, a) == Put::Duplicate);
  CHECK(s->has(key(a)) && s->size() == 1);
  Item it;
  CHECK(s->get(key(a), it) && Bytes(it.raw, it.raw + it.len) == a && it.origin == Origin::Air);
  CHECK(it.packet.type == mesh::Type::Data && it.packet.id[0] == 1);
  uint8_t junk[4] = {1, 2, 3, 4};
  CHECK(s->put(junk, 4, Origin::Air, 0, 0, kTTL) == Put::Invalid);
  CHECK(put(*s, pkt(mesh::Type::Link, 2)) == Put::Invalid);  // hop-local, never stored
  Bytes big(300, 0);
  CHECK(s->put(big.data(), big.size(), Origin::Air, 0, 0, kTTL) == Put::Invalid);

  // Fragments are separate items; delete_data removes them all.
  for (uint8_t f = 0; f < 4; f++) CHECK(put(*s, pkt(mesh::Type::Data, 7, 1, mesh::kCtxFragmented, f)) == Put::Stored);
  Bytes rc = pkt(mesh::Type::Receipt, 8);  // receipts are never deleted as data
  CHECK(put(*s, rc) == Put::Stored);
  uint8_t acked[8] = {7, 0, 0, 0, 0, 0, 0, 0};
  CHECK(s->delete_data(acked) == 4 && s->has(key(rc)) && s->size() == 2);

  // Expiry.
  CHECK(s->expire(kTTL - 1) == 0);
  CHECK(s->expire(kTTL) == 2 && s->size() == 0);
}

void eviction() {
  auto s = std::make_unique<Store>(nullptr, mesh::store::kSlots);
  uint16_t id = 1;
  // Fill: receipts, data, announces.
  std::vector<Bytes> rcs, data, anns;
  for (size_t i = 0; i < mesh::store::kSlots / 4; i++) rcs.push_back(pkt(mesh::Type::Receipt, id++));
  for (size_t i = 0; i < mesh::store::kSlots / 2; i++) data.push_back(pkt(mesh::Type::Data, id++, uint8_t(i)));
  while (rcs.size() + data.size() + anns.size() < mesh::store::kSlots) anns.push_back(pkt(mesh::Type::Announce, id++));
  for (auto& b : rcs) CHECK(put(*s, b, 10) == Put::Stored);
  for (auto& b : data) CHECK(put(*s, b, 10) == Put::Stored);
  for (auto& b : anns) CHECK(put(*s, b, 10) == Put::Stored);
  CHECK(s->size() == mesh::store::kSlots);

  // New data evicts announces first (oldest first)...
  CHECK(put(*s, pkt(mesh::Type::Data, id++, 200), 10) == Put::Stored);
  CHECK(!s->has(key(anns[0])) && s->has(key(anns[1])));
  for (size_t i = 1; i < anns.size(); i++) CHECK(put(*s, pkt(mesh::Type::Data, id++, uint8_t(100 + i)), 10) == Put::Stored);
  for (auto& b : anns) CHECK(!s->has(key(b)));
  // ...then the oldest data.
  CHECK(put(*s, pkt(mesh::Type::Data, id++, 250), 10) == Put::Stored);
  CHECK(!s->has(key(data[0])) && s->has(key(data[1])));
  // An announce cannot displace data; a receipt can.
  CHECK(put(*s, pkt(mesh::Type::Announce, id++), 10) == Put::Rejected);
  CHECK(put(*s, pkt(mesh::Type::Receipt, id++), 10) == Put::Stored);
  for (auto& b : rcs) CHECK(s->has(key(b)));
  // Expired items go before anything valuable.
  CHECK(put(*s, pkt(mesh::Type::Announce, id++), 10 + kTTL) == Put::Stored);
  CHECK(s->stats().expired == 1 && s->stats().evicted > 0);
}

void quota() {
  auto s = std::make_unique<Store>(nullptr, 3);
  for (uint16_t i = 0; i < 3; i++) CHECK(put(*s, pkt(mesh::Type::Data, uint16_t(i + 1), 9)) == Put::Stored);
  CHECK(put(*s, pkt(mesh::Type::Data, 50, 9)) == Put::Rejected && s->stats().quota == 1);
  CHECK(put(*s, pkt(mesh::Type::Data, 51, 8)) == Put::Stored);       // other destination
  CHECK(put(*s, pkt(mesh::Type::Receipt, 52, 9)) == Put::Stored);    // receipts exempt
  CHECK(put(*s, pkt(mesh::Type::Data, 53, 9), kTTL) == Put::Stored);  // expired ones don't count
}

void sinks() {
  auto s = std::make_unique<Store>();
  Bytes d1 = pkt(mesh::Type::Data, 1), d2 = pkt(mesh::Type::Data, 2, 1, mesh::kCtxPriority);
  Bytes an = pkt(mesh::Type::Announce, 3), rc = pkt(mesh::Type::Receipt, 4), up = pkt(mesh::Type::Data, 5);
  put(*s, d1);
  put(*s, an);
  put(*s, d2);
  put(*s, rc);
  put(*s, up, 0, mesh::store::kSinkUplink, Origin::Uplink);  // came from the relay
  std::vector<Bytes> phone;
  Item it;
  while (s->next_for(mesh::store::kSinkPhone, it)) {
    phone.emplace_back(it.raw, it.raw + it.len);
    s->mark(it.seq, mesh::store::kSinkPhone);
  }
  CHECK(phone.size() == 5);
  CHECK(phone[0] == rc && phone[1] == d2 && phone[2] == d1 && phone[3] == up && phone[4] == an);
  // The uplink still needs everything except what came from it.
  int n = 0;
  while (s->next_for(mesh::store::kSinkUplink, it)) {
    CHECK(Bytes(it.raw, it.raw + it.len) != up);
    s->mark(it.seq, mesh::store::kSinkUplink);
    n++;
  }
  CHECK(n == 4);
  // A newly paired phone gets the whole backlog again.
  s->reset_sink(mesh::store::kSinkPhone);
  n = 0;
  while (s->next_for(mesh::store::kSinkPhone, it)) {
    s->mark(it.seq, mesh::store::kSinkPhone);
    n++;
  }
  CHECK(n == 5);
  s->mark(9999, mesh::store::kSinkPhone);  // unknown seq: no-op
}

void persistence() {
  FakeFlash flash;
  auto s = std::make_unique<Store>(&flash);
  std::vector<Bytes> kept;
  for (uint16_t i = 1; i <= 10; i++) {
    Bytes b = pkt(i % 3 ? mesh::Type::Data : mesh::Type::Receipt, i, uint8_t(i));
    CHECK(put(*s, b, 0) == Put::Stored);
    kept.push_back(b);
  }
  uint8_t acked[8] = {1, 0, 0, 0, 0, 0, 0, 0};
  s->delete_data(acked);
  Item it;
  CHECK(s->next_for(mesh::store::kSinkPhone, it));
  s->mark(it.seq, mesh::store::kSinkPhone);
  uint32_t marked_seq = it.seq;
  int writes = flash.writes;
  s->mark(it.seq, mesh::store::kSinkPhone);  // unchanged: no flash write
  CHECK(flash.writes == writes);

  // Reboot: a fresh store restored from flash has the same contents.
  auto r = std::make_unique<Store>(&flash);
  size_t restored = 0;
  for (size_t i = 0; i < mesh::store::kSlots; i++)
    if (flash.live[i] && r->restore(i, flash.rec[i], 500, kTTL)) restored++;
  CHECK(restored == 9 && r->size() == 9);
  CHECK(!r->has(key(kept[0])));
  for (size_t i = 1; i < kept.size(); i++) CHECK(r->has(key(kept[i])));
  // Sink bits survive; TTL restarts from the restore time.
  int pending = 0;
  while (r->next_for(mesh::store::kSinkPhone, it)) {
    CHECK(it.seq != marked_seq);
    r->mark(it.seq, mesh::store::kSinkPhone);
    pending++;
  }
  CHECK(pending == 8);
  CHECK(r->expire(kTTL) == 0 && r->expire(500 + kTTL) == 9);
  // New items after restore get fresh sequence numbers.
  CHECK(put(*r, pkt(mesh::Type::Data, 77), 600) == Put::Stored);
  CHECK(r->next_for(mesh::store::kSinkPhone, it) && it.seq > 10);

  // Corrupted records are rejected.
  auto c = std::make_unique<Store>();
  mesh::store::Record bad = flash.rec[1];
  bad.magic = 0;
  CHECK(!c->restore(1, bad, 0, kTTL));
  bad = flash.rec[1];
  bad.raw[0] = 0xff;  // version
  CHECK(!c->restore(1, bad, 0, kTTL));
  bad = flash.rec[1];
  bad.len = 3;
  CHECK(!c->restore(1, bad, 0, kTTL));
  bad = flash.rec[1];
  bad.origin = 9;
  CHECK(!c->restore(1, bad, 0, kTTL));
  CHECK(!c->restore(mesh::store::kSlots, flash.rec[1], 0, kTTL));
  CHECK(c->restore(1, flash.rec[1], 0, kTTL));
  CHECK(!c->restore(2, flash.rec[1], 0, kTTL));  // duplicate
}

void fuzz() {
  std::mt19937 rng(11);
  FakeFlash flash;
  auto s = std::make_unique<Store>(&flash, 8);
  uint32_t now = 0;
  for (int i = 0; i < 20000; i++) {
    now += rng() % 30;
    int op = int(rng() % 10);
    if (op < 6) {
      mesh::Type t = mesh::Type(rng() % 5);
      put(*s, pkt(t, uint16_t(rng() % 300), uint8_t(rng() % 12)), now, uint8_t(rng() % 4));
    } else if (op == 6) {
      uint8_t acked[8] = {uint8_t(rng() % 300), 0};
      s->delete_data(acked);
    } else if (op == 7) {
      s->expire(now);
    } else {
      Item it;
      uint8_t sink = rng() % 2 ? mesh::store::kSinkPhone : mesh::store::kSinkUplink;
      if (s->next_for(sink, it)) {
        CHECK((it.sinks & sink) == 0 && it.len <= mesh::kLoRaMTU);
        s->mark(it.seq, sink);
      }
    }
    CHECK(s->size() <= mesh::store::kSlots);
  }
  // Flash mirror matches the store.
  size_t live = 0;
  for (bool l : flash.live) live += l;
  CHECK(live == s->size());
}

}  // namespace

void run_store() {
  basics();
  eviction();
  quota();
  sinks();
  persistence();
  fuzz();
}

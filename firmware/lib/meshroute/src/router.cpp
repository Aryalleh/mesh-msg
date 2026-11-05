#include "router.h"

#include <cstring>

#include "announce.h"

namespace mesh {
namespace route {

Key Key::of(const Packet& p) {
  Key k;
  std::memcpy(k.id, p.id, sizeof k.id);
  k.frag = p.key_frag();
  return k;
}

bool Key::operator==(const Key& o) const { return frag == o.frag && std::memcmp(id, o.id, sizeof id) == 0; }

Priority priority_of(const Packet& p) {
  switch (p.type) {
    case Type::Receipt:
      return Priority::Receipt;
    case Type::Data:
      return p.priority() ? Priority::DataPriority : Priority::Data;
    case Type::Announce:
      return Priority::Announce;
    default:
      return Priority::Summary;
  }
}

const char* verdict_name(Verdict v) {
  switch (v) {
    case Verdict::Invalid:
      return "invalid";
    case Verdict::Duplicate:
      return "duplicate";
    case Verdict::Acked:
      return "acked";
    case Verdict::Local:
      return "local";
    case Verdict::New:
      return "new";
  }
  return "?";
}

// ---- SeenCache ----

bool SeenCache::contains(const Key& k, Millis now) const {
  for (size_t i = 0; i < count_; i++) {
    const Entry& e = ring_[(head_ + i) % kSeenCapacity];
    if (e.key == k && live(e, now)) return true;
  }
  return false;
}

void SeenCache::insert(const Key& k, Millis now) {
  if (contains(k, now)) return;
  if (count_ == kSeenCapacity) {  // evict the oldest
    head_ = (head_ + 1) % kSeenCapacity;
    count_--;
  }
  ring_[(head_ + count_) % kSeenCapacity] = Entry{k, uint32_t(now / 1000)};
  count_++;
}

void SeenCache::expire(Millis now) {
  while (count_ > 0 && !live(ring_[head_], now)) {
    head_ = (head_ + 1) % kSeenCapacity;
    count_--;
  }
}

// ---- KeyCache ----

void KeyCache::put(const uint8_t dest[16], const PublicKey& pub) {
  for (size_t i = 0; i < count_; i++) {
    if (std::memcmp(e_[i].dest, dest, 16) == 0) {
      e_[i].pub = pub;
      return;
    }
  }
  Entry& e = e_[next_];
  std::memcpy(e.dest, dest, 16);
  e.pub = pub;
  next_ = (next_ + 1) % kKeyCapacity;
  if (count_ < kKeyCapacity) count_++;
}

const PublicKey* KeyCache::get(const uint8_t dest[16]) const {
  for (size_t i = 0; i < count_; i++)
    if (std::memcmp(e_[i].dest, dest, 16) == 0) return &e_[i].pub;
  return nullptr;
}

// ---- Router ----

Router::Router(const Config& cfg) : cfg_(cfg), rng_(cfg.seed ? cfg.seed : 1), seen_(cfg.seen_ttl) {}

uint32_t Router::rand() {  // xorshift32
  rng_ ^= rng_ << 13;
  rng_ ^= rng_ >> 17;
  rng_ ^= rng_ << 5;
  return rng_;
}

Millis Router::rebroadcast_delay(int snr_db) {
  Millis d = cfg_.delay_min;
  int span = cfg_.snr_high - cfg_.snr_low;
  if (span > 0) {
    int s = snr_db - cfg_.snr_low;
    if (s < 0) s = 0;
    if (s > span) s = span;
    d += cfg_.delay_window * Millis(s) / Millis(span);
  }
  if (cfg_.delay_jitter > 0) d += rand() % cfg_.delay_jitter;
  if (cfg_.role == Role::Client) d += cfg_.client_extra_delay;
  return d;
}

Router::Pending* Router::find(const Key& k) {
  for (auto& e : pend_)
    if (e.state != State::Free && e.key == k) return &e;
  return nullptr;
}

Router::Pending* Router::alloc(bool own) {
  for (auto& e : pend_)
    if (e.state == State::Free) return &e;
  if (!own) return nullptr;
  // Our own packets displace the least urgent queued rebroadcast.
  Pending* victim = nullptr;
  for (auto& e : pend_)
    if (e.state == State::Waiting && !e.own && (!victim || e.prio > victim->prio)) victim = &e;
  if (victim) victim->state = State::Free;
  return victim;
}

bool Router::schedule(const uint8_t* raw, size_t n, const Packet& p, bool own, Millis due) {
  Pending* e = alloc(own);
  if (!e) {
    stats_.pending_full++;
    return false;
  }
  if (encode_link_frame(cfg_.short_id, raw, n, e->frame, sizeof e->frame, &e->len) != Err::Ok) return false;
  e->state = State::Waiting;
  e->own = own;
  e->heard = 0;
  e->retries_left = own ? cfg_.retries : 0;
  e->prio = priority_of(p);
  e->key = Key::of(p);
  e->due = due;
  stats_.scheduled++;
  return true;
}

void Router::heard_copy(const Key& k) {
  Pending* e = find(k);
  if (!e) return;
  if (e->own) {
    // A neighbour rebroadcast our packet: implicit ACK.
    stats_.acks++;
    e->state = State::Free;
    return;
  }
  if (e->state == State::Waiting && ++e->heard >= cfg_.cancel_after) {
    stats_.cancelled++;
    e->state = State::Free;
  }
}

size_t Router::purge(const uint8_t acked[8]) {
  size_t n = 0;
  for (auto& e : pend_) {
    if (e.state != State::Free && e.prio != Priority::Receipt && std::memcmp(e.key.id, acked, 8) == 0) {
      e.state = State::Free;
      n++;
    }
  }
  stats_.purged += uint32_t(n);
  return n;
}

RxResult Router::on_receive(const uint8_t* frame, size_t n, int snr_db, Millis now) {
  stats_.rx++;
  RxResult r;
  const uint8_t* raw = nullptr;
  size_t rn = 0;
  if (decode_link_frame(frame, n, &r.sender, &raw, &rn) != Err::Ok || decode(raw, rn, r.packet) != Err::Ok) {
    stats_.invalid++;
    r.verdict = Verdict::Invalid;
    return r;
  }
  const Packet& p = r.packet;
  if (p.type == Type::Link) {
    r.verdict = Verdict::Local;
    return r;
  }
  Key k = Key::of(p);
  if (seen_.contains(k, now)) {
    stats_.dup++;
    heard_copy(k);
    r.verdict = Verdict::Duplicate;
    return r;
  }
  if (p.type == Type::Data) {
    Key done;
    std::memcpy(done.id, p.id, 8);
    done.frag = kFragAcked;
    if (seen_.contains(done, now)) {
      seen_.insert(k, now);
      stats_.acked++;
      r.verdict = Verdict::Acked;
      return r;
    }
  }

  // Signed control packets are verified before they are remembered, so a
  // forged copy cannot shadow the real one.
  if (p.type == Type::Announce) {
    Announce a;
    if (parse_announce(p, a) != AnnounceErr::Ok) {
      stats_.invalid++;
      r.verdict = Verdict::Invalid;
      return r;
    }
    keys_.put(p.dest, a.pub);
  } else if (p.type == Type::Receipt) {
    uint8_t acked[8];
    if (receipt_acked(p, acked) != AnnounceErr::Ok) {
      stats_.invalid++;
      r.verdict = Verdict::Invalid;
      return r;
    }
    // Only a receipt we can verify may purge; others are still forwarded.
    if (const PublicKey* signer = keys_.get(p.dest)) {
      if (verify_receipt(p, *signer, acked) != AnnounceErr::Ok) {
        stats_.invalid++;
        r.verdict = Verdict::Invalid;
        return r;
      }
      purge(acked);
      Key done;
      std::memcpy(done.id, acked, 8);
      done.frag = kFragAcked;
      seen_.insert(done, now);
      r.purged = true;
    }
  }

  seen_.insert(k, now);
  stats_.fresh++;
  r.verdict = Verdict::New;

  // Managed flood: DATA, RECEIPT and ANNOUNCE are rebroadcast once.
  // SUMMARY / PATH_REQ are neighbour-local and handled by the caller.
  bool floods = p.type == Type::Data || p.type == Type::Receipt || p.type == Type::Announce;
  if (!floods || p.no_lora()) return r;
  uint8_t fwd[kLoRaMTU];
  std::memcpy(fwd, raw, rn);  // rn <= kLoRaMTU: it came out of a radio frame
  if (!inc_hop(fwd, rn)) {
    stats_.hop_limit++;
    return r;
  }
  r.scheduled = schedule(fwd, rn, p, false, now + rebroadcast_delay(snr_db));
  return r;
}

bool Router::send(const uint8_t* raw, size_t n, bool own, Millis now) {
  Packet p;
  if (n > kLoRaMTU || decode(raw, n, p) != Err::Ok) return false;
  if (p.type == Type::Link || p.no_lora()) return false;
  Key k = Key::of(p);
  if (seen_.contains(k, now)) return false;
  uint8_t buf[kLoRaMTU];
  std::memcpy(buf, raw, n);
  if (!own && !inc_hop(buf, n)) {
    stats_.hop_limit++;
    return false;
  }
  if (!schedule(buf, n, p, own, now)) return false;
  seen_.insert(k, now);
  return true;
}

bool Router::poll(Millis now, Frame& out, Millis* next_due) {
  Pending* best = nullptr;
  Millis next = kNever;
  for (auto& e : pend_) {
    if (e.state != State::Waiting && e.state != State::AwaitAck) continue;
    if (e.due > now) {
      if (e.due < next) next = e.due;
      continue;
    }
    if (!best || e.prio < best->prio || (e.prio == best->prio && e.due < best->due)) best = &e;
  }
  if (!best) {
    if (next_due) *next_due = next;
    return false;
  }
  std::memcpy(out.data, best->frame, best->len);
  out.len = best->len;
  out.prio = best->prio;
  out.key = best->key;
  stats_.sent++;
  if (best->state == State::AwaitAck) {
    stats_.retries++;
    best->retries_left--;
  }
  if (best->retries_left > 0) {
    best->state = State::Handed;
    best->due = now;  // handed-at, for handed_timeout
  } else {
    if (best->own && best->state == State::AwaitAck) stats_.gave_up++;
    best->state = State::Free;
  }
  if (next_due) {
    // Recompute: something else may already be due.
    next = kNever;
    for (auto& e : pend_)
      if ((e.state == State::Waiting || e.state == State::AwaitAck) && e.due < next) next = e.due;
    *next_due = next;
  }
  return true;
}

void Router::on_transmitted(const Key& k, Millis now) {
  Pending* e = find(k);
  if (!e || e->state != State::Handed) return;
  e->state = State::AwaitAck;
  e->due = now + cfg_.ack_timeout + (cfg_.ack_jitter ? rand() % cfg_.ack_jitter : 0);
}

void Router::tick(Millis now) {
  seen_.expire(now);
  for (auto& e : pend_)
    if (e.state == State::Handed && now - e.due >= cfg_.handed_timeout) e.state = State::Free;
}

size_t Router::pending() const {
  size_t n = 0;
  for (const auto& e : pend_)
    if (e.state != State::Free) n++;
  return n;
}

}  // namespace route
}  // namespace mesh

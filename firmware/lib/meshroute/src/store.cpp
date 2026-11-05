#include "store.h"

#include <cstring>

namespace mesh {
namespace store {

Store::Store(Persist* persist, size_t per_dest_max)
    : persist_(persist), per_dest_max_(per_dest_max ? per_dest_max : (kSlots / 4 ? kSlots / 4 : 1)) {}

bool Store::fill(Slot& s, const uint8_t* raw, size_t n) {
  Packet p;
  if (n > kLoRaMTU || decode(raw, n, p) != Err::Ok || p.type == Type::Link) return false;
  s.key = Key::of(p);
  s.type = p.type;
  s.prio = route::priority_of(p);
  std::memcpy(s.dest, p.dest, 16);
  std::memcpy(s.rec.raw, raw, n);
  s.rec.len = uint8_t(n);
  return true;
}

int Store::find(const Key& k) const {
  for (size_t i = 0; i < kSlots; i++)
    if (s_[i].used && s_[i].key == k) return int(i);
  return -1;
}

int Store::free_slot() const {
  for (size_t i = 0; i < kSlots; i++)
    if (!s_[i].used) return int(i);
  return -1;
}

int Store::victim(uint32_t now_s, Priority incoming) const {
  int best = -1;
  for (size_t i = 0; i < kSlots; i++) {
    const Slot& s = s_[i];
    if (!s.used) continue;
    if (int32_t(s.rec.expires_s - now_s) <= 0) return int(i);  // expired: take it
    if (s.prio < incoming) continue;  // more valuable than what arrives
    if (best < 0 || s.prio > s_[best].prio || (s.prio == s_[best].prio && s.rec.seq < s_[best].rec.seq))
      best = int(i);
  }
  return best;
}

void Store::save(int i) {
  if (persist_) persist_->save(size_t(i), s_[i].rec);
}

void Store::drop(int i) {
  s_[i].used = false;
  if (persist_) persist_->erase(size_t(i));
}

Put Store::put(const uint8_t* raw, size_t n, Origin origin, uint8_t sinks, uint32_t now_s, uint32_t ttl_s) {
  Slot tmp;
  if (!fill(tmp, raw, n)) return Put::Invalid;
  if (find(tmp.key) >= 0) {
    stats_.dup++;
    return Put::Duplicate;
  }
  if (tmp.type == Type::Data) {
    size_t same = 0;
    for (const auto& s : s_)
      if (s.used && s.type == Type::Data && std::memcmp(s.dest, tmp.dest, 16) == 0 &&
          int32_t(s.rec.expires_s - now_s) > 0)
        same++;
    if (same >= per_dest_max_) {
      stats_.quota++;
      return Put::Rejected;
    }
  }
  int i = free_slot();
  if (i < 0) {
    i = victim(now_s, tmp.prio);
    if (i < 0) return Put::Rejected;
    if (int32_t(s_[i].rec.expires_s - now_s) <= 0)
      stats_.expired++;
    else
      stats_.evicted++;
  }
  tmp.used = true;
  tmp.rec.magic = kRecordMagic;
  tmp.rec.seq = seq_++;
  tmp.rec.expires_s = now_s + ttl_s;
  tmp.rec.origin = uint8_t(origin);
  tmp.rec.sinks = sinks;
  s_[i] = tmp;
  save(i);
  stats_.stored++;
  return Put::Stored;
}

bool Store::has(const Key& k) const { return find(k) >= 0; }

void Store::item(int i, Item& out) const {
  const Slot& s = s_[i];
  out.raw = s.rec.raw;
  out.len = s.rec.len;
  out.seq = s.rec.seq;
  out.origin = Origin(s.rec.origin);
  out.sinks = s.rec.sinks;
  decode(out.raw, out.len, out.packet);  // validated on the way in
}

bool Store::get(const Key& k, Item& out) const {
  int i = find(k);
  if (i < 0) return false;
  item(i, out);
  return true;
}

size_t Store::delete_data(const uint8_t acked[8]) {
  size_t n = 0;
  for (size_t i = 0; i < kSlots; i++) {
    if (s_[i].used && s_[i].type == Type::Data && std::memcmp(s_[i].key.id, acked, 8) == 0) {
      drop(int(i));
      n++;
    }
  }
  stats_.deleted += uint32_t(n);
  return n;
}

size_t Store::expire(uint32_t now_s) {
  size_t n = 0;
  for (size_t i = 0; i < kSlots; i++) {
    if (s_[i].used && int32_t(s_[i].rec.expires_s - now_s) <= 0) {
      drop(int(i));
      n++;
    }
  }
  stats_.expired += uint32_t(n);
  return n;
}

bool Store::next_for(uint8_t sink, Item& out) const {
  int best = -1;
  for (size_t i = 0; i < kSlots; i++) {
    const Slot& s = s_[i];
    if (!s.used || (s.rec.sinks & sink)) continue;
    if (best < 0 || s.prio < s_[best].prio || (s.prio == s_[best].prio && s.rec.seq < s_[best].rec.seq))
      best = int(i);
  }
  if (best < 0) return false;
  item(best, out);
  return true;
}

void Store::mark(uint32_t seq, uint8_t sink) {
  for (size_t i = 0; i < kSlots; i++) {
    if (s_[i].used && s_[i].rec.seq == seq) {
      if ((s_[i].rec.sinks & sink) != sink) {
        s_[i].rec.sinks |= sink;
        save(int(i));
      }
      return;
    }
  }
}

void Store::unmark(uint32_t seq, uint8_t sink) {
  for (size_t i = 0; i < kSlots; i++) {
    if (s_[i].used && s_[i].rec.seq == seq) {
      if (s_[i].rec.sinks & sink) {
        s_[i].rec.sinks &= uint8_t(~sink);
        save(int(i));
      }
      return;
    }
  }
}

void Store::reset_sink(uint8_t sink) {
  for (size_t i = 0; i < kSlots; i++) {
    if (s_[i].used && (s_[i].rec.sinks & sink)) {
      s_[i].rec.sinks &= uint8_t(~sink);
      save(int(i));
    }
  }
}

bool Store::restore(size_t slot, const Record& r, uint32_t now_s, uint32_t ttl_s) {
  if (slot >= kSlots || r.magic != kRecordMagic || r.origin > uint8_t(Origin::Local)) return false;
  Slot tmp;
  if (!fill(tmp, r.raw, r.len) || find(tmp.key) >= 0) return false;
  tmp.used = true;
  tmp.rec = r;
  tmp.rec.expires_s = now_s + ttl_s;
  s_[slot] = tmp;
  if (r.seq >= seq_) seq_ = r.seq + 1;
  return true;
}

size_t Store::size() const {
  size_t n = 0;
  for (const auto& s : s_)
    if (s.used) n++;
  return n;
}

}  // namespace store
}  // namespace mesh

#include "kbproto.h"

namespace mesh {
namespace keys {

uint8_t encode_event(const Event& e) {
  return uint8_t((e.pressed ? 0x80 : 0) | (e.pos.row & 7) << 3 | (e.pos.col & 7));
}

Event decode_event(uint8_t b) {
  Event e;
  e.pressed = b & 0x80;
  e.pos.row = int8_t(b >> 3 & 7);
  e.pos.col = int8_t(b & 7);
  return e;
}

void ModuleState::push(const Event& e) {
  if (count_ == kFifo) {  // full: lose the oldest, keep the newest
    head_ = (head_ + 1) % kFifo;
    count_--;
  }
  q_[(head_ + count_) % kFifo] = encode_event(e);
  count_++;
}

void ModuleState::motion(int dx, int dy) {
  auto sat = [](int v) { return v > 127 ? 127 : v < -127 ? -127 : v; };
  dx_ = sat(dx_ + dx);
  dy_ = sat(dy_ + dy);
}

size_t ModuleState::read(uint8_t reg, uint8_t* out, size_t cap) {
  switch (reg) {
    case kRegId:
      if (cap < 4) return 0;
      out[0] = 'M';
      out[1] = 'K';
      out[2] = kProtoVersion;
      out[3] = uint8_t(variant_);
      return 4;
    case kRegEvents: {
      if (cap < 1) return 0;
      size_t n = count_ < kEventsPerRead ? count_ : kEventsPerRead;
      if (n > cap - 1) n = cap - 1;
      out[0] = uint8_t(n);
      for (size_t i = 0; i < n; i++) out[1 + i] = q_[(head_ + i) % kFifo];
      head_ = (head_ + n) % kFifo;
      count_ -= n;
      return 1 + n;
    }
    case kRegTrack:
      if (cap < 2) return 0;
      out[0] = uint8_t(int8_t(dx_));
      out[1] = uint8_t(int8_t(dy_));
      dx_ = dy_ = 0;
      return 2;
    default:
      return 0;
  }
}

int ModuleState::write(const uint8_t* b, size_t n) {
  if (n >= 2 && b[0] == kRegBacklight) return b[1];
  return -1;
}

size_t parse_events(const uint8_t* b, size_t n, Event* out, size_t cap) {
  if (n < 1) return 0;
  size_t k = b[0];
  if (k > n - 1) k = n - 1;
  if (k > cap) k = cap;
  for (size_t i = 0; i < k; i++) out[i] = decode_event(b[1 + i]);
  return k;
}

bool parse_id(const uint8_t* b, size_t n, Variant* v) {
  if (n < 4 || b[0] != 'M' || b[1] != 'K' || b[2] != kProtoVersion || b[3] > uint8_t(Variant::BB9900Trackpad))
    return false;
  *v = Variant(b[3]);
  return true;
}

}  // namespace keys
}  // namespace mesh

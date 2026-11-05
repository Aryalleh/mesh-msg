#include "packet.h"

#include <cstring>

namespace mesh {

const char* type_name(Type t) {
  switch (t) {
    case Type::Data: return "DATA";
    case Type::Receipt: return "RECEIPT";
    case Type::Announce: return "ANNOUNCE";
    case Type::PathReq: return "PATH_REQ";
    case Type::Summary: return "SUMMARY";
    case Type::Link: return "LINK";
  }
  return "TYPE(?)";
}

const char* err_name(Err e) {
  switch (e) {
    case Err::Ok: return "ok";
    case Err::Short: return "too short";
    case Err::Version: return "unsupported version";
    case Err::TooLarge: return "too large";
    case Err::Frag: return "invalid fragment";
    case Err::Range: return "out of range";
  }
  return "?";
}

uint8_t Packet::flags() const {
  return static_cast<uint8_t>(kVersion << 6 | (static_cast<uint8_t>(type) & 0x7) << 3 | (ctx & 0x7));
}

Err encode(const Packet& p, uint8_t* out, size_t cap, size_t* out_len) {
  if (p.hop_count > 15 || p.max_hops > 15 || static_cast<uint8_t>(p.type) > 7) return Err::Range;
  if (p.body_len > 0 && p.body == nullptr) return Err::Range;
  const size_t n = p.size();
  if (n > kMaxSize) return Err::TooLarge;
  if (p.fragmented() && (p.frag_count == 0 || p.frag_count > kMaxFragments || p.frag_index >= p.frag_count))
    return Err::Frag;
  if (cap < n) return Err::Short;
  out[0] = p.flags();
  out[1] = static_cast<uint8_t>(p.hop_count << 4 | p.max_hops);
  std::memcpy(out + 2, p.id, 8);
  std::memcpy(out + 10, p.dest, 16);
  size_t off = kHeaderSize;
  if (p.fragmented()) out[off++] = static_cast<uint8_t>(p.frag_index << 4 | (p.frag_count - 1));
  if (p.body_len) std::memmove(out + off, p.body, p.body_len);
  *out_len = n;
  return Err::Ok;
}

Err decode(const uint8_t* b, size_t n, Packet& out) {
  if (n < kHeaderSize) return Err::Short;
  if (n > kMaxSize) return Err::TooLarge;
  if (b[0] >> 6 != kVersion) return Err::Version;
  Packet p;
  p.type = static_cast<Type>(b[0] >> 3 & 0x7);
  p.ctx = b[0] & 0x7;
  p.hop_count = b[1] >> 4;
  p.max_hops = b[1] & 0xF;
  std::memcpy(p.id, b + 2, 8);
  std::memcpy(p.dest, b + 10, 16);
  const uint8_t* body = b + kHeaderSize;
  size_t body_len = n - kHeaderSize;
  if (p.fragmented()) {
    if (body_len < 1) return Err::Short;
    p.frag_index = body[0] >> 4;
    p.frag_count = static_cast<uint8_t>((body[0] & 0xF) + 1);
    if (p.frag_index >= p.frag_count) return Err::Frag;
    body++;
    body_len--;
  }
  p.body = body;
  p.body_len = body_len;
  out = p;
  return Err::Ok;
}

void prologue(Type t, const uint8_t id[8], const uint8_t dest[16], uint8_t out[kPrologueSize]) {
  out[0] = static_cast<uint8_t>(kVersion << 6 | (static_cast<uint8_t>(t) & 0x7) << 3);
  std::memcpy(out + 1, id, 8);
  std::memcpy(out + 9, dest, 16);
}

bool inc_hop(uint8_t* raw, size_t n) {
  if (n < kHeaderSize) return false;
  const uint8_t hc = raw[1] >> 4, mh = raw[1] & 0xF;
  if (hc >= mh || hc == 15) return false;
  raw[1] = static_cast<uint8_t>((hc + 1) << 4 | mh);
  return true;
}

size_t fragment_count(const Packet& p, size_t max_size) {
  if (p.fragmented()) return 0;
  if (p.size() <= max_size) return 1;
  if (max_size <= kHeaderSize + 1) return 0;
  const size_t per = max_size - kHeaderSize - 1;
  const size_t n = (p.body_len + per - 1) / per;
  return n > kMaxFragments ? 0 : n;
}

Err fragment(const Packet& p, size_t max_size, size_t index, Packet& out) {
  const size_t n = fragment_count(p, max_size);
  if (n == 0) return p.fragmented() ? Err::Frag : Err::TooLarge;
  if (index >= n) return Err::Range;
  if (n == 1) {
    out = p;
    return Err::Ok;
  }
  const size_t per = max_size - kHeaderSize - 1;
  const size_t start = index * per;
  const size_t end = start + per < p.body_len ? start + per : p.body_len;
  out = p;
  out.ctx = static_cast<uint8_t>(p.ctx | kCtxFragmented);
  out.frag_index = static_cast<uint8_t>(index);
  out.frag_count = static_cast<uint8_t>(n);
  out.body = p.body + start;
  out.body_len = end - start;
  return Err::Ok;
}

Err reassemble(const Packet* frags, size_t n, uint8_t* body_buf, size_t cap, Packet& out) {
  if (n == 0) return Err::Frag;
  const size_t count = frags[0].frag_count;
  if (count == 0 || count > kMaxFragments || n != count) return Err::Frag;
  const Packet* ordered[kMaxFragments] = {};
  size_t total = 0;
  for (size_t i = 0; i < n; i++) {
    const Packet& f = frags[i];
    if (!f.fragmented() || f.frag_count != count || f.frag_index >= count ||
        std::memcmp(f.id, frags[0].id, 8) != 0 || ordered[f.frag_index] != nullptr)
      return Err::Frag;
    ordered[f.frag_index] = &f;
    total += f.body_len;
  }
  if (total > cap) return Err::Short;
  size_t off = 0;
  for (size_t i = 0; i < count; i++) {
    if (ordered[i]->body_len) std::memcpy(body_buf + off, ordered[i]->body, ordered[i]->body_len);
    off += ordered[i]->body_len;
  }
  out = *ordered[0];
  out.ctx = static_cast<uint8_t>(out.ctx & ~kCtxFragmented);
  out.frag_index = 0;
  out.frag_count = 0;
  out.body = body_buf;
  out.body_len = total;
  return Err::Ok;
}

Err encode_link_frame(uint8_t sender, const uint8_t* raw, size_t n, uint8_t* out, size_t cap,
                      size_t* out_len) {
  if (n > kLoRaMTU) return Err::TooLarge;
  if (cap < n + kLoRaLinkHeader) return Err::Short;
  std::memmove(out + kLoRaLinkHeader, raw, n);
  out[0] = sender;
  *out_len = n + kLoRaLinkHeader;
  return Err::Ok;
}

Err decode_link_frame(const uint8_t* frame, size_t n, uint8_t* sender, const uint8_t** raw,
                      size_t* raw_len) {
  if (n < kLoRaLinkHeader + kHeaderSize) return Err::Short;
  if (n > kLoRaRadioMax) return Err::TooLarge;
  *sender = frame[0];
  *raw = frame + kLoRaLinkHeader;
  *raw_len = n - kLoRaLinkHeader;
  return Err::Ok;
}

}  // namespace mesh

#include "signal.h"

#include <cstdio>
#include <cstring>

namespace mesh {
namespace call {
namespace {

uint16_t get16(const uint8_t* p) { return uint16_t(p[0] << 8 | p[1]); }
uint32_t get32(const uint8_t* p) { return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3]; }

struct W {
  uint8_t* p;
  size_t cap, n = 0;
  bool ok = true;
  void u8(uint8_t v) {
    if (n + 1 > cap) {
      ok = false;
      return;
    }
    p[n++] = v;
  }
  void bytes(const uint8_t* b, size_t k) {
    if (n + k > cap) {
      ok = false;
      return;
    }
    if (k) std::memcpy(p + n, b, k);
    n += k;
  }
  void u16(uint16_t v) {
    u8(uint8_t(v >> 8));
    u8(uint8_t(v));
  }
  void u32(uint32_t v) {
    u16(uint16_t(v >> 16));
    u16(uint16_t(v));
  }
  void addr(const Addr& a) {
    if (!a.valid()) {
      u8(0);
      return;
    }
    u8(a.family);
    bytes(a.ip, a.is4() ? 4 : 16);
    u16(a.port);
  }
};

bool read_addr(const uint8_t*& b, size_t& n, Addr& a) {
  if (n < 1) return false;
  a = Addr();
  switch (b[0]) {
    case 0:
      b++, n--;
      return true;
    case 4:
      if (n < 1 + 4 + 2) return false;
      a.family = 4;
      std::memcpy(a.ip, b + 1, 4);
      a.port = get16(b + 5);
      b += 7, n -= 7;
      return true;
    case 6:
      if (n < 1 + 16 + 2) return false;
      a.family = 6;
      std::memcpy(a.ip, b + 1, 16);
      a.port = get16(b + 17);
      b += 19, n -= 19;
      // An IPv4-mapped IPv6 address is IPv4 (Go's Unmap).
      {
        static const uint8_t pre[12] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff};
        if (std::memcmp(a.ip, pre, 12) == 0) {
          std::memmove(a.ip, a.ip + 12, 4);
          std::memset(a.ip + 4, 0, 12);
          a.family = 4;
        }
      }
      return true;
    default:
      return false;
  }
}

void write_caps(W& w, const Caps& c) {
  w.u8(c.version);
  w.u8(c.n);
  for (uint8_t i = 0; i < c.n; i++) {
    const StreamCap& s = c.streams[i];
    w.u8(s.id);
    w.u8(s.media);
    w.u8(s.dir);
    w.u8(s.n_codecs);
    w.bytes(s.codecs, s.n_codecs);
    w.u16(s.max_kbps);
    w.u8(s.n_params);
    w.bytes(s.params, s.n_params);
  }
}

bool read_caps(const uint8_t*& b, size_t& n, Caps& c) {
  if (n < 2) return false;
  c = Caps();
  c.version = b[0];
  size_t k = b[1];
  b += 2, n -= 2;
  for (size_t i = 0; i < k; i++) {
    if (n < 4) return false;
    StreamCap s;
    s.id = b[0], s.media = b[1], s.dir = b[2];
    size_t nc = b[3];
    b += 4, n -= 4;
    if (n < nc + 3) return false;
    s.n_codecs = uint8_t(nc < sizeof s.codecs ? nc : sizeof s.codecs);
    std::memcpy(s.codecs, b, s.n_codecs);
    s.max_kbps = get16(b + nc);
    size_t np = b[nc + 2];
    b += nc + 3, n -= nc + 3;
    if (n < np) return false;
    s.n_params = uint8_t(np < sizeof s.params ? np : sizeof s.params);
    std::memcpy(s.params, b, s.n_params);
    b += np, n -= np;
    if (c.n < sizeof c.streams / sizeof c.streams[0]) c.streams[c.n++] = s;
  }
  return true;
}

}  // namespace

bool Addr::same_ip(const Addr& o) const {
  return family == o.family && std::memcmp(ip, o.ip, is4() ? 4 : 16) == 0;
}

bool Addr::same(const Addr& o) const { return same_ip(o) && port == o.port; }

bool Addr::loopback() const {
  if (is4()) return ip[0] == 127;
  static const uint8_t one[16] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1};
  return family == 6 && std::memcmp(ip, one, 16) == 0;
}

Addr Addr::v4(uint8_t a, uint8_t b, uint8_t c, uint8_t d, uint16_t port) {
  Addr x;
  x.family = 4;
  x.ip[0] = a, x.ip[1] = b, x.ip[2] = c, x.ip[3] = d;
  x.port = port;
  return x;
}

void Addr::str(char* out, size_t cap) const {
  if (is4()) {
    std::snprintf(out, cap, "%u.%u.%u.%u:%u", ip[0], ip[1], ip[2], ip[3], port);
  } else if (family == 6) {
    int k = std::snprintf(out, cap, "[");
    for (int i = 0; i < 16 && k > 0 && size_t(k) < cap; i += 2)
      k += std::snprintf(out + k, cap - size_t(k), i ? ":%x" : "%x", unsigned(ip[i] << 8 | ip[i + 1]));
    if (k > 0 && size_t(k) < cap) std::snprintf(out + k, cap - size_t(k), "]:%u", port);
  } else {
    std::snprintf(out, cap, "-");
  }
}

const char* kind_name(Kind k) {
  static const char* n[] = {"host", "reflexive", "peer-reflexive", "local", "mapped", "predicted", "relayed", "?"};
  return n[uint8_t(k) < 7 ? uint8_t(k) : 7];
}

const char* nat_name(NatType t) {
  static const char* n[] = {"unknown", "open", "endpoint-independent", "symmetric (sequential)", "symmetric (random)",
                            "UDP blocked", "?"};
  return n[uint8_t(t) < 6 ? uint8_t(t) : 6];
}

void Offer::add(const Candidate& c) {
  if (!c.addr.valid()) return;
  for (size_t i = 0; i < n; i++)
    if (cand[i].addr.same(c.addr)) return;
  if (n < kMaxCandidates) cand[n++] = c;
}

uint32_t priority(Kind k, const Addr& a) {
  uint32_t base = 0;
  switch (k) {
    case Kind::Host: base = 126; break;
    case Kind::Local: base = 120; break;
    case Kind::Mapped:
    case Kind::PeerRefl: base = 110; break;
    case Kind::Reflexive: base = 100; break;
    case Kind::Predicted: base = 50; break;
    case Kind::Relayed: base = 0; break;
  }
  uint32_t p = base << 24;
  if (a.family == 6) p += 1u << 16;
  return p;
}

int pair_class(Kind local, Kind remote, const Addr& a) {
  if (local == Kind::Relayed || remote == Kind::Relayed) return 6;
  if (local == Kind::Predicted || remote == Kind::Predicted) return 5;
  if ((local == Kind::Host || local == Kind::Local) && (remote == Kind::Host || remote == Kind::Local)) {
    // Global IPv6 both ways: no NAT on the way.
    bool global6 = a.family == 6 && (a.ip[0] & 0xe0) == 0x20;
    return global6 ? 2 : 1;
  }
  if (local == Kind::Mapped || remote == Kind::Mapped || local == Kind::PeerRefl || remote == Kind::PeerRefl) return 3;
  return 4;
}

size_t encode_offer(const Offer& o, uint8_t* out, size_t cap) {
  W w{out, cap};
  w.u8(uint8_t(o.nat.type));
  w.u16(uint16_t(o.nat.step));
  w.addr(o.nat.last);
  w.u8(uint8_t(o.n));
  for (size_t i = 0; i < o.n; i++) {
    w.u8(uint8_t(o.cand[i].kind));
    w.addr(o.cand[i].addr);
    w.u32(o.cand[i].priority);
  }
  return w.ok ? w.n : 0;
}

bool decode_offer(const uint8_t* b, size_t n, Offer& o, size_t* used) {
  const uint8_t* start = b;
  o = Offer();
  if (n < 3) return false;
  o.nat.type = NatType(b[0]);
  o.nat.step = int16_t(get16(b + 1));
  b += 3, n -= 3;
  if (!read_addr(b, n, o.nat.last)) return false;
  if (n < 1) return false;
  size_t k = b[0];
  b++, n--;
  for (size_t i = 0; i < k; i++) {
    if (n < 1) return false;
    Candidate c;
    c.kind = Kind(b[0]);
    b++, n--;
    if (!read_addr(b, n, c.addr) || n < 4) return false;
    c.priority = get32(b);
    b += 4, n -= 4;
    if (c.addr.valid() && o.n < kMaxCandidates) o.cand[o.n++] = c;
  }
  if (used) *used = size_t(b - start);
  return true;
}

const StreamCap* Caps::audio() const {
  for (uint8_t i = 0; i < n; i++)
    if (streams[i].id == 0 && streams[i].media == kMediaAudio) return &streams[i];
  return nullptr;
}

const StreamCap* Caps::video() const {
  for (uint8_t i = 0; i < n; i++)
    if (streams[i].media == kMediaVideo) return &streams[i];
  return nullptr;
}

size_t encode_signal(const Signal& s, uint8_t* out, size_t cap) {
  W w{out, cap};
  w.bytes(s.call_id, 16);
  w.u8(uint8_t(s.op));
  switch (s.op) {
    case Op::Offer: {
      w.u32(s.expires);
      write_caps(w, s.caps);
      w.bytes(s.key, 32);
      if (!w.ok) return 0;
      size_t k = encode_offer(s.offer, out + w.n, cap - w.n);
      if (!k) return 0;
      w.n += k;
      break;
    }
    case Op::Answer:
    case Op::Candidates: {
      if (s.op == Op::Answer) write_caps(w, s.caps);
      if (!w.ok) return 0;
      size_t k = encode_offer(s.offer, out + w.n, cap - w.n);
      if (!k) return 0;
      w.n += k;
      break;
    }
    case Op::Reject:
    case Op::End:
      w.u8(s.reason);
      break;
    case Op::Cancel:
      break;
    case Op::Update:
      write_caps(w, s.caps);
      break;
    case Op::Control:
      w.u8(s.stream);
      w.u8(s.kind);
      w.bytes(s.body, s.body_n);
      break;
  }
  return w.ok ? w.n : 0;
}

Decoded decode_signal(const uint8_t* b, size_t n, Signal& s) {
  s = Signal();
  if (n < 17) return Decoded::Short;
  std::memcpy(s.call_id, b, 16);
  uint8_t op = b[16];
  b += 17, n -= 17;
  if (op > uint8_t(Op::Control)) return Decoded::UnknownOp;
  s.op = Op(op);
  switch (s.op) {
    case Op::Offer:
      if (n < 4) return Decoded::Short;
      s.expires = get32(b);
      b += 4, n -= 4;
      if (!read_caps(b, n, s.caps) || n < 32) return Decoded::Short;
      std::memcpy(s.key, b, 32);
      b += 32, n -= 32;
      return decode_offer(b, n, s.offer, nullptr) ? Decoded::Ok : Decoded::Short;
    case Op::Answer:
      if (!read_caps(b, n, s.caps)) return Decoded::Short;
      return decode_offer(b, n, s.offer, nullptr) ? Decoded::Ok : Decoded::Short;
    case Op::Candidates:
      return decode_offer(b, n, s.offer, nullptr) ? Decoded::Ok : Decoded::Short;
    case Op::Reject:
    case Op::End:
      if (n < 1) return Decoded::Short;
      s.reason = b[0];
      return Decoded::Ok;
    case Op::Cancel:
      return Decoded::Ok;
    case Op::Update:
      return read_caps(b, n, s.caps) ? Decoded::Ok : Decoded::Short;
    case Op::Control:
      if (n < 2) return Decoded::Short;
      s.stream = b[0], s.kind = b[1];
      s.body_n = uint8_t(n - 2 < sizeof s.body ? n - 2 : sizeof s.body);
      std::memcpy(s.body, b + 2, s.body_n);
      return Decoded::Ok;
  }
  return Decoded::UnknownOp;
}

uint8_t pick_codec(const uint8_t* pref, size_t pn, const uint8_t* have, size_t hn) {
  for (size_t i = 0; i < pn; i++)
    for (size_t j = 0; j < hn; j++)
      if (pref[i] == have[j]) return pref[i];
  return 0;
}

}  // namespace call
}  // namespace mesh

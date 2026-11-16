#include "meshtastic.h"

#include <cstring>

#include "packet.h"

namespace mesh {
namespace meshtastic {

const uint8_t kDefaultPsk[16] = {0xd4, 0xf1, 0xbb, 0x3a, 0x20, 0x29, 0x07, 0x59,
                                 0xf0, 0xbc, 0xff, 0xab, 0xcf, 0x4e, 0x69, 0x01};

namespace {

void put32(uint8_t* p, uint32_t v) {
  p[0] = uint8_t(v);
  p[1] = uint8_t(v >> 8);
  p[2] = uint8_t(v >> 16);
  p[3] = uint8_t(v >> 24);
}

uint32_t get32(const uint8_t* p) {
  return uint32_t(p[0]) | uint32_t(p[1]) << 8 | uint32_t(p[2]) << 16 | uint32_t(p[3]) << 24;
}

uint8_t xor_hash(const uint8_t* p, size_t n) {
  uint8_t h = 0;
  for (size_t i = 0; i < n; i++) h ^= p[i];
  return h;
}

size_t put_varint(uint32_t v, uint8_t* out, size_t cap) {
  size_t n = 0;
  do {
    if (n >= cap) return 0;
    uint8_t b = uint8_t(v & 0x7f);
    v >>= 7;
    out[n++] = uint8_t(v ? b | 0x80 : b);
  } while (v);
  return n;
}

bool get_varint(const uint8_t* p, size_t n, size_t& off, uint32_t& v) {
  v = 0;
  for (unsigned shift = 0; off < n && shift < 35; shift += 7) {
    uint8_t b = p[off++];
    v |= uint32_t(b & 0x7f) << shift;
    if (!(b & 0x80)) return true;
  }
  return false;
}

// Data { portnum = 256; payload = mava }.
size_t encode_data(const uint8_t* payload, size_t n, uint8_t* out, size_t cap) {
  size_t o = 0;
  if (cap < 1) return 0;
  out[o++] = 0x08;  // field 1, varint
  size_t v = put_varint(kPrivateAppPort, out + o, cap - o);
  if (!v) return 0;
  o += v;
  if (o >= cap) return 0;
  out[o++] = 0x12;  // field 2, length-delimited
  v = put_varint(uint32_t(n), out + o, cap - o);
  if (!v || o + v + n > cap) return 0;
  o += v;
  std::memcpy(out + o, payload, n);
  return o + n;
}

// Returns the payload length if this is a PRIVATE_APP Data message, else 0.
size_t decode_data(const uint8_t* in, size_t n, uint8_t* payload, size_t cap) {
  size_t off = 0, len = 0;
  uint32_t port = 0;
  bool have_payload = false;
  while (off < n) {
    uint32_t key = 0;
    if (!get_varint(in, n, off, key)) return 0;
    uint32_t field = key >> 3, wire = key & 7;
    if (wire == 0) {
      uint32_t v = 0;
      if (!get_varint(in, n, off, v)) return 0;
      if (field == 1) port = v;
    } else if (wire == 2) {
      uint32_t l = 0;
      if (!get_varint(in, n, off, l) || l > n - off) return 0;
      if (field == 2) {
        if (l > cap) return 0;
        std::memcpy(payload, in + off, l);
        len = l;
        have_payload = true;
      }
      off += l;
    } else if (wire == 5) {
      if (n - off < 4) return 0;
      off += 4;
    } else if (wire == 1) {
      if (n - off < 8) return 0;
      off += 8;
    } else {
      return 0;
    }
  }
  return port == kPrivateAppPort && have_payload ? len : 0;
}

// Mava dedup key (packet id || fragment) of a raw Mava packet.
bool mava_key(const uint8_t* raw, size_t n, uint8_t key[9]) {
  Packet p;
  if (decode(raw, n, p) != Err::Ok) return false;
  std::memcpy(key, p.id, 8);
  key[8] = p.key_frag();
  return true;
}

}  // namespace

bool Channel::valid() const {
  size_t nl = strnlen(name, sizeof name);
  if (nl == 0 || nl > kChannelNameMax) return false;
  if (hop_limit < 1 || hop_limit > 7) return false;
  if (psk_len == 1) return psk[0] <= 10;
  return psk_len == 0 || psk_len == 16 || psk_len == 32;
}

uint32_t Backend::node_id_for(const uint8_t pub[32]) {
  uint32_t crc = 0xFFFFFFFFu;
  for (size_t i = 0; i < 32; i++) {
    crc ^= pub[i];
    for (int b = 0; b < 8; b++) crc = (crc >> 1) ^ (0xEDB88320u & (0u - (crc & 1)));
  }
  crc ^= 0xFFFFFFFFu;
  // 0 is "unset" and 0xFFFFFFFF broadcast in Meshtastic; a few low numbers are reserved.
  if (crc < 4 || crc == kBroadcast) crc ^= 0x5A5A0000u;
  return crc;
}

void Backend::begin(uint32_t node_id, uint32_t random, const Channel& ch) {
  node_id_ = node_id;
  rng_ = random ? random : 0x9E3779B9u;
  for (auto& o : cache_) o = Origin();
  next_slot_ = 0;
  stats_ = Stats();
  if (!set_channel(ch)) set_channel(Channel());
}

bool Backend::set_channel(const Channel& ch) {
  if (!ch.valid()) return false;
  if (ch.psk_len == 1) {
    if (ch.psk[0] == 0) {
      key_len_ = 0;
    } else {
      std::memcpy(key_, kDefaultPsk, sizeof kDefaultPsk);
      key_[15] = uint8_t(key_[15] + ch.psk[0] - 1);
      key_len_ = 16;
    }
  } else {
    std::memcpy(key_, ch.psk, ch.psk_len);
    key_len_ = ch.psk_len;
  }
  size_t nl = strnlen(ch.name, sizeof ch.name);
  hash_ = uint8_t(xor_hash(reinterpret_cast<const uint8_t*>(ch.name), nl) ^ xor_hash(key_, key_len_));
  hop_limit_ = ch.hop_limit;
  for (auto& o : cache_) o = Origin();  // ids from the old channel mean nothing here
  return true;
}

uint32_t Backend::next_id() {
  // xorshift32; Meshtastic uses random 32-bit ids too, never 0.
  uint32_t x = rng_;
  x ^= x << 13;
  x ^= x >> 17;
  x ^= x << 5;
  rng_ = x;
  return x ? x : 1;
}

Backend::Origin* Backend::find(const uint8_t key[9]) {
  for (auto& o : cache_)
    if (o.used && std::memcmp(o.key, key, 9) == 0) return &o;
  return nullptr;
}

Backend::Origin& Backend::remember(const uint8_t key[9]) {
  Origin* o = find(key);
  if (!o) {
    o = &cache_[next_slot_];
    next_slot_ = (next_slot_ + 1) % kRelayCache;
    *o = Origin();
    o->used = true;
    std::memcpy(o->key, key, 9);
  }
  return *o;
}

void Backend::crypt(uint32_t id, uint32_t from, uint8_t* data, size_t n) const {
  if (key_len_ == 0 || n == 0) return;
  uint8_t iv[16] = {};
  put32(iv, id);  // u64 LE packet id, upper half 0
  put32(iv + 8, from);
  aes_ctr(key_, key_len_, iv, data, n);
}

size_t Backend::encode(const uint8_t* mava, size_t n, uint8_t* out, size_t cap) {
  if (!mava || n == 0 || n > kMaxPayload || !out || cap < kHeaderSize) return 0;
  uint8_t key[9];
  if (!mava_key(mava, n, key)) return 0;

  uint32_t from = node_id_, id = 0;
  uint8_t hop_limit = hop_limit_, hop_start = hop_limit_, relay = uint8_t(node_id_);
  Origin* o = find(key);
  if (o && !o->own) {
    // Rebroadcast of something Meshtastic delivered to us: relay it as a
    // Meshtastic node would, under its original identity.
    if (o->hop_limit == 0) {
      stats_.tx_hops_exhausted++;
      return 0;
    }
    from = o->from;
    id = o->id;
    hop_limit = uint8_t(o->hop_limit - 1);
    hop_start = o->hop_start;
    stats_.tx_relayed++;
  } else if (o) {
    id = o->id;  // our retransmission: same frame identity
  } else {
    Origin& mine = remember(key);
    mine.own = true;
    mine.from = node_id_;
    mine.id = id = next_id();
    mine.hop_limit = mine.hop_start = hop_limit_;
  }

  uint8_t* data = out + kHeaderSize;
  size_t dn = encode_data(mava, n, data, cap - kHeaderSize);
  if (!dn || kHeaderSize + dn > kRadioMax) return 0;
  put32(out, kBroadcast);
  put32(out + 4, from);
  put32(out + 8, id);
  out[12] = uint8_t((hop_limit & 7) | (hop_start & 7) << 5);
  out[13] = hash_;
  out[14] = 0;
  out[15] = relay;
  crypt(id, from, data, dn);
  stats_.tx++;
  return kHeaderSize + dn;
}

size_t Backend::decode(const uint8_t* frame, size_t n, uint8_t* mava, size_t cap, RxMeta* meta) {
  if (!frame || n <= kHeaderSize || n > kRadioMax || !mava) return 0;
  stats_.rx++;
  RxMeta m;
  m.to = get32(frame);
  m.from = get32(frame + 4);
  m.id = get32(frame + 8);
  m.hop_limit = frame[12] & 7;
  m.hop_start = uint8_t(frame[12] >> 5);
  m.relay_node = frame[15];
  if (frame[13] != hash_) {
    stats_.rx_other_channel++;
    return 0;
  }
  if (m.from == 0) return 0;
  // from == us: a Meshtastic node relayed our packet (a radio never hears
  // its own transmission). Passed on: the Mava router counts it as the
  // implicit ACK.
  if (m.from == node_id_) stats_.rx_own++;
  uint8_t data[kRadioMax];
  size_t dn = n - kHeaderSize;
  std::memcpy(data, frame + kHeaderSize, dn);
  crypt(m.id, m.from, data, dn);
  size_t len = decode_data(data, dn, mava, cap);
  uint8_t key[9];
  if (len == 0 || len > kMaxPayload || !mava_key(mava, len, key)) {
    stats_.rx_not_mava++;
    return 0;
  }
  Origin& o = remember(key);
  if (!o.own && (o.from == 0 || m.hop_limit > o.hop_limit)) {
    // First copy, or a copy with more hops left: relay under that identity.
    o.from = m.from;
    o.id = m.id;
    o.hop_limit = m.hop_limit;
    o.hop_start = m.hop_start;
  }
  if (meta) *meta = m;
  return len;
}

}  // namespace meshtastic
}  // namespace mesh

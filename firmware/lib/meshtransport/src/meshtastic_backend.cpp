#include "meshtastic_backend.h"

#include <Arduino.h>
#include <mbedtls/aes.h>

#include <cstring>

namespace mesh::meshtastic {
namespace {

constexpr uint8_t kDefaultPsk[16] = {
    0xd4, 0xf1, 0xbb, 0x3a, 0x20, 0x29, 0x07, 0x59,
    0xf0, 0xbc, 0xff, 0xab, 0xcf, 0x4e, 0x69, 0x01};

void put32le(uint8_t* p, uint32_t v) {
  p[0] = uint8_t(v);
  p[1] = uint8_t(v >> 8);
  p[2] = uint8_t(v >> 16);
  p[3] = uint8_t(v >> 24);
}

uint32_t get32le(const uint8_t* p) {
  return uint32_t(p[0]) | (uint32_t(p[1]) << 8) | (uint32_t(p[2]) << 16) |
         (uint32_t(p[3]) << 24);
}

}  // namespace

void Backend::begin(const PublicKey& pub) {
  node_id_ = node_id_from_public_key(pub);
  packet_counter_ = 1;

  // Default Meshtastic primary channel: LongFast + short PSK index 1.
  // The caller can later be extended to load a channel config from storage.
  channel_hash_ = uint8_t(xor_hash(reinterpret_cast<const uint8_t*>("LongFast"), 8) ^
                          xor_hash(kDefaultPsk, sizeof kDefaultPsk));

  key_len_ = sizeof kDefaultPsk;
  memcpy(key_, kDefaultPsk, sizeof kDefaultPsk);
  encrypt_ = true;
  hop_limit_ = kDefaultHopLimit;
}

uint32_t Backend::node_id_from_public_key(const PublicKey& pub) {
  // Meshtastic NodeNum is the CRC-32 of the Curve25519 public key.
  uint32_t crc = 0xFFFFFFFFu;
  for (size_t i = 0; i < 32; ++i) {
    crc ^= pub.x[i];
    for (int b = 0; b < 8; ++b)
      crc = (crc >> 1) ^ (0xEDB88320u & uint32_t(-(int32_t(crc & 1))));
  }
  return crc ^ 0xFFFFFFFFu;
}

uint8_t Backend::xor_hash(const uint8_t* p, size_t n) {
  uint8_t h = 0;
  for (size_t i = 0; i < n; ++i) h ^= p[i];
  return h;
}

size_t Backend::encode_varint(uint32_t v, uint8_t* out, size_t cap) {
  size_t n = 0;
  do {
    if (n >= cap) return 0;
    uint8_t b = uint8_t(v & 0x7f);
    v >>= 7;
    if (v) b |= 0x80;
    out[n++] = b;
  } while (v);
  return n;
}

bool Backend::read_varint(const uint8_t* p, size_t n, size_t& off, uint32_t& v) {
  v = 0;
  uint8_t shift = 0;
  while (off < n && shift < 35) {
    uint8_t b = p[off++];
    v |= uint32_t(b & 0x7f) << shift;
    if (!(b & 0x80)) return true;
    shift += 7;
  }
  return false;
}

size_t Backend::encode_data(const uint8_t* payload, size_t n, uint8_t* out, size_t cap) {
  if (!payload || n > kMaxDataPayload || cap < n + 5) return 0;

  // Data { portnum = 256; payload = bytes }.
  size_t o = 0;
  out[o++] = 0x08;  // field 1, varint
  size_t vn = encode_varint(kPrivateAppPort, out + o, cap - o);
  if (!vn) return 0;
  o += vn;
  out[o++] = 0x12;  // field 2, length-delimited
  vn = encode_varint(uint32_t(n), out + o, cap - o);
  if (!vn || o + vn + n > cap) return 0;
  o += vn;
  memcpy(out + o, payload, n);
  return o + n;
}

bool Backend::decode_data(const uint8_t* in, size_t n, uint8_t* payload, size_t cap,
                          size_t& payload_len) {
  payload_len = 0;
  size_t off = 0;
  uint32_t port = 0;

  while (off < n) {
    uint32_t key = 0;
    if (!read_varint(in, n, off, key)) return false;
    const uint32_t field = key >> 3;
    const uint32_t wire = key & 7;

    if (field == 1 && wire == 0) {
      if (!read_varint(in, n, off, port)) return false;
      continue;
    }

    if (field == 2 && wire == 2) {
      uint32_t len = 0;
      if (!read_varint(in, n, off, len) || len > cap || off + len > n) return false;
      memcpy(payload, in + off, len);
      payload_len = len;
      off += len;
      continue;
    }

    // Skip unknown protobuf fields.
    if (wire == 0) {
      uint32_t ignored = 0;
      if (!read_varint(in, n, off, ignored)) return false;
    } else if (wire == 2) {
      uint32_t len = 0;
      if (!read_varint(in, n, off, len) || off + len > n) return false;
      off += len;
    } else if (wire == 5) {
      if (off + 4 > n) return false;
      off += 4;
    } else if (wire == 1) {
      if (off + 8 > n) return false;
      off += 8;
    } else {
      return false;
    }
  }

  return port == kPrivateAppPort && payload_len != 0;
}

void Backend::crypt_ctr(const uint8_t* key, size_t key_len, uint32_t packet_id,
                        uint32_t from_node, uint8_t* data, size_t n) {
  if (!key || (key_len != 16 && key_len != 32) || !data || n == 0) return;

  uint8_t nonce[16] = {};
  uint64_t packet = packet_id;
  memcpy(nonce, &packet, sizeof packet);
  put32le(nonce + 8, from_node);

  mbedtls_aes_context aes;
  mbedtls_aes_init(&aes);
  if (mbedtls_aes_setkey_enc(&aes, key, int(key_len * 8)) != 0) {
    mbedtls_aes_free(&aes);
    return;
  }

  uint8_t stream[16] = {};
  size_t nc_off = 0;
  uint8_t stream_block[16] = {};
  mbedtls_aes_crypt_ctr(&aes, n, &nc_off, nonce, stream_block, data, data);
  mbedtls_aes_free(&aes);
}

size_t Backend::encode(const uint8_t* mava, size_t mava_len,
                       uint8_t* out, size_t cap, uint32_t packet_id) {
  if (!mava || mava_len == 0 || mava_len > kMaxDataPayload ||
      !out || cap < kHeaderSize + mava_len + 8) return 0;

  if (packet_id == 0) {
    packet_id = packet_counter_++;
    if (packet_counter_ == 0) packet_counter_ = 1;
  }

  uint8_t data[kMaxDataPayload + 8] = {};
  size_t data_len = encode_data(mava, mava_len, data, sizeof data);
  if (!data_len || data_len > kMaxDataPayload) return 0;

  put32le(out + 0, kBroadcast);
  put32le(out + 4, node_id_);
  put32le(out + 8, packet_id);
  out[12] = uint8_t((hop_limit_ & 0x07) | ((hop_limit_ & 0x07) << 5));
  out[13] = channel_hash_;
  out[14] = 0;  // no next-hop preference
  out[15] = 0;  // no relay node on origin transmission

  if (encrypt_) crypt_ctr(key_, key_len_, packet_id, node_id_, data, data_len);
  memcpy(out + kHeaderSize, data, data_len);
  return kHeaderSize + data_len;
}

size_t Backend::decode(const uint8_t* frame, size_t frame_len,
                       uint8_t* mava_out, size_t cap, RxMeta* meta) const {
  if (!frame || frame_len <= kHeaderSize || !mava_out || cap == 0) return 0;
  uint32_t from = get32le(frame + 4);
  if (from == 0) return 0;

  const uint32_t id = get32le(frame + 8);
  const uint8_t flags = frame[12];
  const uint8_t channel = frame[13];

  if (channel != channel_hash_) return 0;

  uint8_t data[kMaxDataPayload] = {};
  size_t data_len = frame_len - kHeaderSize;
  if (data_len > sizeof data) return 0;
  memcpy(data, frame + kHeaderSize, data_len);

  if (encrypt_) crypt_ctr(key_, key_len_, id, from, data, data_len);

  size_t mava_len = 0;
  if (!decode_data(data, data_len, mava_out, cap, mava_len)) return 0;

  if (meta) {
    meta->from = from;
    meta->to = get32le(frame + 0);
    meta->id = id;
    meta->hop_limit = flags & 0x07;
    meta->hop_start = uint8_t((flags >> 5) & 0x07);
    meta->channel_hash = channel;
  }
  return mava_len;
}

}  // namespace mesh::meshtastic

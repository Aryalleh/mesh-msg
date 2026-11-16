#pragma once

#include <cstddef>
#include <cstdint>

#include "identity.h"

namespace mesh::meshtastic {

// Meshtastic-compatible on-air adapter for Mava packets.
//
// Mava remains the application protocol. Meshtastic supplies the LoRa
// packet header, hop/rebroadcast semantics and channel crypto. Mava DATA
// packets are carried as a private Meshtastic application (port 256).
//
// The adapter deliberately does not implement Meshtastic PhoneAPI or
// Meshtastic text messages. It speaks the radio packet format directly.

constexpr size_t kHeaderSize = 16;
constexpr size_t kMaxRadioPayload = 255;
constexpr size_t kMaxDataPayload = 233;
constexpr size_t kPrivateAppPort = 256;
constexpr uint32_t kBroadcast = 0xFFFFFFFFu;
constexpr uint8_t kDefaultHopLimit = 3;

struct Config {
  const char* channel_name = "LongFast";
  // Meshtastic short PSK form: 0 = clear, 1 = published default PSK.
  // 16 or 32 bytes may also be supplied directly.
  const uint8_t* psk = nullptr;
  size_t psk_len = 1;
  uint8_t short_psk = 1;
  uint8_t channel_hash = 0;  // 0 = calculate from name + PSK.
  bool encrypt = true;
  uint8_t hop_limit = kDefaultHopLimit;
};

struct RxMeta {
  uint32_t from = 0;
  uint32_t to = kBroadcast;
  uint32_t id = 0;
  uint8_t hop_limit = 0;
  uint8_t hop_start = 0;
  uint8_t channel_hash = 0;
};

class Backend {
 public:
  Backend() = default;

  void begin(const PublicKey& pub);

  uint32_t node_id() const { return node_id_; }

  // Wrap one raw Mava packet (without the Mava 1-byte LoRa link header)
  // into a Meshtastic-compatible 16-byte header + Data protobuf.
  size_t encode(const uint8_t* mava, size_t mava_len,
                uint8_t* out, size_t cap, uint32_t packet_id = 0);

  // Decode a Meshtastic radio frame. Returns the inner Mava packet length
  // when this is a Mava/private-app frame, otherwise 0.
  size_t decode(const uint8_t* frame, size_t frame_len,
                uint8_t* mava_out, size_t cap, RxMeta* meta = nullptr) const;

  uint8_t channel_hash() const { return channel_hash_; }

 private:
  uint32_t node_id_ = 0;
  uint32_t packet_counter_ = 1;
  uint8_t channel_hash_ = 0;
  uint8_t key_[32] = {};
  size_t key_len_ = 0;
  bool encrypt_ = false;
  uint8_t hop_limit_ = kDefaultHopLimit;

  static uint32_t node_id_from_public_key(const PublicKey& pub);
  static uint8_t xor_hash(const uint8_t* p, size_t n);
  static size_t encode_varint(uint32_t v, uint8_t* out, size_t cap);
  static bool read_varint(const uint8_t* p, size_t n, size_t& off, uint32_t& v);
  static size_t encode_data(const uint8_t* payload, size_t n, uint8_t* out, size_t cap);
  static bool decode_data(const uint8_t* in, size_t n, uint8_t* payload, size_t cap, size_t& payload_len);
  static void crypt_ctr(const uint8_t* key, size_t key_len, uint32_t packet_id,
                        uint32_t from_node, uint8_t* data, size_t n);
};

}  // namespace mesh::meshtastic

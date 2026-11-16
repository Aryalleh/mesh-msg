// Meshtastic backbone (docs/board.md §3): Mava packets cross LoRa as the
// payload of Meshtastic private-app messages, so existing Meshtastic nodes
// repeat Mava traffic without being able to read it (it is end-to-end
// encrypted by Mava before it gets here).
//
// On air a frame is the Meshtastic radio packet:
//
//   header (16 B, little-endian)            then AES-CTR(channel key) of
//     0  to          u32  0xFFFFFFFF            Data protobuf {
//     4  from        u32  node number             1: portnum = 256 (PRIVATE_APP)
//     8  id          u32  packet id               2: payload = Mava packet }
//    12  flags       u8   hop_limit | want_ack<<3 | via_mqtt<<4 | hop_start<<5
//    13  channel     u8   xor hash of channel name and key
//    14  next_hop    u8
//    15  relay_node  u8   low byte of the node that (re)transmitted it
//
// Nonce = id (u64 LE) || from (u32 LE) || 0 (u32).
//
// Relaying keeps Meshtastic's identity of the frame: when the Mava router
// rebroadcasts a packet it first heard from Meshtastic, the backend sends it
// with the original from/id and hop_limit − 1, so Meshtastic nodes that
// already heard it drop it instead of flooding it a second time. Our own
// retransmissions reuse the id of the first transmission for the same reason.
//
// Pure C++17, no heap, no Arduino: AES comes from aes_ctr() (mbedtls on the
// board, OpenSSL in the host tests).
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace meshtastic {

constexpr size_t kHeaderSize = 16;
constexpr size_t kRadioMax = 255;
constexpr size_t kMaxPayload = 233;  // Meshtastic DATA_PAYLOAD_LEN = Mava LoRa MTU
constexpr uint32_t kPrivateAppPort = 256;
constexpr uint32_t kBroadcast = 0xFFFFFFFFu;
constexpr uint8_t kDefaultHopLimit = 3;
constexpr size_t kChannelNameMax = 11;  // Meshtastic channel names are < 12 bytes
constexpr size_t kRelayCache = 48;

// The published Meshtastic default key ("AQ==", short PSK index 1).
extern const uint8_t kDefaultPsk[16];

// AES-CTR in place, 128- or 256-bit key, counter block = iv (big-endian
// increment of the last bytes, as mbedtls / OpenSSL do). Provided by the
// platform.
void aes_ctr(const uint8_t* key, size_t key_len, const uint8_t iv[16], uint8_t* data, size_t n);

struct Channel {
  char name[kChannelNameMax + 1] = "LongFast";
  // Meshtastic PSK forms: 0 bytes = no encryption; 1 byte = short index
  // (0 = none, 1 = default key, 2..10 = default key with last byte + index − 1);
  // 16 or 32 bytes = AES-128 / AES-256 key.
  uint8_t psk[32] = {1};
  uint8_t psk_len = 1;
  uint8_t hop_limit = kDefaultHopLimit;  // 1..7

  bool valid() const;
};

struct RxMeta {
  uint32_t from = 0, to = kBroadcast, id = 0;
  uint8_t hop_limit = 0, hop_start = 0, relay_node = 0;
};

struct Stats {
  uint32_t rx = 0, rx_other_channel = 0, rx_not_mava = 0, rx_own = 0;
  uint32_t tx = 0, tx_relayed = 0, tx_hops_exhausted = 0;
};

class Backend {
 public:
  // node_id: our Meshtastic node number (node_id_for()); random: a hardware
  // random value for packet ids.
  void begin(uint32_t node_id, uint32_t random, const Channel& ch);
  bool set_channel(const Channel& ch);

  // encode wraps one Mava packet (no Mava link header) into a radio frame.
  // Returns the frame length, or 0 if it must not be sent (too large, or a
  // relayed packet whose Meshtastic hop limit is used up).
  size_t encode(const uint8_t* mava, size_t n, uint8_t* out, size_t cap);

  // decode unwraps a radio frame into the Mava packet it carries. Returns 0
  // for frames on another channel, other Meshtastic apps and garbage. Our
  // own packets relayed back are returned (the router's implicit ACK).
  size_t decode(const uint8_t* frame, size_t n, uint8_t* mava, size_t cap, RxMeta* meta = nullptr);

  uint32_t node_id() const { return node_id_; }
  uint8_t channel_hash() const { return hash_; }
  const Stats& stats() const { return stats_; }

  // Meshtastic-style node number from a 32-byte public key (CRC-32), never
  // 0 or broadcast.
  static uint32_t node_id_for(const uint8_t pub[32]);

 private:
  struct Origin {
    bool used = false;
    uint8_t key[9] = {};  // Mava packet id || fragment
    uint32_t from = 0, id = 0;
    uint8_t hop_limit = 0, hop_start = 0;
    bool own = false;
  };
  Origin* find(const uint8_t key[9]);
  Origin& remember(const uint8_t key[9]);
  uint32_t next_id();
  void crypt(uint32_t id, uint32_t from, uint8_t* data, size_t n) const;

  uint32_t node_id_ = 0;
  uint32_t rng_ = 1;
  uint8_t key_[32] = {};
  size_t key_len_ = 0;  // 0 = no encryption
  uint8_t hash_ = 0;
  uint8_t hop_limit_ = kDefaultHopLimit;
  Origin cache_[kRelayCache];
  size_t next_slot_ = 0;
  Stats stats_;
};

}  // namespace meshtastic
}  // namespace mesh

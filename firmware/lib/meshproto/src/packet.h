// Outer packet format shared by every tier (docs/protocol.md §2), mirroring
// core/packet. Pure C++17, no heap, no Arduino: runs on the boards and in the
// host test build. Must reproduce spec/test-vectors/vectors.json exactly.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {

constexpr uint8_t kVersion = 1;
constexpr size_t kHeaderSize = 26;
constexpr size_t kLoRaRadioMax = 255;  // SX126x maximum radio payload
constexpr size_t kLoRaLinkHeader = 1;  // sender short ID (docs/firmware.md §5)
constexpr size_t kLoRaMTU = kLoRaRadioMax - kLoRaLinkHeader;
constexpr size_t kMaxFragments = 16;
constexpr size_t kFragBodySize = kLoRaMTU - kHeaderSize - 1;
constexpr size_t kMaxSize = 64 * 1024;
constexpr uint8_t kDefaultMaxHops = 8;
constexpr size_t kPrologueSize = 25;

enum class Type : uint8_t {
  Data = 0,
  Receipt = 1,
  Announce = 2,
  PathReq = 3,
  Summary = 4,
  Link = 5,  // hop-local, never forwarded
};

const char* type_name(Type t);

// Ctx flags (3 bits).
constexpr uint8_t kCtxPriority = 0x1;
constexpr uint8_t kCtxNoLoRa = 0x2;
constexpr uint8_t kCtxFragmented = 0x4;

enum class Err : uint8_t {
  Ok = 0,
  Short,     // too short / truncated / output buffer too small
  Version,   // unsupported version
  TooLarge,  // over kMaxSize, or needs more than kMaxFragments
  Frag,      // invalid fragment byte or fragment set
  Range,     // field out of range
};

const char* err_name(Err e);

// Decoded packet. body excludes the fragment byte and aliases the buffer it
// was decoded from (or whatever the caller points it at before encoding).
struct Packet {
  Type type = Type::Data;
  uint8_t ctx = 0;
  uint8_t hop_count = 0;  // 0..15
  uint8_t max_hops = 0;   // 0..15
  uint8_t id[8] = {};
  uint8_t dest[16] = {};
  uint8_t frag_index = 0;  // valid if fragmented()
  uint8_t frag_count = 0;  // 1..16, valid if fragmented()
  const uint8_t* body = nullptr;
  size_t body_len = 0;

  bool fragmented() const { return (ctx & kCtxFragmented) != 0; }
  bool no_lora() const { return (ctx & kCtxNoLoRa) != 0; }
  bool priority() const { return (ctx & kCtxPriority) != 0; }
  uint8_t flags() const;
  size_t size() const { return kHeaderSize + body_len + (fragmented() ? 1 : 0); }
  // Storage / dedup key fragment number: frag_index if fragmented, else 0.
  uint8_t key_frag() const { return fragmented() ? frag_index : 0; }
};

// encode writes p into out[0..cap). On success *out_len is the encoded size.
Err encode(const Packet& p, uint8_t* out, size_t cap, size_t* out_len);

// decode parses raw bytes; out.body aliases b.
Err decode(const uint8_t* b, size_t n, Packet& out);

// prologue writes the 25 header bytes bound into end-to-end encryption:
// flags with ctx masked out || packet_id || dest_hash.
void prologue(Type t, const uint8_t id[8], const uint8_t dest[16], uint8_t out[kPrologueSize]);

// inc_hop increments the hop count of an encoded packet in place. It returns
// false (leaving raw untouched) if the packet reached its hop limit and must
// not be forwarded.
bool inc_hop(uint8_t* raw, size_t n);

// fragment_count returns how many packets p becomes when sent over a link
// with the given maximum packet size: 1 if it already fits, 0 if it cannot be
// sent (already fragmented, max_size too small, or more than kMaxFragments).
size_t fragment_count(const Packet& p, size_t max_size);

// fragment fills out with fragment `index` of p (see fragment_count). If p
// fits unfragmented, index 0 yields p itself. out.body aliases p.body.
Err fragment(const Packet& p, size_t max_size, size_t index, Packet& out);

// reassemble joins a complete set of n fragments (any order) into one
// unfragmented packet whose body is written to body_buf.
Err reassemble(const Packet* frags, size_t n, uint8_t* body_buf, size_t cap, Packet& out);

// LoRa link frame = sender short ID || packet.
Err encode_link_frame(uint8_t sender, const uint8_t* raw, size_t n, uint8_t* out, size_t cap,
                      size_t* out_len);
Err decode_link_frame(const uint8_t* frame, size_t n, uint8_t* sender, const uint8_t** raw,
                      size_t* raw_len);

}  // namespace mesh

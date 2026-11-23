// Hamyar presence (Hamyar docs/presence.md, protocol v1): a Mava node in a dorm, building or classroom
//  - advertises a beacon that phones with the Hamyar app hear (phone hears node), and
//  - collects the tokens phones advertise and posts them, signed, to the university (node hears phone).
// Tokens: first 8 bytes of HMAC-SHA256(key, "beacon|<w>") / "phone|<w>", w = unix seconds / 120.
// Report signature: hex HMAC-SHA256(node key, "report|" + body). Pinned to spec/test-vectors/presence.json
// (made by the Hamyar server code). Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace presence {

constexpr uint32_t kWindowSec = 120;
constexpr size_t kTokenLen = 8;
constexpr size_t kAdvLen = 16;  // "MV"/"MP" | version | flags | id (u32 BE) | token (8)
constexpr uint8_t kVersion = 1;
constexpr size_t kMaxHeard = 128;  // distinct phones per window kept by one node

inline uint32_t window_of(uint64_t unix_sec) { return uint32_t(unix_sec / kWindowSec); }

void beacon_token(const uint8_t key[32], uint32_t window, uint8_t out[kTokenLen]);
void phone_token(const uint8_t key[32], uint32_t window, uint8_t out[kTokenLen]);

// Advert payload (the bytes after the manufacturer id). kind 'V' = node beacon, 'P' = phone.
void build_adv(char kind, uint32_t id, const uint8_t token[kTokenLen], uint8_t out[kAdvLen]);
struct Adv {
  char kind;
  uint32_t id;
  uint8_t token[kTokenLen];
};
bool parse_adv(const uint8_t* p, size_t n, Adv* out);

// Phones heard by a node in one window: distinct tokens, strongest RSSI kept. A new window clears it.
class Collector {
 public:
  explicit Collector(uint32_t window = 0) : window_(window) {}
  // False when full (the token is dropped) or the token belongs to another window.
  bool add(uint32_t window, const uint8_t token[kTokenLen], int8_t rssi);
  uint32_t window() const { return window_; }
  size_t size() const { return n_; }
  void reset(uint32_t window) {
    window_ = window;
    n_ = 0;
  }
  // {"w":<w>,"heard":[{"t":"<16 hex>","r":<rssi>},...]} ; returns the length, 0 if cap is too small.
  size_t report_body(char* out, size_t cap) const;

 private:
  struct Entry {
    uint8_t token[kTokenLen];
    int8_t rssi;
  };
  uint32_t window_;
  size_t n_ = 0;
  Entry e_[kMaxHeard];
};

// 64 hex chars + NUL.
void report_sig(const uint8_t key[32], const char* body, size_t n, char out[65]);
// "0011..." -> 32 bytes. False on bad input.
bool key_from_hex(const char* hex, uint8_t key[32]);

}  // namespace presence
}  // namespace mesh

#include "presence.h"

#include <cstdio>
#include <cstring>

#include "callcrypto.h"

namespace mesh {
namespace presence {

namespace {
void token(const uint8_t key[32], const char* what, uint32_t window, uint8_t out[kTokenLen]) {
  char msg[32];
  int n = snprintf(msg, sizeof msg, "%s|%lu", what, (unsigned long)window);
  uint8_t mac[32];
  call::hmac_sha256(key, 32, reinterpret_cast<const uint8_t*>(msg), size_t(n), nullptr, 0, mac);
  memcpy(out, mac, kTokenLen);
}
const char kHex[] = "0123456789abcdef";
int nib(char c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  return -1;
}
}  // namespace

void beacon_token(const uint8_t key[32], uint32_t window, uint8_t out[kTokenLen]) { token(key, "beacon", window, out); }
void phone_token(const uint8_t key[32], uint32_t window, uint8_t out[kTokenLen]) { token(key, "phone", window, out); }

void build_adv(char kind, uint32_t id, const uint8_t tok[kTokenLen], uint8_t out[kAdvLen]) {
  out[0] = 'M';
  out[1] = uint8_t(kind);
  out[2] = kVersion;
  out[3] = 0;
  out[4] = uint8_t(id >> 24);
  out[5] = uint8_t(id >> 16);
  out[6] = uint8_t(id >> 8);
  out[7] = uint8_t(id);
  memcpy(out + 8, tok, kTokenLen);
}

bool parse_adv(const uint8_t* p, size_t n, Adv* out) {
  if (n < kAdvLen || p[0] != 'M' || (p[1] != 'V' && p[1] != 'P') || p[2] != kVersion) return false;
  out->kind = char(p[1]);
  out->id = (uint32_t(p[4]) << 24) | (uint32_t(p[5]) << 16) | (uint32_t(p[6]) << 8) | p[7];
  memcpy(out->token, p + 8, kTokenLen);
  return true;
}

bool Collector::add(uint32_t window, const uint8_t tok[kTokenLen], int8_t rssi) {
  if (window != window_) return false;
  for (size_t i = 0; i < n_; i++) {
    if (memcmp(e_[i].token, tok, kTokenLen) == 0) {
      if (rssi > e_[i].rssi) e_[i].rssi = rssi;
      return true;
    }
  }
  if (n_ == kMaxHeard) return false;
  memcpy(e_[n_].token, tok, kTokenLen);
  e_[n_].rssi = rssi;
  n_++;
  return true;
}

size_t Collector::report_body(char* out, size_t cap) const {
  size_t k = 0;
  int w = snprintf(out, cap, "{\"w\":%lu,\"heard\":[", (unsigned long)window_);
  if (w < 0 || size_t(w) >= cap) return 0;
  k = size_t(w);
  for (size_t i = 0; i < n_; i++) {
    char hex[kTokenLen * 2 + 1];
    for (size_t j = 0; j < kTokenLen; j++) {
      hex[2 * j] = kHex[e_[i].token[j] >> 4];
      hex[2 * j + 1] = kHex[e_[i].token[j] & 15];
    }
    hex[kTokenLen * 2] = 0;
    w = snprintf(out + k, cap - k, "%s{\"t\":\"%s\",\"r\":%d}", i ? "," : "", hex, int(e_[i].rssi));
    if (w < 0 || size_t(w) >= cap - k) return 0;
    k += size_t(w);
  }
  if (cap - k < 3) return 0;
  out[k++] = ']';
  out[k++] = '}';
  out[k] = 0;
  return k;
}

void report_sig(const uint8_t key[32], const char* body, size_t n, char out[65]) {
  static const char kPre[] = "report|";
  uint8_t mac[32];
  call::hmac_sha256(key, 32, reinterpret_cast<const uint8_t*>(kPre), sizeof kPre - 1, reinterpret_cast<const uint8_t*>(body), n, mac);
  for (int i = 0; i < 32; i++) {
    out[2 * i] = kHex[mac[i] >> 4];
    out[2 * i + 1] = kHex[mac[i] & 15];
  }
  out[64] = 0;
}

bool key_from_hex(const char* hex, uint8_t key[32]) {
  if (!hex || strlen(hex) != 64) return false;
  for (int i = 0; i < 32; i++) {
    int a = nib(hex[2 * i]), b = nib(hex[2 * i + 1]);
    if (a < 0 || b < 0) return false;
    key[i] = uint8_t(a << 4 | b);
  }
  return true;
}

}  // namespace presence
}  // namespace mesh

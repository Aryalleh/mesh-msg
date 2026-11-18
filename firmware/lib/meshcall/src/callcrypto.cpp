#include "callcrypto.h"

#include <cstdio>
#include <cstring>

#include "monocypher.h"
#include "sha256.h"

namespace mesh {
namespace call {
namespace {

void le64(uint8_t* p, uint64_t v) {
  for (int i = 0; i < 8; i++) p[i] = uint8_t(v >> (8 * i));
}

void poly_pad(crypto_poly1305_ctx* c, size_t n) {
  static const uint8_t zero[16] = {};
  if (n % 16) crypto_poly1305_update(c, zero, 16 - n % 16);
}

void aead_tag(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* ct, size_t n,
              uint8_t tag[16]) {
  uint8_t block[64] = {};
  crypto_chacha20_ietf(block, block, sizeof block, key, nonce, 0);  // the one-time Poly1305 key
  crypto_poly1305_ctx c;
  crypto_poly1305_init(&c, block);
  crypto_poly1305_update(&c, ad, ad_n);
  poly_pad(&c, ad_n);
  crypto_poly1305_update(&c, ct, n);
  poly_pad(&c, n);
  uint8_t lens[16];
  le64(lens, ad_n);
  le64(lens + 8, n);
  crypto_poly1305_update(&c, lens, sizeof lens);
  crypto_poly1305_final(&c, tag);
  crypto_wipe(block, sizeof block);
}

// Noise's nonce: 32 zero bits, then n little-endian.
void noise_nonce(uint64_t n, uint8_t out[12]) {
  std::memset(out, 0, 4);
  le64(out + 4, n);
}

// Noise HKDF with two outputs.
void noise_hkdf(const uint8_t ck[32], const uint8_t* ikm, size_t ikm_n, uint8_t o1[32], uint8_t o2[32]) {
  uint8_t temp[32];
  hmac_sha256(ck, 32, ikm, ikm_n, nullptr, 0, temp);
  const uint8_t one = 1, two = 2;
  hmac_sha256(temp, 32, &one, 1, nullptr, 0, o1);
  hmac_sha256(temp, 32, o1, 32, &two, 1, o2);
  crypto_wipe(temp, sizeof temp);
}

}  // namespace

void hmac_sha256(const uint8_t* key, size_t key_n, const uint8_t* a, size_t a_n, const uint8_t* b, size_t b_n,
                 uint8_t out[32]) {
  uint8_t k[64] = {};
  if (key_n > 64) {
    Sha256::hash(key, key_n, k);
  } else if (key_n) {
    std::memcpy(k, key, key_n);
  }
  uint8_t pad[64];
  for (int i = 0; i < 64; i++) pad[i] = uint8_t(k[i] ^ 0x36);
  Sha256 in;
  in.update(pad, 64);
  if (a_n) in.update(a, a_n);
  if (b_n) in.update(b, b_n);
  uint8_t inner[32];
  in.finish(inner);
  for (int i = 0; i < 64; i++) pad[i] = uint8_t(k[i] ^ 0x5c);
  Sha256 o;
  o.update(pad, 64);
  o.update(inner, 32);
  o.finish(out);
  crypto_wipe(k, sizeof k);
}

void hkdf_sha256(const uint8_t* ikm, size_t ikm_n, const char* info, uint8_t out[32]) {
  uint8_t prk[32];
  hmac_sha256(nullptr, 0, ikm, ikm_n, nullptr, 0, prk);  // salt: none (HashLen zeros)
  const uint8_t one = 1;
  uint8_t t[128];
  size_t n = std::strlen(info);
  if (n > sizeof t - 1) n = sizeof t - 1;
  std::memcpy(t, info, n);
  t[n] = one;
  hmac_sha256(prk, 32, t, n + 1, nullptr, 0, out);
  crypto_wipe(prk, sizeof prk);
}

void aead_seal(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* pt,
               size_t n, uint8_t* out) {
  if (n) crypto_chacha20_ietf(out, pt, n, key, nonce, 1);
  aead_tag(key, nonce, ad, ad_n, out, n, out + n);
}

bool aead_open(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* in,
               size_t n, uint8_t* out) {
  if (n < kTag) return false;
  size_t m = n - kTag;
  uint8_t tag[16];
  aead_tag(key, nonce, ad, ad_n, in, m, tag);
  if (crypto_verify16(tag, in + m) != 0) return false;
  if (m) crypto_chacha20_ietf(out, in, m, key, nonce, 1);
  return true;
}

// ---- Noise KK ----

NoiseKK::NoiseKK(bool initiator, const Identity& self, const uint8_t peer_x[32], const uint8_t* prologue,
                 size_t prologue_n, const uint8_t eph[32])
    : self_(self), initiator_(initiator) {
  std::memcpy(rs_, peer_x, 32);
  std::memcpy(e_, eph, 32);
  crypto_x25519_public_key(e_pub_, e_);
  static const char kName[] = "Noise_KK_25519_ChaChaPoly_SHA256";  // exactly 32 bytes: h = name
  std::memcpy(h_, kName, 32);
  std::memcpy(ck_, h_, 32);
  mix_hash(prologue, prologue_n);
  // Pre-messages: -> s, <- s.
  const uint8_t* is = initiator ? self.pub().x : rs_;
  const uint8_t* rsp = initiator ? rs_ : self.pub().x;
  mix_hash(is, 32);
  mix_hash(rsp, 32);
}

NoiseKK::~NoiseKK() {
  crypto_wipe(e_, sizeof e_);
  crypto_wipe(k_, sizeof k_);
  crypto_wipe(ck_, sizeof ck_);
  crypto_wipe(send_, sizeof send_);
  crypto_wipe(recv_, sizeof recv_);
}

void NoiseKK::mix_hash(const uint8_t* d, size_t n) {
  Sha256 s;
  s.update(h_, 32);
  if (n) s.update(d, n);
  s.finish(h_);
}

void NoiseKK::mix_key(const uint8_t ikm[32]) {
  noise_hkdf(ck_, ikm, 32, ck_, k_);
  has_k_ = true;
  n_ = 0;
}

void NoiseKK::seal_payload(uint8_t tag[kTag]) {
  uint8_t nonce[12];
  noise_nonce(n_++, nonce);
  aead_seal(k_, nonce, h_, 32, nullptr, 0, tag);
  mix_hash(tag, kTag);
}

bool NoiseKK::open_payload(const uint8_t tag[kTag]) {
  uint8_t nonce[12];
  noise_nonce(n_, nonce);
  uint8_t none[1];
  if (!aead_open(k_, nonce, h_, 32, tag, kTag, none)) return false;
  n_++;
  mix_hash(tag, kTag);
  return true;
}

void NoiseKK::split() {
  uint8_t k1[32], k2[32];
  noise_hkdf(ck_, nullptr, 0, k1, k2);
  std::memcpy(initiator_ ? send_ : recv_, k1, 32);
  std::memcpy(initiator_ ? recv_ : send_, k2, 32);
  crypto_wipe(k1, sizeof k1);
  crypto_wipe(k2, sizeof k2);
  done_ = true;
}

size_t NoiseKK::write(uint8_t out[kMsgSize]) {
  uint8_t dh[32];
  if (initiator_ && step_ == 0) {
    // -> e, es, ss
    std::memcpy(out, e_pub_, 32);
    mix_hash(e_pub_, 32);
    crypto_x25519(dh, e_, rs_);
    mix_key(dh);
    if (!self_.dh(rs_, dh)) return 0;
    mix_key(dh);
    seal_payload(out + 32);
    step_ = 1;
  } else if (!initiator_ && step_ == 1) {
    // <- e, ee, se
    std::memcpy(out, e_pub_, 32);
    mix_hash(e_pub_, 32);
    crypto_x25519(dh, e_, re_);
    mix_key(dh);
    crypto_x25519(dh, e_, rs_);
    mix_key(dh);
    seal_payload(out + 32);
    step_ = 2;
    split();
  } else {
    return 0;
  }
  crypto_wipe(dh, sizeof dh);
  return kMsgSize;
}

bool NoiseKK::read(const uint8_t* m, size_t n) {
  if (n != kMsgSize) return false;
  uint8_t dh[32];
  if (!initiator_ && step_ == 0) {
    std::memcpy(re_, m, 32);
    mix_hash(re_, 32);
    if (!self_.dh(re_, dh)) return false;  // es
    mix_key(dh);
    if (!self_.dh(rs_, dh)) return false;  // ss
    mix_key(dh);
    if (!open_payload(m + 32)) return false;
    step_ = 1;
  } else if (initiator_ && step_ == 1) {
    std::memcpy(re_, m, 32);
    mix_hash(re_, 32);
    crypto_x25519(dh, e_, re_);  // ee
    mix_key(dh);
    if (!self_.dh(re_, dh)) return false;  // se
    mix_key(dh);
    if (!open_payload(m + 32)) return false;
    step_ = 2;
    split();
  } else {
    return false;
  }
  crypto_wipe(dh, sizeof dh);
  return true;
}

void NoiseKK::encrypt(uint64_t n, const uint8_t* ad, size_t ad_n, const uint8_t* pt, size_t len, uint8_t* out) const {
  uint8_t nonce[12];
  noise_nonce(n, nonce);
  aead_seal(send_, nonce, ad, ad_n, pt, len, out);
}

bool NoiseKK::decrypt(uint64_t n, const uint8_t* ad, size_t ad_n, const uint8_t* in, size_t len, uint8_t* out) const {
  uint8_t nonce[12];
  noise_nonce(n, nonce);
  return aead_open(recv_, nonce, ad, ad_n, in, len, out);
}

void call_code(const uint8_t hh[32], char out[8]) {
  static const char kLabel[] = "mava call code";
  Sha256 s;
  s.update(reinterpret_cast<const uint8_t*>(kLabel), sizeof kLabel - 1);
  s.update(hh, 32);
  uint8_t d[32];
  s.finish(d);
  uint32_t v = (uint32_t(d[0]) << 24 | uint32_t(d[1]) << 16 | uint32_t(d[2]) << 8 | d[3]) % 1000000;
  std::snprintf(out, 8, "%03u %03u", unsigned(v / 1000), unsigned(v % 1000));
}

}  // namespace call
}  // namespace mesh

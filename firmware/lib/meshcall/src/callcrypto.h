// Crypto for calls and the direct path (docs/calls.md §3, connectivity.md),
// byte-compatible with core/call (flynn/noise) and core/direct:
//   HMAC-SHA256, HKDF-SHA256 (no salt, one 32-byte block), ChaCha20-Poly1305
//   (RFC 8439), and Noise_KK_25519_ChaChaPoly_SHA256 with empty payloads.
// Checked against spec/test-vectors/calls.json (made by the Go code).
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

#include "identity.h"

namespace mesh {
namespace call {

constexpr size_t kTag = 16;

void hmac_sha256(const uint8_t* key, size_t key_n, const uint8_t* a, size_t a_n, const uint8_t* b, size_t b_n,
                 uint8_t out[32]);
// HKDF-SHA256 with an empty salt; the first 32 bytes (Go's hkdf.New(sha256, key, nil, info)).
void hkdf_sha256(const uint8_t* ikm, size_t ikm_n, const char* info, uint8_t out[32]);

// ChaCha20-Poly1305: out = ciphertext (n bytes) || tag (16).
void aead_seal(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* pt,
               size_t n, uint8_t* out);
// in = ciphertext || tag (n ≥ 16); writes n − 16 bytes to out. False: forged or corrupt.
bool aead_open(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* in,
               size_t n, uint8_t* out);

// Noise KK, both static keys known (the contacts' X25519 keys); fresh
// ephemeral keys per call; two 48-byte messages, then two transport keys.
class NoiseKK {
 public:
  static constexpr size_t kMsgSize = 32 + kTag;

  // eph: 32 random bytes, the ephemeral private key.
  NoiseKK(bool initiator, const Identity& self, const uint8_t peer_x[32], const uint8_t* prologue, size_t prologue_n,
          const uint8_t eph[32]);
  ~NoiseKK();

  bool initiator() const { return initiator_; }
  // Initiator: message 1. Responder: message 2 (after read1). Returns 0 out of turn.
  size_t write(uint8_t out[kMsgSize]);
  // Responder: message 1. Initiator: message 2. False if it does not authenticate.
  bool read(const uint8_t* m, size_t n);
  bool done() const { return done_; }
  const uint8_t* handshake_hash() const { return h_; }

  // Transport, after done(): explicit nonce n (Noise counter), out = ct || tag.
  void encrypt(uint64_t n, const uint8_t* ad, size_t ad_n, const uint8_t* pt, size_t len, uint8_t* out) const;
  bool decrypt(uint64_t n, const uint8_t* ad, size_t ad_n, const uint8_t* in, size_t len, uint8_t* out) const;

 private:
  void mix_hash(const uint8_t* d, size_t n);
  void mix_key(const uint8_t ikm[32]);
  void seal_payload(uint8_t tag[kTag]);       // EncryptAndHash(empty)
  bool open_payload(const uint8_t tag[kTag]);  // DecryptAndHash(empty)
  void split();

  const Identity& self_;
  bool initiator_;
  uint8_t rs_[32];
  uint8_t e_[32], e_pub_[32], re_[32] = {};
  uint8_t h_[32], ck_[32], k_[32] = {};
  uint64_t n_ = 0;
  bool has_k_ = false;
  int step_ = 0;  // messages done
  bool done_ = false;
  uint8_t send_[32] = {}, recv_[32] = {};
};

// The short code both people compare by voice: "123 456".
void call_code(const uint8_t handshake_hash[32], char out[8]);

}  // namespace call
}  // namespace mesh

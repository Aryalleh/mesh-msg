// Identity keys (docs/protocol.md §1), mirroring core/identity: an X25519 key
// for encryption and an Ed25519 key for signatures. A node uses its own
// identity only to sign its announces; it never decrypts user traffic.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {

constexpr size_t kPublicSize = 64;
constexpr size_t kSeedSize = 64;
constexpr size_t kSigSize = 64;
// Largest domain-tagged message sign_domain/verify_domain accept. Every
// signed message in the protocol is far smaller.
constexpr size_t kMaxSignedMsg = 256;

struct PublicKey {
  uint8_t x[32] = {};
  uint8_t ed[32] = {};

  static PublicKey from_bytes(const uint8_t b[kPublicSize]);
  // Destination hash: SHA-256(x25519 || ed25519)[:16].
  void hash(uint8_t out[16]) const;
  // Verifies an Ed25519 signature over domain || msg.
  bool verify(const char* domain, const uint8_t* msg, size_t n, const uint8_t sig[kSigSize]) const;
};

class Identity {
 public:
  // seed = x25519 private (32) || ed25519 seed (32).
  static Identity from_seed(const uint8_t seed[kSeedSize]);
  ~Identity();

  const PublicKey& pub() const { return pub_; }
  // Signs domain || msg. Returns false if the message is too long.
  bool sign(const char* domain, const uint8_t* msg, size_t n, uint8_t sig[kSigSize]) const;
  void wipe();

 private:
  PublicKey pub_;
  uint8_t x_priv_[32] = {};
  uint8_t ed_secret_[64] = {};  // Monocypher form: seed || public
};

}  // namespace mesh

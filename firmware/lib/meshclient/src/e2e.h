// End-to-end encryption of DATA bodies (docs/protocol.md §3), byte-identical
// to core/e2e (Go): Noise_K_25519_ChaChaPoly_SHA256 to a known contact,
// Noise_X_25519_ChaChaPoly_SHA256 at first contact (the sender's Ed25519 key
// travels encrypted in front of the inner payload). Checked against the e2e
// vectors in spec/test-vectors. Pure C++, no heap.
#pragma once

#include <cstddef>
#include <cstdint>

#include "identity.h"

namespace mesh {
namespace e2e {

constexpr size_t kOverheadK = 32 + 16;
constexpr size_t kOverheadX = 32 + 48 + 16;
constexpr size_t kInnerHeader = 5;  // content type (1) + Unix time (4, big endian)

constexpr uint8_t kText = 0;
constexpr uint8_t kTextCompact = 1;
constexpr uint8_t kCall = 7;  // call signaling (docs/calls.md §2)

// Seals the inner payload (type | time | content) from sender to recipient.
// known: Noise K, else Noise X. ephemeral: 32 random bytes (the e private
// key). Returns the body length, 0 if cap is too small.
size_t seal(const Identity& sender, const PublicKey& recipient, bool known, const uint8_t packet_id[8],
            const uint8_t ephemeral[32], uint8_t content_type, uint32_t time, const uint8_t* content, size_t n,
            uint8_t* out, size_t cap);

struct Opened {
  PublicKey from;
  bool first_contact = false;  // Noise X
  int contact = -1;            // index into contacts for Noise K
  uint8_t content_type = 0;
  uint32_t time = 0;
  const uint8_t* content = nullptr;  // points into plain
  size_t len = 0;
};

// Opens a body addressed to me: Noise X first, then Noise K against each
// contact. plain must hold the body length. False: not for me / tampered.
bool open(const Identity& me, const PublicKey* contacts, size_t n_contacts, const uint8_t packet_id[8],
          const uint8_t* body, size_t n, uint8_t* plain, size_t cap, Opened& out);

// IETF ChaCha20-Poly1305 (RFC 8439), exposed for tests.
void aead_seal(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* pt,
               size_t n, uint8_t* ct, uint8_t tag[16]);
bool aead_open(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* ct,
               size_t n, const uint8_t tag[16], uint8_t* pt);

}  // namespace e2e
}  // namespace mesh

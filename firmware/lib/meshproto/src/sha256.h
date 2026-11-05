// Small SHA-256 (FIPS 180-4) for destination hashes and receipt IDs.
// Monocypher has no SHA-256, and the protocol fixes it for those two uses.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {

class Sha256 {
 public:
  Sha256();
  void update(const uint8_t* data, size_t n);
  void finish(uint8_t out[32]);

  static void hash(const uint8_t* data, size_t n, uint8_t out[32]);

 private:
  void block(const uint8_t* p);
  uint32_t h_[8];
  uint8_t buf_[64];
  size_t buf_len_;
  uint64_t total_;
};

}  // namespace mesh

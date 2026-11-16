// aes_ctr() on the board: mbedtls (hardware AES on the ESP32-S3). The host
// tests provide their own (test/meshtastic_test.cpp, OpenSSL).
#if defined(ESP_PLATFORM)
#include <mbedtls/aes.h>

#include <cstring>

#include "meshtastic.h"

namespace mesh {
namespace meshtastic {

void aes_ctr(const uint8_t* key, size_t key_len, const uint8_t iv[16], uint8_t* data, size_t n) {
  mbedtls_aes_context aes;
  mbedtls_aes_init(&aes);
  if (mbedtls_aes_setkey_enc(&aes, key, unsigned(key_len * 8)) == 0) {
    uint8_t ctr[16], block[16];
    std::memcpy(ctr, iv, 16);
    size_t off = 0;
    mbedtls_aes_crypt_ctr(&aes, n, &off, ctr, block, data, data);
  }
  mbedtls_aes_free(&aes);
}

}  // namespace meshtastic
}  // namespace mesh
#endif

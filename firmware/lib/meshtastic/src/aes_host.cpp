// aes_ctr() off the board (host tests, simulator): OpenSSL. The board build
// uses aes_mbedtls.cpp.
#if !defined(ESP_PLATFORM)
#include <openssl/evp.h>

#include "meshtastic.h"

namespace mesh {
namespace meshtastic {

void aes_ctr(const uint8_t* key, size_t key_len, const uint8_t iv[16], uint8_t* data, size_t n) {
  EVP_CIPHER_CTX* c = EVP_CIPHER_CTX_new();
  EVP_EncryptInit_ex(c, key_len == 32 ? EVP_aes_256_ctr() : EVP_aes_128_ctr(), nullptr, key, iv);
  int out = 0;
  EVP_EncryptUpdate(c, data, &out, data, int(n));
  EVP_CIPHER_CTX_free(c);
}

}  // namespace meshtastic
}  // namespace mesh
#endif

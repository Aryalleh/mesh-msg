#include "identity.h"

#include <cstring>

#include "monocypher-ed25519.h"
#include "monocypher.h"
#include "sha256.h"

namespace mesh {

namespace {

// Builds domain || msg in buf; returns its length or 0 if it does not fit.
size_t domain_msg(const char* domain, const uint8_t* msg, size_t n, uint8_t buf[kMaxSignedMsg]) {
  const size_t dl = std::strlen(domain);
  if (dl + n > kMaxSignedMsg) return 0;
  std::memcpy(buf, domain, dl);
  if (n) std::memcpy(buf + dl, msg, n);
  return dl + n;
}

}  // namespace

PublicKey PublicKey::from_bytes(const uint8_t b[kPublicSize]) {
  PublicKey p;
  std::memcpy(p.x, b, 32);
  std::memcpy(p.ed, b + 32, 32);
  return p;
}

void PublicKey::hash(uint8_t out[16]) const {
  Sha256 s;
  s.update(x, 32);
  s.update(ed, 32);
  uint8_t sum[32];
  s.finish(sum);
  std::memcpy(out, sum, 16);
}

bool PublicKey::verify(const char* domain, const uint8_t* msg, size_t n, const uint8_t sig[kSigSize]) const {
  uint8_t buf[kMaxSignedMsg];
  const size_t len = domain_msg(domain, msg, n, buf);
  if (len == 0) return false;
  return crypto_ed25519_check(sig, ed, buf, len) == 0;
}

Identity Identity::from_seed(const uint8_t seed[kSeedSize]) {
  Identity id;
  std::memcpy(id.x_priv_, seed, 32);
  crypto_x25519_public_key(id.pub_.x, id.x_priv_);
  uint8_t ed_seed[32];  // crypto_ed25519_key_pair wipes its seed argument
  std::memcpy(ed_seed, seed + 32, 32);
  crypto_ed25519_key_pair(id.ed_secret_, id.pub_.ed, ed_seed);
  return id;
}

Identity::~Identity() { wipe(); }

bool Identity::sign(const char* domain, const uint8_t* msg, size_t n, uint8_t sig[kSigSize]) const {
  uint8_t buf[kMaxSignedMsg];
  const size_t len = domain_msg(domain, msg, n, buf);
  if (len == 0) return false;
  crypto_ed25519_sign(sig, ed_secret_, buf, len);
  return true;
}

void Identity::wipe() {
  crypto_wipe(x_priv_, sizeof x_priv_);
  crypto_wipe(ed_secret_, sizeof ed_secret_);
}

}  // namespace mesh

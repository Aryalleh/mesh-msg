#include "e2e.h"

#include <cstring>

#include "monocypher.h"
#include "packet.h"
#include "sha256.h"

namespace mesh {
namespace e2e {
namespace {

void hmac(const uint8_t key[32], const uint8_t* a, size_t an, const uint8_t* b, size_t bn, uint8_t out[32]) {
  uint8_t pad[64];
  std::memset(pad, 0, sizeof pad);
  std::memcpy(pad, key, 32);
  for (auto& c : pad) c ^= 0x36;
  Sha256 in;
  in.update(pad, 64);
  in.update(a, an);
  if (bn) in.update(b, bn);
  uint8_t inner[32];
  in.finish(inner);
  for (auto& c : pad) c ^= 0x36 ^ 0x5c;
  Sha256 o;
  o.update(pad, 64);
  o.update(inner, 32);
  o.finish(out);
  crypto_wipe(pad, sizeof pad);
}

// Noise HKDF with two outputs.
void hkdf2(const uint8_t ck[32], const uint8_t ikm[32], uint8_t o1[32], uint8_t o2[32]) {
  uint8_t temp[32], one = 1, two[33];
  hmac(ck, ikm, 32, nullptr, 0, temp);
  hmac(temp, &one, 1, nullptr, 0, o1);
  std::memcpy(two, o1, 32);
  two[32] = 2;
  hmac(temp, two, 33, nullptr, 0, o2);
  crypto_wipe(temp, sizeof temp);
}

void le64(uint8_t* p, uint64_t v) {
  for (int i = 0; i < 8; i++) p[i] = uint8_t(v >> (8 * i));
}

void poly_mac(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* ct,
              size_t n, uint8_t tag[16]) {
  uint8_t block[64] = {};
  crypto_chacha20_ietf(block, nullptr, 64, key, nonce, 0);  // counter 0: the Poly1305 key
  crypto_poly1305_ctx p;
  crypto_poly1305_init(&p, block);
  static const uint8_t zero[16] = {};
  crypto_poly1305_update(&p, ad, ad_n);
  crypto_poly1305_update(&p, zero, (16 - ad_n % 16) % 16);
  crypto_poly1305_update(&p, ct, n);
  crypto_poly1305_update(&p, zero, (16 - n % 16) % 16);
  uint8_t lens[16];
  le64(lens, ad_n);
  le64(lens + 8, n);
  crypto_poly1305_update(&p, lens, 16);
  crypto_poly1305_final(&p, tag);
  crypto_wipe(block, sizeof block);
}

struct Sym {
  uint8_t ck[32], h[32], k[32];
  bool has_k = false;
  uint64_t n = 0;

  explicit Sym(const char* name) {
    std::memset(h, 0, 32);
    std::memcpy(h, name, std::strlen(name));  // both names are 31 bytes: padded, not hashed
    std::memcpy(ck, h, 32);
  }
  ~Sym() {
    crypto_wipe(ck, 32);
    crypto_wipe(k, 32);
  }
  void mix_hash(const uint8_t* d, size_t len) {
    Sha256 s;
    s.update(h, 32);
    s.update(d, len);
    s.finish(h);
  }
  void mix_key(const uint8_t ikm[32]) {
    uint8_t ck2[32];
    hkdf2(ck, ikm, ck2, k);
    std::memcpy(ck, ck2, 32);
    has_k = true;
    n = 0;
  }
  void nonce(uint8_t out[12]) {
    std::memset(out, 0, 4);
    le64(out + 4, n);
  }
  // Writes len + 16 bytes.
  void encrypt_and_hash(const uint8_t* pt, size_t len, uint8_t* out) {
    uint8_t nn[12];
    nonce(nn);
    aead_seal(k, nn, h, 32, pt, len, out, out + len);
    n++;
    mix_hash(out, len + 16);
  }
  bool decrypt_and_hash(const uint8_t* ct, size_t len, uint8_t* out) {  // len includes the tag
    if (len < 16) return false;
    uint8_t nn[12];
    nonce(nn);
    if (!aead_open(k, nn, h, 32, ct, len - 16, ct + len - 16, out)) return false;
    n++;
    mix_hash(ct, len);
    return true;
  }
};

const char kNameK[] = "Noise_K_25519_ChaChaPoly_SHA256";
const char kNameX[] = "Noise_X_25519_ChaChaPoly_SHA256";

void start(Sym& s, const uint8_t packet_id[8], const PublicKey& recipient) {
  uint8_t dest[16], pro[kPrologueSize];
  recipient.hash(dest);
  prologue(Type::Data, packet_id, dest, pro);
  s.mix_hash(pro, sizeof pro);
}

}  // namespace

void aead_seal(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* pt,
               size_t n, uint8_t* ct, uint8_t tag[16]) {
  crypto_chacha20_ietf(ct, pt, n, key, nonce, 1);
  poly_mac(key, nonce, ad, ad_n, ct, n, tag);
}

bool aead_open(const uint8_t key[32], const uint8_t nonce[12], const uint8_t* ad, size_t ad_n, const uint8_t* ct,
               size_t n, const uint8_t tag[16], uint8_t* pt) {
  uint8_t want[16];
  poly_mac(key, nonce, ad, ad_n, ct, n, want);
  if (crypto_verify16(want, tag) != 0) return false;
  crypto_chacha20_ietf(pt, ct, n, key, nonce, 1);
  return true;
}

size_t seal(const Identity& sender, const PublicKey& recipient, bool known, const uint8_t packet_id[8],
            const uint8_t ephemeral[32], uint8_t content_type, uint32_t time, const uint8_t* content, size_t n,
            uint8_t* out, size_t cap) {
  size_t pay = (known ? 0 : 32) + kInnerHeader + n;
  size_t need = (known ? kOverheadK : kOverheadX) + kInnerHeader + n;
  if (cap < need) return 0;
  Sym s(known ? kNameK : kNameX);
  start(s, packet_id, recipient);
  if (known) s.mix_hash(sender.pub().x, 32);  // pre-message -> s
  s.mix_hash(recipient.x, 32);                // pre-message <- s
  // -> e
  uint8_t e_pub[32], dh[32];
  crypto_x25519_public_key(e_pub, ephemeral);
  std::memcpy(out, e_pub, 32);
  s.mix_hash(e_pub, 32);
  size_t o = 32;
  // es
  crypto_x25519(dh, ephemeral, recipient.x);
  s.mix_key(dh);
  if (!known) {  // s
    s.encrypt_and_hash(sender.pub().x, 32, out + o);
    o += 48;
  }
  // ss
  if (!sender.dh(recipient.x, dh)) return 0;
  s.mix_key(dh);
  crypto_wipe(dh, sizeof dh);
  // payload: [ed25519 (X)] | type | time | content
  uint8_t* p = out + o;  // encrypted in place below
  size_t k = 0;
  if (!known) {
    std::memcpy(p, sender.pub().ed, 32);
    k = 32;
  }
  p[k] = content_type;
  p[k + 1] = uint8_t(time >> 24);
  p[k + 2] = uint8_t(time >> 16);
  p[k + 3] = uint8_t(time >> 8);
  p[k + 4] = uint8_t(time);
  std::memmove(p + k + kInnerHeader, content, n);
  s.encrypt_and_hash(p, pay, p);
  return o + pay + 16;
}

namespace {

bool finish(Sym& s, const uint8_t* ct, size_t len, uint8_t* plain, size_t cap, size_t skip, Opened& out) {
  if (len < 16 || cap < len - 16 || len - 16 < skip + kInnerHeader) return false;
  if (!s.decrypt_and_hash(ct, len, plain)) return false;
  const uint8_t* p = plain + skip;
  out.content_type = p[0];
  out.time = uint32_t(p[1]) << 24 | uint32_t(p[2]) << 16 | uint32_t(p[3]) << 8 | p[4];
  out.content = p + kInnerHeader;
  out.len = len - 16 - skip - kInnerHeader;
  return true;
}

}  // namespace

bool open(const Identity& me, const PublicKey* contacts, size_t n_contacts, const uint8_t packet_id[8],
          const uint8_t* body, size_t n, uint8_t* plain, size_t cap, Opened& out) {
  if (n < 32 + 16 + kInnerHeader) return false;
  const uint8_t* e = body;
  uint8_t dh[32];
  if (!me.dh(e, dh)) return false;  // es (the same for K and X)

  // Noise X: the sender's static key is in the message.
  if (n >= kOverheadX + kInnerHeader + 32) {
    Sym s(kNameX);
    start(s, packet_id, me.pub());
    s.mix_hash(me.pub().x, 32);
    s.mix_hash(e, 32);
    s.mix_key(dh);
    uint8_t rs[32];
    if (s.decrypt_and_hash(body + 32, 48, rs)) {
      uint8_t ss[32];
      if (me.dh(rs, ss)) {
        s.mix_key(ss);
        crypto_wipe(ss, sizeof ss);
        if (finish(s, body + 80, n - 80, plain, cap, 32, out)) {
          std::memcpy(out.from.x, rs, 32);
          std::memcpy(out.from.ed, plain, 32);
          out.first_contact = true;
          out.contact = -1;
          crypto_wipe(dh, sizeof dh);
          return true;
        }
      }
    }
  }
  // Noise K against each contact.
  for (size_t i = 0; i < n_contacts; i++) {
    Sym s(kNameK);
    start(s, packet_id, me.pub());
    s.mix_hash(contacts[i].x, 32);
    s.mix_hash(me.pub().x, 32);
    s.mix_hash(e, 32);
    s.mix_key(dh);
    uint8_t ss[32];
    if (!me.dh(contacts[i].x, ss)) continue;
    s.mix_key(ss);
    crypto_wipe(ss, sizeof ss);
    if (finish(s, body + 32, n - 32, plain, cap, 0, out)) {
      out.from = contacts[i];
      out.first_contact = false;
      out.contact = int(i);
      crypto_wipe(dh, sizeof dh);
      return true;
    }
  }
  crypto_wipe(dh, sizeof dh);
  return false;
}

}  // namespace e2e
}  // namespace mesh

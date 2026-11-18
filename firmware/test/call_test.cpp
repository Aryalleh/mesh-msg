// Calls on the board (lib/meshcall) against bytes made by the Go code
// (spec/test-vectors/calls.json: core/call with flynn/noise, core/direct).
#include <fstream>
#include <sstream>
#include <string>

#include "callcrypto.h"
#include "check.h"
#include "identity.h"
#include "json.h"

namespace mc = mesh::call;
using check::Bytes;

namespace {

json::Value g_v;

Bytes hexv(const char* k) { return check::unhex(g_v[k].s()); }

void crypto_vectors() {
  Bytes key = hexv("path_key");
  uint8_t out[32];
  mc::hkdf_sha256(key.data(), key.size(), "mava direct probe", out);
  CHECK(Bytes(out, out + 32) == hexv("probe_mac_key"));
  mc::hkdf_sha256(key.data(), key.size(), "mava direct data a", out);
  CHECK(Bytes(out, out + 32) == hexv("data_key_a"));
  mc::hkdf_sha256(key.data(), key.size(), "mava direct data b", out);
  CHECK(Bytes(out, out + 32) == hexv("data_key_b"));

  // The direct link's datagram 5: magic | seq(8 BE) | AEAD(nonce 0000 | seq BE, ad = header).
  Bytes d = hexv("link_datagram5");
  Bytes ka = hexv("data_key_a");
  uint8_t nonce[12] = {};
  nonce[11] = 5;
  std::string msg = "datagram from the controlling side";
  std::vector<uint8_t> sealed(msg.size() + mc::kTag);
  mc::aead_seal(ka.data(), nonce, d.data(), 12, reinterpret_cast<const uint8_t*>(msg.data()), msg.size(), sealed.data());
  CHECK(Bytes(d.begin() + 12, d.end()) == Bytes(sealed.begin(), sealed.end()));
  std::vector<uint8_t> plain(msg.size());
  CHECK(mc::aead_open(ka.data(), nonce, d.data(), 12, d.data() + 12, d.size() - 12, plain.data()));
  CHECK(std::string(plain.begin(), plain.end()) == msg);
  d[20] ^= 1;
  CHECK(!mc::aead_open(ka.data(), nonce, d.data(), 12, d.data() + 12, d.size() - 12, plain.data()));
}

void noise_vectors() {
  Bytes sa = hexv("seed_a"), sb = hexv("seed_b"), id = hexv("call_id"), ea = hexv("eph_a"), eb = hexv("eph_b");
  mesh::Identity a = mesh::Identity::from_seed(sa.data());
  mesh::Identity b = mesh::Identity::from_seed(sb.data());
  Bytes prologue = {'m', 'a', 'v', 'a', ' ', 'c', 'a', 'l', 'l', ' ', 'v', '1'};
  prologue.insert(prologue.end(), id.begin(), id.end());
  mc::NoiseKK ia(true, a, b.pub().x, prologue.data(), prologue.size(), ea.data());
  mc::NoiseKK rb(false, b, a.pub().x, prologue.data(), prologue.size(), eb.data());
  uint8_t m1[mc::NoiseKK::kMsgSize], m2[mc::NoiseKK::kMsgSize];
  CHECK(rb.write(m2) == 0);  // out of turn
  CHECK(ia.write(m1) == sizeof m1);
  CHECK(Bytes(m1, m1 + sizeof m1) == hexv("noise_msg1"));
  CHECK(rb.read(m1, sizeof m1));
  CHECK(rb.write(m2) == sizeof m2 && rb.done());
  CHECK(Bytes(m2, m2 + sizeof m2) == hexv("noise_msg2"));
  CHECK(ia.read(m2, sizeof m2) && ia.done());
  CHECK(Bytes(ia.handshake_hash(), ia.handshake_hash() + 32) == hexv("handshake_hash"));
  CHECK(Bytes(rb.handshake_hash(), rb.handshake_hash() + 32) == hexv("handshake_hash"));
  char code[8];
  mc::call_code(ia.handshake_hash(), code);
  CHECK(std::string(code) == g_v["code"].s());

  // Media frames: 3 | stream | seq(4) | ts(4) | flags | sealed, nonce = stream << 32 | seq, ad = header.
  Bytes fa = hexv("frame_a");
  std::string pa = "opus frame from A";
  std::vector<uint8_t> out(pa.size() + mc::kTag);
  ia.encrypt(uint64_t(0) << 32 | 7, fa.data() + 1, 10, reinterpret_cast<const uint8_t*>(pa.data()), pa.size(), out.data());
  CHECK(Bytes(fa.begin() + 11, fa.end()) == Bytes(out.begin(), out.end()));
  std::vector<uint8_t> got(pa.size());
  CHECK(rb.decrypt(uint64_t(0) << 32 | 7, fa.data() + 1, 10, fa.data() + 11, fa.size() - 11, got.data()));
  CHECK(std::string(got.begin(), got.end()) == pa);
  Bytes fb = hexv("frame_b");
  std::string pb = "video from B";
  std::vector<uint8_t> gb(pb.size());
  CHECK(fb[1] == 1 && ia.decrypt(uint64_t(1) << 32 | 3, fb.data() + 1, 10, fb.data() + 11, fb.size() - 11, gb.data()));
  CHECK(std::string(gb.begin(), gb.end()) == pb);
  // A tampered message 1 does not authenticate.
  Bytes bad(m1, m1 + sizeof m1);
  bad[40] ^= 1;
  mc::NoiseKK r2(false, b, a.pub().x, prologue.data(), prologue.size(), eb.data());
  CHECK(!r2.read(bad.data(), bad.size()));
}

}  // namespace

void run_call(const std::string& dir) {
  std::ifstream f(dir + "/calls.json", std::ios::binary);
  if (!f) {
    CHECK(false && "spec/test-vectors/calls.json missing");
    return;
  }
  std::stringstream ss;
  ss << f.rdbuf();
  g_v = json::parse(ss.str());
  crypto_vectors();
  noise_vectors();
}

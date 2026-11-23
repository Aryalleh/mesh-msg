// Hamyar presence tokens, advert and node report (lib/meshpresence), pinned to
// spec/test-vectors/presence.json (made by the Hamyar server's own code).
#include <cstring>
#include <string>

#include "check.h"
#include "presence.h"

using namespace mesh::presence;

namespace {
std::string hex(const uint8_t* p, size_t n) {
  static const char k[] = "0123456789abcdef";
  std::string s;
  for (size_t i = 0; i < n; i++) {
    s += k[p[i] >> 4];
    s += k[p[i] & 15];
  }
  return s;
}
}  // namespace

void run_presence() {
  uint8_t key[32];
  CHECK(key_from_hex("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", key));
  CHECK(!key_from_hex("0102", key));
  CHECK(!key_from_hex("zz02030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", key));
  CHECK(key_from_hex("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", key));

  uint8_t t[kTokenLen];
  beacon_token(key, 15000000, t);
  CHECK(hex(t, kTokenLen) == "1d50077d00f91f27");
  phone_token(key, 15000000, t);
  CHECK(hex(t, kTokenLen) == "604335470604075b");
  CHECK(window_of(1800000000ull) == 15000000u);

  // advert round trip
  uint8_t adv[kAdvLen];
  build_adv('V', 0x01020304u, t, adv);
  CHECK(adv[0] == 'M' && adv[1] == 'V' && adv[2] == kVersion && adv[4] == 1 && adv[7] == 4);
  Adv a;
  CHECK(parse_adv(adv, kAdvLen, &a));
  CHECK(a.kind == 'V' && a.id == 0x01020304u && memcmp(a.token, t, kTokenLen) == 0);
  CHECK(!parse_adv(adv, kAdvLen - 1, &a));
  adv[1] = 'X';
  CHECK(!parse_adv(adv, kAdvLen, &a));

  // collector: distinct tokens, strongest rssi, window bound, report body + signature
  Collector c(15000000);
  uint8_t p1[kTokenLen] = {0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77};
  CHECK(c.add(15000000, p1, -70));
  CHECK(c.add(15000000, p1, -61));  // same phone, stronger
  CHECK(c.add(15000000, p1, -90));  // weaker: kept -61
  CHECK(!c.add(15000001, p1, -50)); // other window
  CHECK(c.size() == 1);
  char body[512];
  size_t n = c.report_body(body, sizeof body);
  CHECK(std::string(body, n) == "{\"w\":15000000,\"heard\":[{\"t\":\"0011223344556677\",\"r\":-61}]}");
  char sig[65];
  report_sig(key, body, n, sig);
  CHECK(std::string(sig) == "926c217400d84deea2d1450ec8e37aab41c3e9a6fb6e98f52552aa98c6c0c31f");
  CHECK(c.report_body(body, 20) == 0);  // too small

  // full collector drops new phones
  c.reset(7);
  for (size_t i = 0; i < kMaxHeard; i++) {
    uint8_t k[kTokenLen] = {uint8_t(i), uint8_t(i >> 8), 1, 2, 3, 4, 5, 6};
    CHECK(c.add(7, k, -80));
  }
  uint8_t extra[kTokenLen] = {0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0};
  CHECK(!c.add(7, extra, -40));
  CHECK(c.size() == kMaxHeard);
  static char big[kMaxHeard * 40 + 64];
  CHECK(c.report_body(big, sizeof big) > 0);
}

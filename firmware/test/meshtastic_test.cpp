// Meshtastic backbone adapter: frame layout, channel hash, encryption round
// trip, foreign traffic ignored, and relaying under the original identity.
#include <algorithm>
#include <cstring>

#include "check.h"
#include "mac.h"
#include "meshtastic.h"
#include "packet.h"

using check::Bytes;
namespace mt = mesh::meshtastic;


namespace {

Bytes mava(uint8_t id, size_t body = 40) {
  Bytes b(body, 0x5a);
  mesh::Packet p;
  p.type = mesh::Type::Data;
  p.max_hops = 3;
  for (int i = 0; i < 8; i++) p.id[i] = uint8_t(id + i);
  p.body = b.data();
  p.body_len = b.size();
  Bytes out(p.size());
  size_t n = 0;
  mesh::encode(p, out.data(), out.size(), &n);
  return out;
}

uint32_t le32(const uint8_t* p) { return uint32_t(p[0]) | uint32_t(p[1]) << 8 | uint32_t(p[2]) << 16 | uint32_t(p[3]) << 24; }

}  // namespace

void run_meshtastic() {
  mt::Channel ch;
  mt::Backend a, b, c;
  a.begin(0x11111111, 1, ch);
  b.begin(0x22222222, 2, ch);
  c.begin(0x33333333, 3, ch);

  // LongFast + default key: the well-known channel hash 8.
  CHECK(a.channel_hash() == 8);

  // Round trip; the header is Meshtastic's.
  Bytes m = mava(1);
  uint8_t f[mt::kRadioMax], got[mt::kRadioMax];
  size_t fn = a.encode(m.data(), m.size(), f, sizeof f);
  CHECK(fn > mt::kHeaderSize && fn <= mt::kRadioMax);
  CHECK(le32(f) == mt::kBroadcast && le32(f + 4) == 0x11111111 && le32(f + 8) != 0);
  CHECK((f[12] & 7) == 3 && (f[12] >> 5) == 3 && f[13] == 8 && f[15] == 0x11);
  // Encrypted: the Mava bytes do not appear in clear.
  CHECK(std::search(f, f + fn, m.begin() + 10, m.begin() + 30) == f + fn);
  mt::RxMeta meta;
  size_t gn = b.decode(f, fn, got, sizeof got, &meta);
  CHECK(gn == m.size() && Bytes(got, got + gn) == m);
  CHECK(meta.from == 0x11111111 && meta.hop_limit == 3);

  // Our own packet relayed back by someone: passed on (implicit ACK), and
  // it stays ours (no relaying under our own identity with fewer hops).
  CHECK(a.decode(f, fn, got, sizeof got) == m.size() && a.stats().rx_own == 1);

  // Retransmission of our own packet keeps the Meshtastic id.
  uint8_t f2[mt::kRadioMax];
  size_t fn2 = a.encode(m.data(), m.size(), f2, sizeof f2);
  CHECK(fn2 == fn && le32(f2 + 8) == le32(f + 8) && Bytes(f, f + fn) == Bytes(f2, f2 + fn2));

  // B relays it: original from / id, one hop fewer, B as relay node.
  CHECK(mesh::inc_hop(m.data(), m.size()));  // the Mava router bumped its hop count
  uint8_t r[mt::kRadioMax];
  size_t rn = b.encode(m.data(), m.size(), r, sizeof r);
  CHECK(rn == fn && le32(r + 4) == 0x11111111 && le32(r + 8) == le32(f + 8));
  CHECK((r[12] & 7) == 2 && (r[12] >> 5) == 3 && r[15] == 0x22);
  CHECK(c.decode(r, rn, got, sizeof got, &meta) == m.size() && meta.hop_limit == 2 && meta.relay_node == 0x22);

  // Hop limit used up: relaying stops.
  f[12] = uint8_t(3 << 5);  // hop_limit 0
  Bytes m3 = mava(9);
  size_t n3 = a.encode(m3.data(), m3.size(), f, sizeof f);
  f[12] = uint8_t(3 << 5);
  // re-encrypt is not needed: the header is not covered by encryption.
  CHECK(c.decode(f, n3, got, sizeof got) == m3.size());
  CHECK(c.encode(m3.data(), m3.size(), r, sizeof r) == 0 && c.stats().tx_hops_exhausted == 1);

  // Another channel: ignored. Another key on the same hash: not Mava.
  mt::Channel other;
  std::strcpy(other.name, "Mava");
  other.psk_len = 16;
  for (int i = 0; i < 16; i++) other.psk[i] = uint8_t(i * 7 + 1);
  mt::Backend d;
  d.begin(0x44444444, 4, other);
  CHECK(d.channel_hash() != 8);
  CHECK(d.decode(r, rn, got, sizeof got) == 0 && d.stats().rx_other_channel == 1);
  mt::Backend e;
  e.begin(0x55555555, 5, other);
  size_t en = e.encode(m.data(), m.size(), f, sizeof f);
  CHECK(d.decode(f, en, got, sizeof got) == m.size());

  // AES-256 key and an unencrypted channel both round-trip.
  mt::Channel k256 = other;
  k256.psk_len = 32;
  mt::Backend g, h;
  g.begin(0x66666666, 6, k256);
  h.begin(0x77777777, 7, k256);
  en = g.encode(m.data(), m.size(), f, sizeof f);
  CHECK(h.decode(f, en, got, sizeof got) == m.size());
  mt::Channel clear;
  clear.psk[0] = 0;
  g.set_channel(clear);
  h.set_channel(clear);
  en = g.encode(m.data(), m.size(), f, sizeof f);
  CHECK(std::search(f, f + en, m.begin(), m.end()) != f + en);
  CHECK(h.decode(f, en, got, sizeof got) == m.size());

  // Size limits: 233 bytes fits a 255-byte frame, 234 does not encode.
  Bytes big = mava(20, mesh::kLoRaMTU - mesh::kHeaderSize);
  CHECK(big.size() == mt::kMaxPayload);
  CHECK(a.encode(big.data(), big.size(), f, sizeof f) == mt::kRadioMax);
  big.push_back(0);
  CHECK(a.encode(big.data(), big.size(), f, sizeof f) == 0);

  // Invalid channels are refused; garbage does not crash.
  mt::Channel bad;
  bad.psk_len = 5;
  CHECK(!bad.valid() && !a.set_channel(bad));
  for (int i = 0; i < 2000; i++) {
    uint8_t junk[mt::kRadioMax];
    for (auto& x : junk) x = uint8_t(i * 131 + (&x - junk) * 17);
    junk[13] = 8;
    b.decode(junk, size_t(17 + i % 238), got, sizeof got);
  }
  CHECK(mt::Backend::node_id_for(m.data()) != 0);

  // The MAC times frames with LongFast: a full frame is about 2.16 s on air.
  CHECK(mesh::mac::airtime_us(mesh::mac::kMeshtasticLongFast, mt::kRadioMax) == 2156544);
}

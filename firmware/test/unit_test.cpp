// Edge cases and robustness for the protocol library (run under ASan/UBSan).
#include <random>

#include "announce.h"
#include "check.h"
#include "identity.h"
#include "packet.h"
#include "sha256.h"

using check::Bytes;
using check::hex;

namespace {

mesh::Packet sample(const Bytes& body) {
  mesh::Packet p;
  p.type = mesh::Type::Data;
  p.max_hops = 8;
  for (int i = 0; i < 8; i++) p.id[i] = uint8_t(i + 1);
  for (int i = 0; i < 16; i++) p.dest[i] = uint8_t(0xa0 + i);
  p.body = body.data();
  p.body_len = body.size();
  return p;
}

Bytes enc(const mesh::Packet& p) {
  Bytes out(p.size());
  size_t n = 0;
  if (mesh::encode(p, out.data(), out.size(), &n) != mesh::Err::Ok) return {};
  out.resize(n);
  return out;
}

void sha256() {
  uint8_t out[32];
  mesh::Sha256::hash(nullptr, 0, out);
  CHECK(hex(out, 32) == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
  const uint8_t abc[] = {'a', 'b', 'c'};
  mesh::Sha256::hash(abc, 3, out);
  CHECK(hex(out, 32) == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
  // Multi-block, fed in uneven pieces.
  const char* m = "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
  mesh::Sha256 s;
  s.update(reinterpret_cast<const uint8_t*>(m), 5);
  s.update(reinterpret_cast<const uint8_t*>(m) + 5, std::strlen(m) - 5);
  s.finish(out);
  CHECK(hex(out, 32) == "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
}

void decode_errors() {
  const Bytes body = {1, 2, 3};
  Bytes raw = enc(sample(body));
  mesh::Packet p;
  CHECK(mesh::decode(raw.data(), mesh::kHeaderSize - 1, p) == mesh::Err::Short);
  CHECK(mesh::decode(raw.data(), mesh::kHeaderSize, p) == mesh::Err::Ok && p.body_len == 0);

  Bytes v2 = raw;
  v2[0] = uint8_t((v2[0] & 0x3f) | 2 << 6);
  CHECK(mesh::decode(v2.data(), v2.size(), p) == mesh::Err::Version);

  Bytes frag = raw;
  frag[0] |= mesh::kCtxFragmented;
  frag.resize(mesh::kHeaderSize);  // fragment byte missing
  CHECK(mesh::decode(frag.data(), frag.size(), p) == mesh::Err::Short);
  frag.push_back(0x21);  // index 2 of count 2
  CHECK(mesh::decode(frag.data(), frag.size(), p) == mesh::Err::Frag);
  frag.back() = 0x12;  // index 1 of count 3
  CHECK(mesh::decode(frag.data(), frag.size(), p) == mesh::Err::Ok && p.frag_index == 1 && p.frag_count == 3);

  Bytes huge(mesh::kMaxSize + 1, 0);
  huge[0] = 0x40;
  CHECK(mesh::decode(huge.data(), huge.size(), p) == mesh::Err::TooLarge);
}

void encode_errors() {
  const Bytes body = {9};
  mesh::Packet p = sample(body);
  uint8_t out[64];
  size_t n = 0;
  p.hop_count = 16;
  CHECK(mesh::encode(p, out, sizeof out, &n) == mesh::Err::Range);
  p.hop_count = 0;
  CHECK(mesh::encode(p, out, 10, &n) == mesh::Err::Short);
  p.ctx = mesh::kCtxFragmented;
  p.frag_count = 0;
  CHECK(mesh::encode(p, out, sizeof out, &n) == mesh::Err::Frag);
  p.frag_count = 17;
  CHECK(mesh::encode(p, out, sizeof out, &n) == mesh::Err::Frag);
  p.frag_count = 2;
  p.frag_index = 2;
  CHECK(mesh::encode(p, out, sizeof out, &n) == mesh::Err::Frag);
  p.frag_index = 1;
  CHECK(mesh::encode(p, out, sizeof out, &n) == mesh::Err::Ok && n == mesh::kHeaderSize + 2);
}

void hops_and_frames() {
  uint8_t shortbuf[4] = {0x40, 0x08, 0, 0};
  CHECK(!mesh::inc_hop(shortbuf, sizeof shortbuf));

  uint8_t frame[mesh::kLoRaRadioMax + 1] = {};
  uint8_t pkt[mesh::kLoRaMTU + 1] = {};
  size_t n = 0;
  CHECK(mesh::encode_link_frame(1, pkt, mesh::kLoRaMTU, frame, sizeof frame, &n) == mesh::Err::Ok &&
        n == mesh::kLoRaRadioMax);
  CHECK(mesh::encode_link_frame(1, pkt, mesh::kLoRaMTU + 1, frame, sizeof frame, &n) == mesh::Err::TooLarge);
  uint8_t sender;
  const uint8_t* raw;
  size_t raw_len;
  CHECK(mesh::decode_link_frame(frame, mesh::kHeaderSize, &sender, &raw, &raw_len) == mesh::Err::Short);
  CHECK(mesh::decode_link_frame(frame, sizeof frame, &sender, &raw, &raw_len) == mesh::Err::TooLarge);
}

void fragmentation() {
  Bytes body(mesh::kFragBodySize * mesh::kMaxFragments);
  for (size_t i = 0; i < body.size(); i++) body[i] = uint8_t(i * 7);
  mesh::Packet p = sample(body);
  CHECK(mesh::fragment_count(p, mesh::kLoRaMTU) == mesh::kMaxFragments);
  body.push_back(0);
  p = sample(body);
  CHECK(mesh::fragment_count(p, mesh::kLoRaMTU) == 0);
  mesh::Packet f;
  CHECK(mesh::fragment(p, mesh::kLoRaMTU, 0, f) == mesh::Err::TooLarge);
  body.pop_back();
  p = sample(body);
  CHECK(mesh::fragment_count(p, mesh::kHeaderSize + 1) == 0);
  CHECK(mesh::fragment(p, mesh::kLoRaMTU, 16, f) == mesh::Err::Range);

  mesh::Packet frags[mesh::kMaxFragments];
  for (size_t i = 0; i < mesh::kMaxFragments; i++) CHECK(mesh::fragment(p, mesh::kLoRaMTU, i, frags[i]) == mesh::Err::Ok);
  CHECK(mesh::fragment_count(frags[0], mesh::kLoRaMTU) == 0);  // already fragmented

  Bytes out(body.size());
  mesh::Packet whole;
  CHECK(mesh::reassemble(frags, mesh::kMaxFragments, out.data(), out.size(), whole) == mesh::Err::Ok);
  CHECK(whole.body_len == body.size() && std::memcmp(out.data(), body.data(), body.size()) == 0);
  CHECK(!whole.fragmented());
  CHECK(mesh::reassemble(frags, mesh::kMaxFragments, out.data(), out.size() - 1, whole) == mesh::Err::Short);
  CHECK(mesh::reassemble(frags, mesh::kMaxFragments - 1, out.data(), out.size(), whole) == mesh::Err::Frag);
  mesh::Packet dup[2] = {frags[0], frags[0]};
  dup[0].frag_count = dup[1].frag_count = 2;
  CHECK(mesh::reassemble(dup, 2, out.data(), out.size(), whole) == mesh::Err::Frag);
  mesh::Packet other[mesh::kMaxFragments];
  std::copy(frags, frags + mesh::kMaxFragments, other);
  other[3].id[0] ^= 1;
  CHECK(mesh::reassemble(other, mesh::kMaxFragments, out.data(), out.size(), whole) == mesh::Err::Frag);

  // A packet that fits is its own single fragment.
  const Bytes small = {1, 2};
  p = sample(small);
  CHECK(mesh::fragment_count(p, mesh::kLoRaMTU) == 1);
  CHECK(mesh::fragment(p, mesh::kLoRaMTU, 0, f) == mesh::Err::Ok && !f.fragmented() && f.body == p.body);
}

void signed_packets() {
  uint8_t seed[64];
  for (int i = 0; i < 64; i++) seed[i] = uint8_t(200 + i);
  const mesh::Identity id = mesh::Identity::from_seed(seed);
  const uint8_t pid[8] = {1, 2, 3, 4, 5, 6, 7, 8};

  mesh::AppData app;
  std::memset(app.name, 'n', 40);  // senders truncate to 32 bytes
  app.flags = mesh::kFlagNode;
  uint8_t body[mesh::kMaxAnnounceBody];
  mesh::Packet p;
  CHECK(mesh::build_announce(id, 1234, app, 4, pid, body, 10, p) == mesh::AnnounceErr::TooSmall);
  CHECK(mesh::build_announce(id, 1234, app, 4, pid, body, sizeof body, p) == mesh::AnnounceErr::Ok);
  mesh::Announce a;
  CHECK(mesh::parse_announce(p, a) == mesh::AnnounceErr::Ok);
  CHECK(std::strlen(a.app.name) == 32 && a.app.flags == mesh::kFlagNode && a.time == 1234);

  mesh::Packet wrong = p;
  wrong.type = mesh::Type::Data;
  CHECK(mesh::parse_announce(wrong, a) == mesh::AnnounceErr::NotAnnounce);
  wrong = p;
  wrong.body_len--;
  CHECK(mesh::parse_announce(wrong, a) == mesh::AnnounceErr::Length);
  wrong = p;
  wrong.dest[0] ^= 1;
  CHECK(mesh::parse_announce(wrong, a) == mesh::AnnounceErr::DestMismatch);
  Bytes tampered(body, body + p.body_len);
  tampered[66] ^= 1;  // timestamp
  wrong = p;
  wrong.body = tampered.data();
  CHECK(mesh::parse_announce(wrong, a) == mesh::AnnounceErr::BadSignature);

  // Validly signed but malformed app data (TLV length runs past the end).
  Bytes bad(body, body + mesh::kAnnounceFixed);
  bad[68] = 2;
  bad.push_back(mesh::kTagName);
  bad.push_back(5);
  uint8_t msg[16 + mesh::kAnnounceFixed + 2];
  std::memcpy(msg, p.dest, 16);
  std::memcpy(msg + 16, bad.data(), bad.size());
  uint8_t sig[64];
  CHECK(id.sign("MESHMSG-ANNC", msg, sizeof msg, sig));
  bad.insert(bad.end(), sig, sig + 64);
  wrong = p;
  wrong.body = bad.data();
  wrong.body_len = bad.size();
  CHECK(mesh::parse_announce(wrong, a) == mesh::AnnounceErr::BadAppData);

  uint8_t rbody[mesh::kReceiptBody];
  mesh::Packet r;
  CHECK(mesh::build_receipt(id, pid, 8, rbody, sizeof rbody, r) == mesh::AnnounceErr::Ok);
  uint8_t acked[8];
  CHECK(mesh::verify_receipt(r, id.pub(), acked) == mesh::AnnounceErr::Ok && std::memcmp(acked, pid, 8) == 0);
  mesh::Packet rw = r;
  rw.id[0] ^= 1;
  CHECK(mesh::receipt_acked(rw, acked) == mesh::AnnounceErr::IdMismatch);
  rw = r;
  rw.type = mesh::Type::Data;
  CHECK(mesh::receipt_acked(rw, acked) == mesh::AnnounceErr::NotReceipt);
  Bytes rt(rbody, rbody + sizeof rbody);
  rt[20] ^= 1;
  rw = r;
  rw.body = rt.data();
  CHECK(mesh::verify_receipt(rw, id.pub(), acked) == mesh::AnnounceErr::BadSignature);

  uint8_t big[mesh::kMaxSignedMsg];
  CHECK(!id.sign("X", big, sizeof big, sig));
  CHECK(!id.pub().verify("X", big, sizeof big, sig));
}

// Random input must never crash, and anything that decodes must re-encode to
// the same bytes.
void fuzz() {
  std::mt19937 rng(42);
  int decoded = 0;
  for (int iter = 0; iter < 200000; iter++) {
    const size_t n = rng() % 300;
    Bytes b(n);
    for (auto& x : b) x = uint8_t(rng());
    if (n > 0 && (rng() & 1)) b[0] = uint8_t((b[0] & 0x3f) | 0x40);  // valid version half the time
    mesh::Packet p;
    if (mesh::decode(b.data(), b.size(), p) != mesh::Err::Ok) continue;
    decoded++;
    CHECKC(enc(p) == b, "round trip " + hex(b));
    Bytes copy = b;
    mesh::inc_hop(copy.data(), copy.size());
    mesh::Announce a;
    (void)mesh::parse_announce(p, a);
    uint8_t acked[8];
    (void)mesh::receipt_acked(p, acked);
    mesh::Packet f;
    for (size_t i = 0; i < mesh::fragment_count(p, 64); i++) CHECK(mesh::fragment(p, 64, i, f) == mesh::Err::Ok);
  }
  CHECK(decoded > 10000);
}

}  // namespace

void run_units() {
  sha256();
  decode_errors();
  encode_errors();
  hops_and_frames();
  fragmentation();
  signed_packets();
  fuzz();
}

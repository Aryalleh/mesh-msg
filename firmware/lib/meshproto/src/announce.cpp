#include "announce.h"

#include <cstring>

#include "sha256.h"

namespace mesh {

namespace {

constexpr const char* kDomainAnnounce = "MESHMSG-ANNC";
constexpr const char* kDomainReceipt = "MESHMSG-RCPT";
constexpr char kReceiptIdTag[] = "MESHMSG-RCPT-ID";

bool decode_app_data(const uint8_t* b, size_t n, AppData& a) {
  a = AppData{};
  while (n > 0) {
    if (n < 2 || n < 2 + size_t(b[1])) return false;
    const uint8_t tag = b[0];
    const size_t len = b[1];
    const uint8_t* val = b + 2;
    if (tag == kTagName) {
      const size_t m = len < sizeof a.name - 1 ? len : sizeof a.name - 1;
      std::memcpy(a.name, val, m);
      a.name[m] = 0;
    } else if (tag == kTagFlags && len > 0) {
      a.flags = val[0];
    }
    b += 2 + len;
    n -= 2 + len;
  }
  return true;
}

size_t encode_app_data(const AppData& a, uint8_t out[kMaxAppData]) {
  size_t n = 0;
  if (a.flags) {
    out[n++] = kTagFlags;
    out[n++] = 1;
    out[n++] = a.flags;
  }
  size_t nl = strnlen(a.name, sizeof a.name);
  if (nl > 32) nl = 32;
  if (nl) {
    out[n++] = kTagName;
    out[n++] = uint8_t(nl);
    std::memcpy(out + n, a.name, nl);
    n += nl;
  }
  return n;
}

}  // namespace

AnnounceErr parse_announce(const Packet& p, Announce& out) {
  if (p.type != Type::Announce) return AnnounceErr::NotAnnounce;
  const uint8_t* b = p.body;
  if (p.body_len < kAnnounceFixed + kSigSize) return AnnounceErr::Length;
  const size_t ad_len = b[68];
  if (ad_len > kMaxAppData || p.body_len != kAnnounceFixed + ad_len + kSigSize) return AnnounceErr::Length;
  Announce a;
  a.pub = PublicKey::from_bytes(b);
  uint8_t h[16];
  a.pub.hash(h);
  if (std::memcmp(h, p.dest, 16) != 0) return AnnounceErr::DestMismatch;
  // Signed message: dest || keys || time || ad_len || app data.
  uint8_t msg[16 + kAnnounceFixed + kMaxAppData];
  std::memcpy(msg, p.dest, 16);
  std::memcpy(msg + 16, b, kAnnounceFixed + ad_len);
  if (!a.pub.verify(kDomainAnnounce, msg, 16 + kAnnounceFixed + ad_len, b + kAnnounceFixed + ad_len))
    return AnnounceErr::BadSignature;
  if (!decode_app_data(b + kAnnounceFixed, ad_len, a.app)) return AnnounceErr::BadAppData;
  a.time = uint32_t(b[64]) << 24 | uint32_t(b[65]) << 16 | uint32_t(b[66]) << 8 | b[67];
  out = a;
  return AnnounceErr::Ok;
}

AnnounceErr build_announce(const Identity& id, uint32_t now, const AppData& app, uint8_t max_hops,
                           const uint8_t packet_id[8], uint8_t* body_buf, size_t cap, Packet& out) {
  uint8_t ad[kMaxAppData];
  const size_t ad_len = encode_app_data(app, ad);
  const size_t body_len = kAnnounceFixed + ad_len + kSigSize;
  if (cap < body_len) return AnnounceErr::TooSmall;
  std::memcpy(body_buf, id.pub().x, 32);
  std::memcpy(body_buf + 32, id.pub().ed, 32);
  body_buf[64] = uint8_t(now >> 24);
  body_buf[65] = uint8_t(now >> 16);
  body_buf[66] = uint8_t(now >> 8);
  body_buf[67] = uint8_t(now);
  body_buf[68] = uint8_t(ad_len);
  std::memcpy(body_buf + kAnnounceFixed, ad, ad_len);

  Packet p;
  p.type = Type::Announce;
  p.max_hops = max_hops;
  std::memcpy(p.id, packet_id, 8);
  id.pub().hash(p.dest);
  uint8_t msg[16 + kAnnounceFixed + kMaxAppData];
  std::memcpy(msg, p.dest, 16);
  std::memcpy(msg + 16, body_buf, kAnnounceFixed + ad_len);
  id.sign(kDomainAnnounce, msg, 16 + kAnnounceFixed + ad_len, body_buf + kAnnounceFixed + ad_len);
  p.body = body_buf;
  p.body_len = body_len;
  out = p;
  return AnnounceErr::Ok;
}

void receipt_id(const uint8_t acked[8], uint8_t out[8]) {
  Sha256 s;
  s.update(reinterpret_cast<const uint8_t*>(kReceiptIdTag), sizeof kReceiptIdTag - 1);
  s.update(acked, 8);
  uint8_t sum[32];
  s.finish(sum);
  std::memcpy(out, sum, 8);
}

AnnounceErr receipt_acked(const Packet& p, uint8_t acked[8]) {
  if (p.type != Type::Receipt) return AnnounceErr::NotReceipt;
  if (p.body_len != kReceiptBody) return AnnounceErr::Length;
  uint8_t want[8];
  receipt_id(p.body, want);
  std::memcpy(acked, p.body, 8);
  if (std::memcmp(want, p.id, 8) != 0) return AnnounceErr::IdMismatch;
  return AnnounceErr::Ok;
}

AnnounceErr verify_receipt(const Packet& p, const PublicKey& signer, uint8_t acked[8]) {
  const AnnounceErr e = receipt_acked(p, acked);
  if (e != AnnounceErr::Ok) return e;
  uint8_t h[16];
  signer.hash(h);
  if (std::memcmp(h, p.dest, 16) != 0) return AnnounceErr::DestMismatch;
  uint8_t msg[24];
  std::memcpy(msg, acked, 8);
  std::memcpy(msg + 8, p.dest, 16);
  if (!signer.verify(kDomainReceipt, msg, sizeof msg, p.body + 8)) return AnnounceErr::BadSignature;
  return AnnounceErr::Ok;
}

AnnounceErr build_receipt(const Identity& id, const uint8_t acked[8], uint8_t max_hops, uint8_t* body_buf,
                          size_t cap, Packet& out) {
  if (cap < kReceiptBody) return AnnounceErr::TooSmall;
  Packet p;
  p.type = Type::Receipt;
  p.max_hops = max_hops;
  receipt_id(acked, p.id);
  id.pub().hash(p.dest);
  uint8_t msg[24];
  std::memcpy(msg, acked, 8);
  std::memcpy(msg + 8, p.dest, 16);
  std::memcpy(body_buf, acked, 8);
  id.sign(kDomainReceipt, msg, sizeof msg, body_buf + 8);
  p.body = body_buf;
  p.body_len = kReceiptBody;
  out = p;
  return AnnounceErr::Ok;
}

}  // namespace mesh

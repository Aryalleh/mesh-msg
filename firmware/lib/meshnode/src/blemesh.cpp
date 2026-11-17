#include "blemesh.h"

#include <cstring>

namespace mesh {
namespace blemesh {

void Peer::reset() {
  hello_ = false;
  hdr_n_ = 0;
  need_ = got_ = 0;
  skip_ = false;
  dec_ = host::Decoder();
  q_off_ = q_len_ = 0;
}

void Peer::command(node::Node& node, size_t port, uint8_t op, route::Millis now) {
  uint8_t p[2] = {op, 0};
  uint8_t f[16];
  size_t e = host::encode(host::kKindCommand, p, sizeof p, f, sizeof f);
  node.host_rx(port, f, e, now);
}

void Peer::packet(node::Node& node, size_t port, route::Millis now) {
  uint8_t f[host::kMaxEncoded];
  size_t e = host::encode(host::kKindPacket, in_, need_, f, sizeof f);
  if (e) node.host_rx(port, f, e, now);
}

void Peer::start(node::Node& node, size_t port, route::Millis now) {
  hello_ = true;
  // A new peer in this seat: send it everything (RESYNC), then the backlog
  // streams as for any host that says HELLO.
  command(node, port, host::kOpResync, now);
  command(node, port, host::kOpHello, now);
}

bool Peer::rx(node::Node& node, size_t port, const uint8_t* b, size_t n, route::Millis now) {
  if (!hello_) {
    if (n != 1 + kSessionBytes || b[0] != kHelloMagic) return false;
    start(node, port, now);
    return true;
  }
  for (size_t i = 0; i < n; i++) {
    if (hdr_n_ < kFrameHeader) {
      hdr_[hdr_n_++] = b[i];
      if (hdr_n_ < kFrameHeader) continue;
      need_ = uint32_t(hdr_[0]) << 16 | uint32_t(hdr_[1]) << 8 | hdr_[2];
      if (need_ == 0 || need_ > kMaxFrame) return false;
      got_ = 0;
      skip_ = need_ > kLoRaMTU;  // a photo or voice frame: not for LoRa
      continue;
    }
    if (!skip_) in_[got_] = b[i];
    if (++got_ == need_) {
      if (!skip_) {
        packet(node, port, now);
        in_n_++;
      } else {
        skipped_n_++;
      }
      hdr_n_ = 0;
    }
  }
  return true;
}

size_t Peer::tx(node::Node& node, size_t port, uint8_t* out, size_t cap) {
  if (!hello_) return 0;
  // Pull from the node while a whole packet still fits behind what waits.
  while (q_len_ + 2 * (kFrameHeader + kLoRaMTU) <= sizeof q_) {
    if (q_off_ && q_len_ == 0) q_off_ = 0;
    if (q_off_ + q_len_ + 2 * (kFrameHeader + kLoRaMTU) > sizeof q_) {
      std::memmove(q_, q_ + q_off_, q_len_);
      q_off_ = 0;
    }
    uint8_t raw[256];
    size_t m = node.host_tx(port, raw, sizeof raw);
    if (!m) break;
    size_t off = 0;
    while (off < m) {
      off += dec_.feed(raw + off, m - off);
      if (!dec_.frame() || dec_.kind() != host::kKindPacket) continue;
      size_t len = dec_.payload_len();
      if (len == 0 || len > kLoRaMTU) continue;
      uint8_t* w = q_ + q_off_ + q_len_;
      w[0] = uint8_t(len >> 16);
      w[1] = uint8_t(len >> 8);
      w[2] = uint8_t(len);
      std::memcpy(w + kFrameHeader, dec_.payload(), len);
      q_len_ += kFrameHeader + len;
      out_n_++;
    }
  }
  size_t k = q_len_ < cap ? q_len_ : cap;
  std::memcpy(out, q_ + q_off_, k);
  q_off_ += k;
  q_len_ -= k;
  return k;
}

}  // namespace blemesh
}  // namespace mesh

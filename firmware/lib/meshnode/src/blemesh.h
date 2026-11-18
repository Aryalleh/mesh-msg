// The board as a peer in the phones' Bluetooth mesh (docs/board.md §4,
// android/app/.../ble/BleProto.kt): phones find the board like another phone
// and connect to it (it advertises the lowest session ID, so it is always
// the GATT server). The phone's first write is a hello (0x4D + 4-byte session
// ID); after that both directions carry len(3, big endian) || packet, cut into
// writes / notifications of at most MTU − 3 bytes.
//
// Each connected phone is a host port of the Node (kMeshPort0…): its packets
// go out on LoRa, to the other phones, USB, the browser and the relay, and
// what the node holds for it streams back. Untrusted: packets only.
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

#include "hostlink.h"
#include "node.h"

namespace mesh {
namespace blemesh {

constexpr uint8_t kHelloMagic = 0x4D;
constexpr size_t kSessionBytes = 4;
constexpr size_t kFrameHeader = 3;
constexpr uint32_t kMaxFrame = 64 * 1024;  // the phones' packet.MaxSize

class Peer {
 public:
  void reset();
  bool up() const { return hello_; }
  // Since boot, for the board log: packets taken from the phone (to the node),
  // too large for LoRa and skipped, and packets sent to the phone.
  uint32_t packets_in() const { return in_n_; }
  uint32_t packets_skipped() const { return skipped_n_; }
  uint32_t packets_out() const { return out_n_; }
  // Why rx() last returned false ("" = never), for the board log.
  const char* error() const { return err_; }
  // Times the phone said hello again on this connection (a new session):
  // the caller drops bytes it still had queued for the old stream.
  uint32_t restarts() const { return restarts_; }
  // This side connected (GATT client) and sent the hello itself: the stream
  // starts at once.
  void start(node::Node& node, size_t port, route::Millis now);

  // Bytes a phone wrote. False: not a hello first, or a corrupt stream (the
  // caller disconnects the phone). Packets larger than LoRa (media) are skipped.
  bool rx(node::Node& node, size_t port, const uint8_t* b, size_t n, route::Millis now);

  // Up to cap bytes to notify the phone next (0: nothing waiting).
  size_t tx(node::Node& node, size_t port, uint8_t* out, size_t cap);

 private:
  void packet(node::Node& node, size_t port, route::Millis now);
  void command(node::Node& node, size_t port, uint8_t op, route::Millis now);

  bool hello_ = false;
  // Incoming frame being assembled.
  uint8_t hdr_[kFrameHeader] = {};
  size_t hdr_n_ = 0;
  uint32_t need_ = 0, got_ = 0;
  bool skip_ = false;
  uint8_t in_[kLoRaMTU] = {};
  // Outgoing: the node's host-link stream decoded into len3 || packet bytes.
  host::Decoder dec_;
  uint8_t q_[1024] = {};
  size_t q_off_ = 0, q_len_ = 0;
  uint32_t in_n_ = 0, skipped_n_ = 0, out_n_ = 0;
  char err_[48] = {};
  uint8_t session_[kSessionBytes] = {};
  uint32_t restarts_ = 0;
};

}  // namespace blemesh
}  // namespace mesh

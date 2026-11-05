// The node application (docs/firmware.md §2, §5, §6): wires the router, MAC
// and store to a radio and to a host (phone over BLE or PC over USB, one
// byte stream in core/hostlink format). Pure C++17 and I/O-free, so the
// whole node runs in the host tests; the board layer (src/) only moves
// bytes and calls these methods:
//
//   radio:  radio_rx() on every received frame;
//           loop { a = radio_next(now): Idle → listen until a.wake,
//                  Cad → radio_cad(busy), Transmit → send, radio_tx_done() }
//   host:   host_attach() / host_detach(), host_rx(bytes), host_tx(buf) drains output
//   timer:  tick(now) about once a second
#pragma once

#include <cstddef>
#include <cstdint>

#include "hostlink.h"
#include "identity.h"
#include "mac.h"
#include "router.h"
#include "store.h"

namespace mesh {
namespace node {

using route::Millis;

constexpr uint8_t kFirmwareVersion = 1;
constexpr uint32_t kStoreTTL = 3 * 24 * 3600;  // seconds
constexpr uint8_t kLoRaMaxHops = 3;             // docs/firmware.md §2a
constexpr size_t kHostOut = 4096;               // host output buffer (bytes)
constexpr size_t kNeighbours = 32;
constexpr Millis kNeighbourTTL = 3600ull * 1000;
constexpr size_t kInflight = 32;  // packets in the host output buffer

// What the board provides.
class Platform {
 public:
  virtual void save_config(const host::NodeConfig& c) = 0;
  // Retune the radio (frequency, preset, power) after a config change.
  virtual void apply_radio(const host::NodeConfig& c) = 0;
  // Erase stored packets, config and the node identity (then reboot).
  virtual void wipe() = 0;
  virtual void reboot() = 0;
  virtual uint16_t battery_mv() = 0;  // 0 = unknown / mains

 protected:
  ~Platform() = default;
};

struct Stats {
  uint32_t host_packets_in = 0, host_packets_rejected = 0, host_bad_frames = 0;
  uint32_t host_packets_out = 0, host_events_dropped = 0, announces = 0;
};

class Node {
 public:
  // seed: the node identity (x25519 || ed25519 seeds, from flash; generated
  // on first boot). random: a hardware random value.
  Node(const host::NodeConfig& cfg, const uint8_t seed[kSeedSize], Platform& platform,
       store::Persist* persist, uint32_t random);

  // ---- radio ----
  void radio_rx(const uint8_t* frame, size_t n, int snr_db, Millis now);
  mac::Action radio_next(Millis now);
  void radio_cad(bool busy, Millis now);
  void radio_tx_done(Millis now);

  // ---- host ----
  void host_attach(Millis now);
  void host_detach();
  bool host_attached() const { return attached_; }
  void host_rx(const uint8_t* b, size_t n, Millis now);
  // host_tx copies up to cap pending output bytes; returns how many.
  size_t host_tx(uint8_t* out, size_t cap);
  size_t host_pending() const { return out_len_; }

  void tick(Millis now);
  // A reboot (or wipe) was requested; the board reboots once host output drained.
  bool reboot_requested() const { return reboot_; }

  // Restores a stored record read back from flash at boot.
  bool restore(size_t slot, const store::Record& r, Millis now);

  host::Status status(Millis now) const;
  const host::NodeConfig& config() const { return cfg_; }
  uint8_t short_id() const { return short_id_; }
  const PublicKey& pub() const { return id_.pub(); }
  const route::Router& router() const { return router_; }
  const mac::Mac& mac() const { return mac_; }
  const store::Store& store() const { return store_; }
  const Stats& stats() const { return stats_; }
  // Unix time if a host told us (0 = unknown).
  uint32_t unix_time(Millis now) const;

 private:
  static route::Config router_config(const host::NodeConfig& c, uint8_t short_id, uint32_t random);
  static mac::Config mac_config(const host::NodeConfig& c, uint32_t random);
  void command(const uint8_t* p, size_t n, Millis now);
  bool emit(uint8_t kind, const uint8_t* payload, size_t n);
  void event(uint8_t ev, uint8_t req, const uint8_t* data, size_t n);
  void ack(uint8_t op, uint8_t req, uint8_t status);
  void pump();
  void announce(Millis now);
  void heard(uint8_t sender, Millis now);
  uint32_t rand32();

  host::NodeConfig cfg_;
  Identity id_;
  Platform& platform_;
  uint8_t short_id_;
  uint32_t rng_;
  route::Router router_;
  mac::Mac mac_;
  store::Store store_;
  host::Decoder dec_;

  uint8_t out_[kHostOut];
  size_t out_len_ = 0;
  struct Inflight {
    uint32_t seq;
    size_t end;  // offset in out_ just past the frame
  };
  Inflight inflight_[kInflight];
  size_t inflight_n_ = 0;
  bool attached_ = false;
  bool reboot_ = false;

  uint32_t unix_at_ = 0;      // unix time given by the host...
  Millis unix_given_ = 0;     // ...at this uptime
  Millis next_announce_ = 0;  // 0 = when time becomes known
  Millis last_tick_ = 0;

  struct Neighbour {
    uint8_t id;
    Millis last;
  };
  Neighbour nb_[kNeighbours] = {};
  size_t nb_count_ = 0;

  Stats stats_;
};

}  // namespace node
}  // namespace mesh

// The node application (docs/firmware.md §2, §5, §6, docs/board.md): wires
// the router, MAC and store to a radio, to up to kHostPorts hosts at once
// (PC over USB, phone over BLE; each one byte stream in core/hostlink format)
// and to an internet uplink (the relay worker). Pure C++17 and I/O-free, so
// the whole node runs in the host tests; the board layer (src/) only moves
// bytes and calls these methods:
//
//   radio:  radio_rx() on every received link frame, or air_rx() for a
//           packet a radio backend (Meshtastic) already unwrapped;
//           loop { a = radio_next(now): Idle → listen until a.wake,
//                  Cad → radio_cad(busy), Transmit → send, radio_tx_done() }
//   host:   host_attach(port) / host_detach(port), host_rx(port, bytes),
//           host_tx(port, buf) drains that port's output
//   uplink: uplink_take() packets the relay does not have, uplink_done() /
//           uplink_return() after the upload, uplink_rx() what the relay sent
//   timer:  tick(now) about once a second
//
// Every packet that enters on one side (air, a host, the uplink) is stored
// and handed to all the others: the store keeps one "already has it" bit per
// host port and one for the uplink.
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
constexpr size_t kHostOut = 8192;               // host output buffer (bytes)
constexpr size_t kNeighbours = 32;
constexpr Millis kNeighbourTTL = 3600ull * 1000;
constexpr size_t kInflight = 32;  // packets in the host output buffer
// 0 = USB, 1 = BLE host link, 2 = web browser (docs/web-access.md),
// 3–5 = phones in the Bluetooth mesh (packets only, untrusted),
// 6 = the handheld's own messaging client (lib/meshclient).
constexpr size_t kHostPorts = 7;
constexpr size_t kServed = 32;    // identities announced near us (gateway serve list)

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
  // Wi-Fi (boards that have it): return a host::kAck* status. SET_WIFI and
  // HOTSPOT come from trusted host ports only (USB, BLE), never the web port.
  virtual uint8_t set_wifi(const host::WifiConfig& w) {
    (void)w;
    return host::kAckUnknownOp;
  }
  virtual uint8_t hotspot(bool on) {
    (void)on;
    return host::kAckUnknownOp;
  }
  // Commands the node doesn't know itself (SCREEN, INPUT…), from trusted
  // ports only; the board answers with a host::kAck* status.
  virtual uint8_t board_command(size_t port, uint8_t op, const uint8_t* args, size_t n) {
    (void)port, (void)op, (void)args, (void)n;
    return host::kAckUnknownOp;
  }

 protected:
  ~Platform() = default;
};

struct Stats {
  uint32_t host_packets_in = 0, host_packets_rejected = 0, host_bad_frames = 0;
  uint32_t host_packets_out = 0, host_events_dropped = 0, announces = 0;
  uint32_t uplink_in = 0, uplink_out = 0;
};

class Node {
 public:
  // seed: the node identity (x25519 || ed25519 seeds, from flash; generated
  // on first boot). random: a hardware random value.
  // modulation: if set, the MAC times frames with it instead of the
  // config's preset (the board's radio always runs Meshtastic LongFast).
  Node(const host::NodeConfig& cfg, const uint8_t seed[kSeedSize], Platform& platform,
       store::Persist* persist, uint32_t random, const mac::Preset* modulation = nullptr);
  static constexpr size_t kWebPort = 2;
  static constexpr size_t kMeshPort0 = 3;  // first of the Bluetooth mesh peers' ports
  static constexpr size_t kMeshPorts = 3;
  static constexpr size_t kLocalPort = 6;

  // ---- radio ----
  void radio_rx(const uint8_t* frame, size_t n, int snr_db, Millis now);
  mac::Action radio_next(Millis now);
  void radio_cad(bool busy, Millis now);
  void radio_tx_done(Millis now);
  // air_rx takes one packet (no link header) that a radio backend unwrapped
  // from its own framing; sender is a one-byte hint of who transmitted it.
  void air_rx(const uint8_t* raw, size_t n, uint8_t sender, int snr_db, Millis now);

  // ---- host ----
  void host_attach(size_t port, Millis now);
  void host_detach(size_t port);
  bool host_attached(size_t port) const { return port < kHostPorts && ports_[port].attached; }
  // An untrusted port (the web port by default: anyone on the Wi-Fi can open
  // it) may exchange packets and read status, but not change or wipe the
  // node, and its receipts purge nothing.
  void set_port_trusted(size_t port, bool trusted) {
    if (port < kHostPorts) ports_[port].trusted = trusted;
  }
  bool any_host_attached() const;
  void host_rx(size_t port, const uint8_t* b, size_t n, Millis now);
  // host_tx copies up to cap pending output bytes of port; returns how many.
  size_t host_tx(size_t port, uint8_t* out, size_t cap);
  size_t host_pending(size_t port) const { return port < kHostPorts ? ports_[port].out_len : 0; }
  size_t hosts_pending() const;
  // Board events (the screen mirror): queued if the port is attached and its
  // output buffer has room for them; false otherwise (nothing is queued).
  bool host_event(size_t port, uint8_t ev, const uint8_t* data, size_t n);
  // Free bytes in a port's output buffer.
  size_t host_room(size_t port) const { return port < kHostPorts ? kHostOut - ports_[port].out_len : 0; }
  // Single-host form (port 0), as used by the simulator and older tests.
  void host_attach(Millis now) { host_attach(0, now); }
  void host_detach() { host_detach(0); }
  bool host_attached() const { return host_attached(0); }
  void host_rx(const uint8_t* b, size_t n, Millis now) { host_rx(0, b, n, now); }
  size_t host_tx(uint8_t* out, size_t cap) { return host_tx(0, out, cap); }
  size_t host_pending() const { return host_pending(0); }

  // ---- uplink (internet relay) ----
  // uplink_take writes packets the relay does not have yet into out as
  // relay frames (len u32 BE || packet), at most max packets, and their
  // store sequence numbers into seqs. They count as taken until
  // uplink_done() (uploaded) or uplink_return() (upload failed).
  size_t uplink_take(uint8_t* out, size_t cap, uint32_t* seqs, size_t max, size_t* count);
  void uplink_return(const uint32_t* seqs, size_t count);
  // uplink_rx takes one packet the relay sent: stored for the hosts and
  // broadcast on LoRa. Returns false if it was invalid or not new.
  bool uplink_rx(const uint8_t* raw, size_t n, Millis now);
  // served copies the destinations announced near us (air or hosts), for the
  // relay's gateway fetch; returns how many.
  size_t served(uint8_t (*out)[16], size_t max) const;

  void tick(Millis now);
  // A reboot (or wipe) was requested; the board reboots once host output drained.
  bool reboot_requested() const { return reboot_; }

  // Restores a stored record read back from flash at boot.
  bool restore(size_t slot, const store::Record& r, Millis now);

  // apply_config validates, saves and applies a new config (as SET_CONFIG).
  bool apply_config(const host::NodeConfig& c, Millis now);
  // set_time gives the node the Unix time (a host's HELLO, or NTP over Wi-Fi).
  void set_time(uint32_t unix_now, Millis now);

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
  Millis first_announce(Millis now);  // 5–30 s from now
  static route::Config router_config(const host::NodeConfig& c, uint8_t short_id, uint32_t random);
  static mac::Config mac_config(const host::NodeConfig& c, uint32_t random, const mac::Preset* modulation);
  struct Inflight {
    uint32_t seq;
    size_t end;  // offset in out just past the frame
  };
  struct Port {
    host::Decoder dec;
    uint8_t out[kHostOut];
    size_t out_len = 0;
    Inflight inflight[kInflight];
    size_t inflight_n = 0;
    bool attached = false;
    bool trusted = true;
  };
  static uint8_t sink_of(size_t port);
  void command(size_t port, const uint8_t* p, size_t n, Millis now);
  bool emit(size_t port, uint8_t kind, const uint8_t* payload, size_t n);
  void event(size_t port, uint8_t ev, uint8_t req, const uint8_t* data, size_t n);
  void ack(size_t port, uint8_t op, uint8_t req, uint8_t status);
  void pump();
  void pump(size_t port);
  // ingest handles a new packet from any side: purges on receipts (if purge:
  // the receipt is verified or from a trusted host), learns announced
  // destinations, stores it for every sink but `have`.
  void ingest(const Packet& p, const uint8_t* raw, size_t n, store::Origin origin, uint8_t have, bool purge,
              Millis now);
  void serve(const uint8_t dest[16]);
  void announce(Millis now);
  void heard(uint8_t sender, Millis now);
  uint32_t rand32();

  host::NodeConfig cfg_;
  Identity id_;
  Platform& platform_;
  const mac::Preset* modulation_;
  uint8_t short_id_;
  uint32_t rng_;
  route::Router router_;
  mac::Mac mac_;
  store::Store store_;
  Port ports_[kHostPorts];
  bool reboot_ = false;
  uint8_t served_[kServed][16] = {};
  size_t served_n_ = 0, served_next_ = 0;

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

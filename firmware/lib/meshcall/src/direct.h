// A direct IP path between two devices (docs/connectivity.md, port of
// core/direct for one UDP socket): host and reflexive candidates (STUN on the
// relays), the NAT type, authenticated UDP checks with hole punching, the
// best pair, then a link of datagrams sealed with ChaCha20-Poly1305 that
// follows the peer when its address changes. Not here (yet): sockets per
// interface, router port mapping, the symmetric-NAT birthday method, TURN.
//
// Event driven, no threads, no heap: the owner opens the socket through Net,
// feeds every datagram to on_datagram() and calls tick() every few ms.
// Pure C++, no Arduino: host-tested (in-memory network) and checked against
// spec/test-vectors/calls.json.
#pragma once

#include <cstddef>
#include <cstdint>

#include "callsignal.h"

namespace mesh {
namespace call {

// Largest link payload: datagrams stay under 1200 bytes.
constexpr size_t kLinkHead = 4 + 8;
constexpr size_t kMaxPayload = 1200 - kLinkHead - 16;

// What the agent needs from the network.
class Net {
 public:
  virtual ~Net() = default;
  // A UDP socket on every interface, any port; -1 on failure.
  virtual int open() = 0;
  virtual void close(int sock) = 0;
  virtual uint16_t port(int sock) = 0;
  virtual bool send(int sock, const Addr& to, const uint8_t* d, size_t n) = 0;
  // Addresses of the up interfaces (ports ignored).
  virtual size_t addrs(Addr* out, size_t max) = 0;
};

struct AgentConfig {
  Addr reflectors[4];  // STUN: every relay's UDP 3478 and 3479
  size_t n_reflectors = 0;
  uint32_t gather_ms = 4000;
  uint32_t check_ms = 10000;
};

struct Path {
  Candidate local, remote;
  uint32_t rtt_ms = 0;
  int cls = 0;  // connectivity.md §5: 1 local network … 6 relayed
};

class Agent {
 public:
  enum class State : uint8_t { Gathering, Gathered, Checking, Connected, Failed, Closed };
  using Random = void (*)(uint8_t* out, size_t n);
  using Receiver = void (*)(void* ctx, const uint8_t* d, size_t n);

  // sid and key come with the signaling (the call id and the OFFER's key);
  // controlling: the side that started.
  Agent(Net& net, const AgentConfig& cfg, const uint8_t sid[16], const uint8_t key[32], bool controlling, Random rnd,
        uint32_t now_ms);
  ~Agent();
  Agent(const Agent&) = delete;
  Agent& operator=(const Agent&) = delete;

  State state() const { return state_; }
  int socket() const { return sock_; }
  // This side's candidates and NAT (complete once state() leaves Gathering).
  const Offer& offer() const { return local_; }
  // Runs the checks against the peer's offer (from Gathered).
  void connect(const Offer& remote, uint32_t now_ms);
  void tick(uint32_t now_ms);
  void on_datagram(const Addr& from, const uint8_t* d, size_t n, uint32_t now_ms);

  // The link, once Connected: received payloads go to the receiver.
  void set_receiver(Receiver fn, void* ctx) { recv_fn_ = fn, recv_ctx_ = ctx; }
  bool send(const uint8_t* d, size_t n);
  const Path& path() const { return path_; }
  // Why it failed or closed.
  const char* reason() const { return reason_; }
  void close();

  // For tests: the probe and STUN bytes (core/direct's formats).
  size_t probe(uint8_t typ, const uint8_t tx[8], const Addr& seen, uint8_t* out, size_t cap) const;

 private:
  struct Refl {
    Addr dst, mapped;
    uint8_t tx[12];
    uint8_t tries = 0;
    uint32_t next = 0;
    bool done = false;
  };
  struct Pair {
    Candidate remote;
    uint8_t tx[8];
    uint32_t sent = 0, next = 0, interval = 100, rtt = 0;
    bool ok = false, once = false;
    Addr seen;
  };
  static constexpr size_t kMaxPairs = 96;

  void finish_gather(uint32_t now);
  Pair* add_pair(const Candidate& c);
  void on_probe(const Addr& from, const uint8_t* d, size_t n, uint32_t now);
  void on_stun(const uint8_t* d, size_t n);
  void on_data(const Addr& from, const uint8_t* d, size_t n, uint32_t now);
  void choose(uint32_t now);
  Kind local_kind(const Pair& p) const;
  void fail(const char* why);
  bool fresh(uint64_t seq) const;
  void mark(uint64_t seq);

  Net& net_;
  AgentConfig cfg_;
  Random rnd_;
  uint8_t sid_[16], key_[32], mac_key_[32];
  bool controlling_;
  State state_ = State::Gathering;
  int sock_ = -1;
  uint32_t t0_ = 0, check_end_ = 0, settle_ = 0;
  bool settling_ = false;
  Refl refl_[4];
  size_t n_refl_ = 0;
  Addr hosts_[4];
  size_t n_hosts_ = 0;
  Offer local_, remote_;
  Pair pairs_[kMaxPairs];
  size_t n_pairs_ = 0;
  const char* reason_ = "";
  // link
  Path path_;
  Addr peer_;
  uint8_t seal_key_[32], open_key_[32];
  uint64_t seq_ = 0, max_rx_ = 0, window_ = 0;
  uint32_t last_rx_ = 0, last_tx_ = 0;
  Receiver recv_fn_ = nullptr;
  void* recv_ctx_ = nullptr;
};

}  // namespace call
}  // namespace mesh

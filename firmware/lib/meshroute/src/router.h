// LoRa managed-flood router (docs/firmware.md §2a, §5), mirroring the LoRa
// behaviour of core/router. Pure C++17, no heap, no Arduino, no I/O: the board
// feeds it received link frames and asks it which frames are due for the MAC.
//
//   radio RX ──► on_receive() ──► verdict (caller hands New packets to the
//                                  phone / uplink) + maybe a scheduled rebroadcast
//   poll() ──► link frame due for the MAC queue
//   MAC ──► on_transmitted() when a frame actually went on air
//   tick() periodically (expiry)
//
// Every new packet is rebroadcast once, after an SNR-ordered delay (nodes that
// heard it weakly, likely farthest away, go first). Hearing enough neighbours
// rebroadcast it first cancels our copy. Packets this node originates are
// retransmitted up to `retries` times unless a neighbour's rebroadcast (the
// implicit ACK) is heard. RECEIPTs verified against a key learned from an
// ANNOUNCE purge queued copies of the packet they acknowledge.
#pragma once

#include <cstddef>
#include <cstdint>

#include "identity.h"
#include "packet.h"

namespace mesh {
namespace route {

using Millis = uint64_t;  // monotonic milliseconds
constexpr Millis kNever = ~Millis(0);

#ifndef MESH_SEEN_CAPACITY
#define MESH_SEEN_CAPACITY 1024
#endif
#ifndef MESH_PENDING_CAPACITY
#define MESH_PENDING_CAPACITY 16
#endif
#ifndef MESH_KEY_CAPACITY
#define MESH_KEY_CAPACITY 64
#endif
constexpr size_t kSeenCapacity = MESH_SEEN_CAPACITY;
constexpr size_t kPendingCapacity = MESH_PENDING_CAPACITY;
constexpr size_t kKeyCapacity = MESH_KEY_CAPACITY;

// Fragment value marking "every fragment of this ID was acknowledged".
constexpr uint8_t kFragAcked = 0xFF;

// Dedup key: packet ID + fragment index (0 if unfragmented), as core/packet.Key.
struct Key {
  uint8_t id[8] = {};
  uint8_t frag = 0;

  static Key of(const Packet& p);
  bool operator==(const Key& o) const;
};

// Transmit priority, lowest value first (docs/firmware.md §5).
enum class Priority : uint8_t {
  Receipt = 0,
  DataPriority = 1,
  Data = 2,
  Summary = 3,  // SUMMARY and PATH_REQ
  Announce = 4,
};

Priority priority_of(const Packet& p);

// Bounded seen-cache. Entries are kept in arrival order, so the oldest is
// evicted first when full and expiry pops from the front.
class SeenCache {
 public:
  explicit SeenCache(Millis ttl) : ttl_(ttl) {}
  bool contains(const Key& k, Millis now) const;
  // insert adds k (no-op if already present and not expired).
  void insert(const Key& k, Millis now);
  void expire(Millis now);
  size_t size() const { return count_; }

 private:
  struct Entry {
    Key key;
    uint32_t at;  // seconds: keeps an entry at 16 bytes
  };
  bool live(const Entry& e, Millis now) const { return now / 1000 - e.at < ttl_ / 1000; }
  Entry ring_[kSeenCapacity];
  size_t head_ = 0, count_ = 0;
  Millis ttl_;
};

// Public keys learned from verified ANNOUNCEs, so RECEIPTs can be verified
// before they purge anything. Oldest replaced first when full.
class KeyCache {
 public:
  void put(const uint8_t dest[16], const PublicKey& pub);
  const PublicKey* get(const uint8_t dest[16]) const;
  size_t size() const { return count_; }

 private:
  struct Entry {
    uint8_t dest[16];
    PublicKey pub;
  };
  Entry e_[kKeyCapacity];
  size_t next_ = 0, count_ = 0;
};

enum class Role : uint8_t { Client, Repeater, Gateway };

struct Config {
  uint8_t short_id = 0;  // our LoRa link-frame sender ID
  Role role = Role::Repeater;
  uint32_t seed = 1;  // jitter PRNG seed (use a hardware random value on boards)

  Millis seen_ttl = 24ull * 3600 * 1000;
  // Rebroadcast delay = delay_min + delay_window * strength + rand(jitter),
  // strength = (snr - snr_low) / (snr_high - snr_low) clamped to [0, 1].
  Millis delay_min = 200;
  Millis delay_window = 3000;
  Millis delay_jitter = 500;
  int snr_low = -20;  // dB, about the SX126x sensitivity floor
  int snr_high = 10;
  // CLIENT nodes forward at low priority: extra delay so repeaters go first.
  Millis client_extra_delay = 3000;
  // Cancel our pending rebroadcast after hearing this many copies.
  uint8_t cancel_after = 2;
  // Originated packets: retransmissions while no implicit ACK is heard.
  uint8_t retries = 2;
  Millis ack_timeout = 20000;  // after the frame went on air
  Millis ack_jitter = 10000;
  // A frame handed to the MAC that never went on air (dropped for budget) is
  // forgotten after this long.
  Millis handed_timeout = 3600ull * 1000;
};

enum class Verdict : uint8_t {
  Invalid,    // undecodable, or an announce/receipt that fails verification
  Duplicate,  // already seen
  Acked,      // DATA whose verified RECEIPT was already heard
  Local,      // hop-local LINK packet: for the caller, never forwarded or deduped
  New,        // first time seen: hand to phone / uplink
};

const char* verdict_name(Verdict v);

struct RxResult {
  Verdict verdict = Verdict::Invalid;
  uint8_t sender = 0;      // link-frame sender short ID (if the frame decoded)
  bool scheduled = false;  // a rebroadcast was queued
  bool purged = false;     // a verified receipt purged pending copies
  Packet packet;           // valid unless Invalid; body aliases the caller's frame
};

// A link frame ready for the MAC queue.
struct Frame {
  uint8_t data[kLoRaRadioMax];
  size_t len = 0;
  Priority prio = Priority::Data;
  Key key;
};

struct Stats {
  uint32_t rx = 0, invalid = 0, dup = 0, acked = 0, fresh = 0;
  uint32_t scheduled = 0, cancelled = 0, purged = 0, hop_limit = 0;
  uint32_t sent = 0, retries = 0, acks = 0, gave_up = 0, pending_full = 0;
};

class Router {
 public:
  explicit Router(const Config& cfg);

  // on_receive processes one received link frame.
  RxResult on_receive(const uint8_t* frame, size_t n, int snr_db, Millis now);

  // send queues a packet (encoded, without link header) for immediate
  // transmission. own=true: originated here (from our phone, or our own
  // announce/receipt), sent as is with implicit-ACK retries. own=false: arrived
  // from another tier (relay via gateway, phone mesh); the hop count is
  // incremented first. Returns false if it was already seen, may not go on
  // LoRa (NO_LORA, hop limit, too large) or the pending table is full.
  bool send(const uint8_t* raw, size_t n, bool own, Millis now);

  // poll fills out with the most urgent due frame, if any. *next_due (if
  // non-null) receives the earliest time something will be due, or kNever.
  bool poll(Millis now, Frame& out, Millis* next_due = nullptr);

  // on_transmitted tells the router the frame with key k went on air.
  void on_transmitted(const Key& k, Millis now);

  void tick(Millis now);
  void set_role(Role r) { cfg_.role = r; }
  // Earliest time a frame will be due for poll(), or kNever.
  Millis next_due() const;

  const Stats& stats() const { return stats_; }
  size_t pending() const;
  const SeenCache& seen() const { return seen_; }
  const KeyCache& keys() const { return keys_; }

 private:
  enum class State : uint8_t { Free, Waiting, Handed, AwaitAck };
  struct Pending {
    State state = State::Free;
    bool own = false;
    uint8_t heard = 0;
    uint8_t retries_left = 0;
    Priority prio = Priority::Data;
    Key key;
    Millis due = 0;
    uint8_t frame[kLoRaRadioMax];
    size_t len = 0;
  };

  Pending* find(const Key& k);
  Pending* alloc(bool own);
  bool schedule(const uint8_t* raw, size_t n, const Packet& p, bool own, Millis due);
  void heard_copy(const Key& k);
  size_t purge(const uint8_t acked[8]);
  Millis rebroadcast_delay(int snr_db);
  uint32_t rand();

  Config cfg_;
  uint32_t rng_;
  SeenCache seen_;
  KeyCache keys_;
  Pending pend_[kPendingCapacity];
  Stats stats_;
};

}  // namespace route
}  // namespace mesh

// LoRa MAC (docs/firmware.md §2a "Airtime budget", §4, §5), mirroring the
// radio model of sim/lora.go: exact time-on-air, a sliding one-hour
// duty-cycle budget with a cap on announce airtime, a priority queue, and
// listen-before-talk (channel activity detection) with random backoff.
// Pure C++17, no heap, no I/O. The radio layer drives it:
//
//   router.poll() frames ──► enqueue()
//   loop: a = next(now)
//     Idle      → sleep / receive until a.wake (or until something is enqueued)
//     Cad       → run channel activity detection, then cad_result(busy, now)
//     Transmit  → send a.frame (half-duplex: no RX meanwhile), then
//                 tx_done(now) and pass the key to router.on_transmitted()
#pragma once

#include <cstddef>
#include <cstdint>

#include "router.h"

namespace mesh {
namespace mac {

using route::Millis;

struct Preset {
  uint8_t sf;      // spreading factor 6..12
  uint32_t bw_hz;  // bandwidth
  uint8_t cr;      // coding rate 1..4 → 4/5..4/8
};

// docs/firmware.md §4.
constexpr Preset kFast{7, 250000, 1};
constexpr Preset kBalanced{9, 125000, 1};
constexpr Preset kLong{11, 125000, 4};

// airtime_us is the Semtech SX126x time on air for a payload of n bytes:
// 8-symbol preamble, explicit header, CRC on, low data rate optimisation when
// a symbol lasts longer than 16 ms.
uint32_t airtime_us(const Preset& p, size_t n);
inline uint32_t airtime_ms(const Preset& p, size_t n) { return (airtime_us(p, n) + 999) / 1000; }

// DutyCycle tracks transmit airtime over a sliding hour in one-minute
// buckets. A bucket counts in full until the whole minute has left the
// window, so the budget errs on the safe (regulatory) side.
class DutyCycle {
 public:
  // duty_ppm: 10000 = 1 %. announce_pct: share of the budget announces may use.
  DutyCycle(uint32_t duty_ppm, uint8_t announce_pct);
  uint32_t budget_ms() const { return budget_ms_; }
  uint32_t used_ms(Millis now) const;
  uint32_t announce_used_ms(Millis now) const;
  bool fits(Millis now, uint32_t air_ms) const;
  bool announce_fits(Millis now, uint32_t air_ms) const;
  void record(Millis now, uint32_t air_ms, bool announce);
  // When the next bucket leaves the window (the earliest moment more budget
  // can become available).
  Millis next_release(Millis now) const;

 private:
  static constexpr size_t kBuckets = 61;  // 60 minutes + the current one
  struct Bucket {
    uint64_t minute = ~uint64_t(0);
    uint32_t all = 0, announce = 0;
  };
  bool live(const Bucket& b, Millis now) const;
  Bucket b_[kBuckets];
  uint32_t budget_ms_, announce_ms_;
};

struct Config {
  Preset preset = kBalanced;
  uint32_t duty_ppm = 10000;  // EU-868 style 1 %
  uint8_t announce_pct = 10;  // forwarded + own announces ≤ 10 % of the budget
  bool cad = true;            // listen before talk
  Millis backoff_min = 100;   // first CAD-busy backoff range [min, max)
  Millis backoff_max = 1000;
  uint8_t backoff_doublings = 5;  // cap on exponential growth
  uint32_t seed = 1;
};

enum class Kind : uint8_t { Idle, Cad, Transmit };

struct Action {
  Kind kind = Kind::Idle;
  Millis wake = route::kNever;  // Idle: when to call next() again at the latest
  const route::Frame* frame = nullptr;  // Transmit: valid until tx_done()
  uint32_t air_ms = 0;                  // Transmit: expected time on air
};

struct Stats {
  uint32_t frames = 0, airtime_ms = 0, queue_drops = 0, budget_waits = 0;
  uint32_t ann_dropped = 0, cad_busy = 0, too_long = 0;
};

#ifndef MESH_MAC_QUEUE
#define MESH_MAC_QUEUE 8
#endif
constexpr size_t kQueueCapacity = MESH_MAC_QUEUE;

class Mac {
 public:
  explicit Mac(const Config& cfg);

  // enqueue copies f. When full, the least urgent frame (f itself if it is
  // the least urgent) is dropped. Returns false if f was dropped.
  bool enqueue(const route::Frame& f, Millis now);

  Action next(Millis now);
  void cad_result(bool busy, Millis now);
  // tx_done finishes the current transmission and returns its key (for
  // Router::on_transmitted). Returns false if nothing was transmitting.
  bool tx_done(Millis now, route::Key* key);

  size_t queued() const { return count_; }
  bool busy() const { return state_ != State::Ready; }
  const DutyCycle& duty() const { return duty_; }
  const Stats& stats() const { return stats_; }
  const Config& config() const { return cfg_; }

 private:
  enum class State : uint8_t { Ready, CadPending, Clear, Transmitting };
  struct Slot {
    route::Frame f;
    uint32_t seq;
  };
  int head() const;  // most urgent: lowest priority value, then FIFO
  void remove(int i);
  uint32_t rand();

  Config cfg_;
  DutyCycle duty_;
  Slot q_[kQueueCapacity];
  size_t count_ = 0;
  uint32_t seq_ = 0;
  State state_ = State::Ready;
  route::Frame cur_;
  uint32_t cur_air_ = 0;
  uint8_t cad_tries_ = 0;
  Millis backoff_until_ = 0;
  uint32_t rng_;
  Stats stats_;
};

}  // namespace mac
}  // namespace mesh

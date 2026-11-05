#include "mac.h"

#include <cstring>

namespace mesh {
namespace mac {

uint32_t airtime_us(const Preset& p, size_t n) {
  // Work in nanoseconds so every common bandwidth gives an exact symbol time.
  const uint64_t tsym = (uint64_t(1) << p.sf) * 1000000000ull / p.bw_hz;
  const int de = tsym > 16000000ull ? 1 : 0;  // low data rate optimisation
  const uint64_t preamble = tsym * 49 / 4;    // (8 + 4.25) symbols
  const int64_t num = 8 * int64_t(n) - 4 * int64_t(p.sf) + 28 + 16;  // CRC on, explicit header
  const int64_t den = 4 * (int64_t(p.sf) - 2 * de);
  int64_t nsym = 0;
  if (num > 0) nsym = (num + den - 1) / den * (p.cr + 4);
  return uint32_t((preamble + uint64_t(8 + nsym) * tsym) / 1000);
}

// ---- DutyCycle ----

DutyCycle::DutyCycle(uint32_t duty_ppm, uint8_t announce_pct)
    : budget_ms_(uint32_t(3600000ull * duty_ppm / 1000000ull)),
      announce_ms_(uint32_t(uint64_t(budget_ms_) * announce_pct / 100)) {}

bool DutyCycle::live(const Bucket& b, Millis now) const {
  return b.minute != ~uint64_t(0) && now / 60000 - b.minute < kBuckets;
}

uint32_t DutyCycle::used_ms(Millis now) const {
  uint32_t s = 0;
  for (const auto& b : b_)
    if (live(b, now)) s += b.all;
  return s;
}

uint32_t DutyCycle::announce_used_ms(Millis now) const {
  uint32_t s = 0;
  for (const auto& b : b_)
    if (live(b, now)) s += b.announce;
  return s;
}

bool DutyCycle::fits(Millis now, uint32_t air_ms) const { return used_ms(now) + air_ms <= budget_ms_; }

bool DutyCycle::announce_fits(Millis now, uint32_t air_ms) const {
  return announce_used_ms(now) + air_ms <= announce_ms_;
}

void DutyCycle::record(Millis now, uint32_t air_ms, bool announce) {
  uint64_t m = now / 60000;
  Bucket& b = b_[m % kBuckets];
  if (b.minute != m) b = Bucket{m, 0, 0};
  b.all += air_ms;
  if (announce) b.announce += air_ms;
}

Millis DutyCycle::next_release(Millis now) const {
  Millis best = route::kNever;
  for (const auto& b : b_) {
    if (!live(b, now) || b.all == 0) continue;
    Millis t = (b.minute + kBuckets) * 60000;
    if (t < best) best = t;
  }
  return best == route::kNever ? now : best;
}

// ---- Mac ----

Mac::Mac(const Config& cfg) : cfg_(cfg), duty_(cfg.duty_ppm, cfg.announce_pct), rng_(cfg.seed ? cfg.seed : 1) {}

uint32_t Mac::rand() {
  rng_ ^= rng_ << 13;
  rng_ ^= rng_ >> 17;
  rng_ ^= rng_ << 5;
  return rng_;
}

int Mac::head() const {
  int best = -1;
  for (size_t i = 0; i < count_; i++) {
    if (best < 0 || q_[i].f.prio < q_[best].f.prio || (q_[i].f.prio == q_[best].f.prio && q_[i].seq < q_[best].seq))
      best = int(i);
  }
  return best;
}

void Mac::remove(int i) {
  q_[i] = q_[count_ - 1];
  count_--;
}

bool Mac::enqueue(const route::Frame& f, Millis) {
  if (count_ < kQueueCapacity) {
    q_[count_++] = Slot{f, seq_++};
    return true;
  }
  int worst = 0;
  for (size_t i = 1; i < count_; i++) {
    if (q_[i].f.prio > q_[worst].f.prio || (q_[i].f.prio == q_[worst].f.prio && q_[i].seq > q_[worst].seq))
      worst = int(i);
  }
  stats_.queue_drops++;
  if (f.prio >= q_[worst].f.prio) return false;
  q_[worst] = Slot{f, seq_++};
  return true;
}

Action Mac::next(Millis now) {
  Action a;
  if (state_ == State::Transmitting) return a;  // waiting for tx_done
  if (state_ == State::CadPending) {
    a.kind = Kind::Cad;
    return a;
  }
  for (;;) {
    int i = head();
    if (i < 0) {
      state_ = State::Ready;
      return a;
    }
    const route::Frame& f = q_[i].f;
    uint32_t air = airtime_ms(cfg_.preset, f.len);
    if (air > duty_.budget_ms()) {  // can never be sent under this budget
      stats_.too_long++;
      remove(i);
      continue;
    }
    bool ann = f.prio == route::Priority::Announce;
    if (ann && !duty_.announce_fits(now, air)) {
      stats_.ann_dropped++;  // announces are refreshed later anyway
      remove(i);
      continue;
    }
    if (!duty_.fits(now, air)) {
      stats_.budget_waits++;
      state_ = State::Ready;
      a.wake = duty_.next_release(now);
      return a;
    }
    if (backoff_until_ > now) {
      state_ = State::Ready;
      a.wake = backoff_until_;
      return a;
    }
    if (cfg_.cad && state_ != State::Clear) {
      state_ = State::CadPending;
      a.kind = Kind::Cad;
      return a;
    }
    cur_ = f;
    cur_air_ = air;
    remove(i);
    duty_.record(now, air, ann);
    state_ = State::Transmitting;
    cad_tries_ = 0;
    stats_.frames++;
    stats_.airtime_ms += air;
    a.kind = Kind::Transmit;
    a.frame = &cur_;
    a.air_ms = air;
    return a;
  }
}

void Mac::cad_result(bool busy, Millis now) {
  if (state_ != State::CadPending) return;
  if (!busy) {
    state_ = State::Clear;
    return;
  }
  stats_.cad_busy++;
  uint8_t e = cad_tries_ < cfg_.backoff_doublings ? cad_tries_ : cfg_.backoff_doublings;
  Millis span = cfg_.backoff_max > cfg_.backoff_min ? cfg_.backoff_max - cfg_.backoff_min : 1;
  backoff_until_ = now + ((cfg_.backoff_min + rand() % span) << e);
  if (cad_tries_ < 255) cad_tries_++;
  state_ = State::Ready;
}

bool Mac::tx_done(Millis, route::Key* key) {
  if (state_ != State::Transmitting) return false;
  if (key) *key = cur_.key;
  state_ = State::Ready;
  return true;
}

}  // namespace mac
}  // namespace mesh

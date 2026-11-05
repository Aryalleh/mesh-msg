// LoRa MAC: airtime, duty-cycle budget, priority queue, CAD backoff.
#include <memory>

#include "check.h"
#include "mac.h"
#include "router.h"

using mesh::mac::Action;
using mesh::mac::Kind;
using mesh::mac::Mac;
using mesh::route::Frame;
using mesh::route::Millis;
using mesh::route::Priority;

namespace {

constexpr Millis kMin = 60000, kHour = 60 * kMin;

struct Ref {
  uint8_t sf;
  uint32_t bw;
  uint8_t cr;
  size_t n;
  uint32_t us;
};

// From sim.LoRaParams.Airtime (Go, float64, truncated to microseconds).
const Ref kRefs[] = {
    {7, 250000, 1, 1, 12927},     {7, 250000, 1, 10, 20608},    {7, 250000, 1, 27, 33408},
    {7, 250000, 1, 51, 51328},    {7, 250000, 1, 100, 87168},   {7, 250000, 1, 200, 158848},
    {7, 250000, 1, 255, 199808},  {9, 125000, 1, 1, 103423},    {9, 125000, 1, 10, 144384},
    {9, 125000, 1, 27, 226303},   {9, 125000, 1, 51, 328704},   {9, 125000, 1, 100, 553983},
    {9, 125000, 1, 200, 1004544}, {9, 125000, 1, 255, 1250303}, {11, 125000, 4, 1, 462848},
    {11, 125000, 4, 10, 724992},  {11, 125000, 4, 27, 1118208}, {11, 125000, 4, 51, 1904640},
    {11, 125000, 4, 100, 3346432}, {11, 125000, 4, 200, 6230016}, {11, 125000, 4, 255, 7802880},
    {12, 125000, 1, 1, 827391},   {12, 125000, 1, 10, 991232},  {12, 125000, 1, 27, 1646591},
    {12, 125000, 1, 51, 2465792}, {12, 125000, 1, 100, 3940352}, {12, 125000, 1, 200, 7217152},
    {12, 125000, 1, 255, 9019392}, {7, 125000, 1, 1, 25855},    {7, 125000, 1, 10, 41216},
    {7, 125000, 1, 27, 66816},    {7, 125000, 1, 51, 102656},   {7, 125000, 1, 100, 174336},
    {7, 125000, 1, 200, 317696},  {7, 125000, 1, 255, 399616},
};

Frame frame(Priority prio, uint8_t id, size_t len = 100) {
  Frame f;
  f.len = len;
  std::memset(f.data, id, len);
  f.prio = prio;
  f.key.id[0] = id;
  return f;
}

mesh::mac::Config nocad() {
  mesh::mac::Config c;
  c.cad = false;
  return c;
}

// Runs the MAC without CAD until idle or `until`; returns frames sent.
int drain(Mac& m, Millis& now, Millis until) {
  int sent = 0;
  while (now < until) {
    Action a = m.next(now);
    if (a.kind == Kind::Transmit) {
      now += a.air_ms;
      m.tx_done(now, nullptr);
      sent++;
    } else if (a.kind == Kind::Idle) {
      if (a.wake == mesh::route::kNever) break;
      now = a.wake > now ? a.wake : now + 1;
    } else {
      m.cad_result(false, now);
    }
  }
  return sent;
}

void airtime() {
  for (const auto& r : kRefs) {
    uint32_t got = mesh::mac::airtime_us({r.sf, r.bw, r.cr}, r.n);
    uint32_t diff = got > r.us ? got - r.us : r.us - got;
    CHECKC(diff <= 1, "sf" + std::to_string(r.sf) + " n=" + std::to_string(r.n) + " got=" + std::to_string(got));
  }
  // Exact integer value where Go's float truncates.
  CHECK(mesh::mac::airtime_us(mesh::mac::kBalanced, 1) == 103424);
  // docs/firmware.md §4 ballparks for 200 B.
  CHECK(mesh::mac::airtime_ms(mesh::mac::kFast, 200) < 250);
  CHECK(mesh::mac::airtime_ms(mesh::mac::kBalanced, 200) > 900 && mesh::mac::airtime_ms(mesh::mac::kBalanced, 200) < 1100);
  CHECK(mesh::mac::airtime_ms(mesh::mac::kLong, 200) > 5000);
  // Monotonic in size.
  for (size_t n = 1; n < 255; n++)
    CHECK(mesh::mac::airtime_us(mesh::mac::kBalanced, n) <= mesh::mac::airtime_us(mesh::mac::kBalanced, n + 1));
}

void duty_cycle() {
  mesh::mac::DutyCycle d(10000, 10);
  CHECK(d.budget_ms() == 36000);
  CHECK(d.fits(0, 36000) && !d.fits(0, 36001));
  d.record(30 * 1000, 20000, false);  // minute 0
  d.record(10 * kMin, 6000, true);    // minute 10
  CHECK(d.used_ms(10 * kMin) == 26000 && d.announce_used_ms(10 * kMin) == 6000);
  CHECK(!d.announce_fits(10 * kMin, 1));  // announce cap 3.6 s already exceeded
  CHECK(d.fits(10 * kMin, 10000) && !d.fits(10 * kMin, 10001));
  // Minute 0 leaves the window only once all of it is older than an hour.
  CHECK(d.next_release(10 * kMin) == 61 * kMin);
  CHECK(d.used_ms(61 * kMin - 1) == 26000 && d.used_ms(61 * kMin) == 6000);
  CHECK(d.used_ms(71 * kMin) == 0 && d.next_release(71 * kMin) == 71 * kMin);
  // Bucket reuse after wrap-around.
  d.record(200 * kMin, 1000, false);
  CHECK(d.used_ms(200 * kMin) == 1000);
}

void priority_queue() {
  Mac m(nocad());
  CHECK(m.enqueue(frame(Priority::Data, 1), 0));
  CHECK(m.enqueue(frame(Priority::Announce, 2), 0));
  CHECK(m.enqueue(frame(Priority::Receipt, 3), 0));
  CHECK(m.enqueue(frame(Priority::DataPriority, 4), 0));
  CHECK(m.enqueue(frame(Priority::Data, 5), 0));
  uint8_t order[5];
  Millis now = 0;
  for (auto& o : order) {
    Action a = m.next(now);
    CHECK(a.kind == Kind::Transmit && a.frame);
    o = a.frame->key.id[0];
    CHECK(m.next(now).kind == Kind::Idle);  // half-duplex: busy until tx_done
    mesh::route::Key k;
    now += a.air_ms;
    CHECK(m.tx_done(now, &k) && k.id[0] == o);
  }
  CHECK(order[0] == 3 && order[1] == 4 && order[2] == 1 && order[3] == 5 && order[4] == 2);
  CHECK(!m.tx_done(now, nullptr));

  // Full queue: the least urgent, newest frame goes.
  Mac q(nocad());
  for (size_t i = 0; i < mesh::mac::kQueueCapacity; i++) CHECK(q.enqueue(frame(Priority::Data, uint8_t(i)), 0));
  CHECK(!q.enqueue(frame(Priority::Data, 99), 0));
  CHECK(!q.enqueue(frame(Priority::Announce, 98), 0));
  CHECK(q.enqueue(frame(Priority::Receipt, 97), 0));
  CHECK(q.queued() == mesh::mac::kQueueCapacity && q.stats().queue_drops == 3);
  Action a = q.next(0);
  CHECK(a.frame->key.id[0] == 97);
  q.tx_done(1, nullptr);
  bool last_gone = true;
  Millis now2 = 2;
  while (true) {
    a = q.next(now2);
    if (a.kind != Kind::Transmit) break;
    if (a.frame->key.id[0] == mesh::mac::kQueueCapacity - 1) last_gone = false;
    now2 += a.air_ms;
    q.tx_done(now2, nullptr);
  }
  CHECK(last_gone);
}

void budget() {
  Mac m(nocad());
  Millis now = 0;
  // BALANCED 255 B frames ≈ 1.25 s: 28 fit in 36 s per hour.
  int sent = 0;
  Action a;
  for (;;) {
    for (int i = 0; m.queued() < mesh::mac::kQueueCapacity; i++) m.enqueue(frame(Priority::Data, uint8_t(i), 255), now);
    a = m.next(now);
    if (a.kind != Kind::Transmit) break;
    now += a.air_ms;
    m.tx_done(now, nullptr);
    sent++;
  }
  uint32_t air = mesh::mac::airtime_ms(mesh::mac::kBalanced, 255);
  CHECK(sent == int(36000 / air) && m.stats().frames == 36000 / air);
  CHECK(m.stats().airtime_ms <= 36000 && m.stats().budget_waits > 0);
  CHECK(a.kind == Kind::Idle && a.wake > now && a.wake <= 61 * kMin);
  CHECK(m.next(a.wake - 1).kind == Kind::Idle);
  // An hour later the budget is back.
  Millis later = now;
  int more = drain(m, later, 3 * kHour);
  CHECK(more > 0 && later >= 61 * kMin);
  // Never more than the budget in any hour window.
  CHECK(m.duty().used_ms(later) <= 36000);

  // Announces: at most 10 % of the budget, excess dropped rather than queued.
  Mac an(nocad());
  Millis t = 0;
  for (int i = 0; i < 20; i++) {
    an.enqueue(frame(Priority::Announce, uint8_t(i), 200), t);
    drain(an, t, kHour);
  }
  CHECK(an.stats().airtime_ms <= 3600 && an.stats().ann_dropped > 0);
  CHECK(an.queued() == 0);

  // A frame that can never fit the budget is dropped, not stuck forever.
  auto tiny = nocad();
  tiny.duty_ppm = 100;  // 360 ms/hour
  tiny.preset = mesh::mac::kLong;
  Mac tm(tiny);
  tm.enqueue(frame(Priority::Data, 1, 200), 0);
  CHECK(tm.next(0).kind == Kind::Idle && tm.stats().too_long == 1 && tm.queued() == 0);
}

void cad_backoff() {
  mesh::mac::Config c;
  c.seed = 3;
  Mac m(c);
  m.enqueue(frame(Priority::Data, 1), 0);
  Millis now = 0;
  CHECK(m.next(now).kind == Kind::Cad);
  CHECK(m.next(now).kind == Kind::Cad);  // still waiting for the result
  Millis prev_wait = 0;
  for (int i = 0; i < 4; i++) {
    m.cad_result(true, now);
    Action a = m.next(now);
    CHECK(a.kind == Kind::Idle);
    Millis wait = a.wake - now;
    CHECK(wait >= (Millis(100) << i) && wait < (Millis(1000) << i));
    if (i > 0) CHECK(wait > prev_wait / 4);
    prev_wait = wait;
    CHECK(m.next(a.wake - 1).kind == Kind::Idle);
    now = a.wake;
    CHECK(m.next(now).kind == Kind::Cad);
  }
  m.cad_result(false, now);
  Action a = m.next(now);
  CHECK(a.kind == Kind::Transmit && a.frame->key.id[0] == 1);
  CHECK(m.stats().cad_busy == 4);
  m.tx_done(now + a.air_ms, nullptr);
  // Backoff growth is capped.
  m.enqueue(frame(Priority::Data, 2), now);
  CHECK(m.next(now).kind == Kind::Cad);
  for (int i = 0; i < 20; i++) {
    m.cad_result(true, now);
    Action b = m.next(now);
    CHECK(b.wake - now < (Millis(1000) << 5));
    now = b.wake;
    m.next(now);
  }
  // A stale result outside CadPending is ignored.
  m.cad_result(false, now);
  Action t = m.next(now);
  CHECK(t.kind == Kind::Transmit);
  m.cad_result(true, now);
  CHECK(m.busy() && m.tx_done(now + t.air_ms, nullptr));
}

}  // namespace

void run_mac() {
  airtime();
  duty_cycle();
  priority_queue();
  budget();
  cad_backoff();
}

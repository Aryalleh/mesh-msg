// The board's call manager (lib/meshcall call.*) end to end on the simulated
// network: two (or three) devices ring, answer, run the Noise handshake on
// the direct path and talk; plus every way a call ends, and the jitter buffer.
#include <cstring>
#include <memory>
#include <string>
#include <vector>

#include "call.h"
#include "check.h"
#include "netsim.h"

namespace mc = mesh::call;
using check::Bytes;
using namespace netsim;

namespace {

struct Lab;

struct Dev {
  Lab* lab = nullptr;
  mesh::Identity id;
  Host host;
  std::unique_ptr<mc::Manager> m;
  std::vector<std::string> log;  // state changes, "ringing", "ended:busy", ...
  bool allow = true;
  bool drop_signals = false;
  uint32_t clock_skew = 0;

  explicit Dev(uint8_t seed) : id(mesh::Identity::from_seed(seed64(seed))) {}
  static const uint8_t* seed64(uint8_t s) {
    static uint8_t b[64];
    for (int i = 0; i < 64; i++) b[i] = uint8_t(s * 31 + i);
    return b;
  }
  const mc::Info& info() const { return m->info(); }
  bool saw(const std::string& s) const {
    for (const auto& l : log)
      if (l == s) return true;
    return false;
  }
};

struct Sig {
  Dev* from;
  mesh::PublicKey to;
  Bytes b;
  uint32_t at;
};

struct Lab {
  World w;
  std::vector<Dev*> devs;
  std::vector<Sig> sigs;
  uint32_t sig_delay = 80;  // the mesh / relay delivering signals

  void add(Dev& d, const mc::Addr& ip, Nat* nat = nullptr) {
    d.lab = this;
    d.host.w = &w;
    d.host.ip = ip;
    d.host.nat = nat;
    w.hosts.push_back(&d.host);
    devs.push_back(&d);
    mc::Env env;
    env.self = &d.id;
    env.net = &d.host;
    for (size_t i = 0; i < w.reflectors.size() && i < 4; i++) env.direct.reflectors[i] = w.reflectors[i];
    env.direct.n_reflectors = w.reflectors.size() < 4 ? w.reflectors.size() : 4;
    env.ctx = &d;
    env.send = [](void* ctx, const mesh::PublicKey& peer, const uint8_t* s, size_t n) {
      Dev* me = static_cast<Dev*>(ctx);
      if (!me->drop_signals) me->lab->sigs.push_back({me, peer, Bytes(s, s + n), me->lab->w.now + me->lab->sig_delay});
      return true;
    };
    env.allowed = [](void* ctx, const mesh::PublicKey&) { return static_cast<Dev*>(ctx)->allow; };
    env.event = [](void* ctx) {
      Dev* me = static_cast<Dev*>(ctx);
      const mc::Info& in = me->m->info();
      std::string s = mc::state_name(in.state);
      if (in.state == mc::State::Ended) s += std::string(":") + in.reason;
      if (me->log.empty() || me->log.back() != s) me->log.push_back(s);
    };
    env.unix_now = [](void* ctx) {
      Dev* me = static_cast<Dev*>(ctx);
      return 1800000000u + me->lab->w.now / 1000 + me->clock_skew;
    };
    env.random = rnd;
    d.m.reset(new mc::Manager(env));
    Dev* dp = &d;
    d.host.rx = [dp](const mc::Addr& from, const uint8_t* b, size_t n, uint32_t now) {
      dp->m->on_datagram(from, b, n, now);
    };
  }

  // Runs ms of time; each 20 ms, per() runs (audio in and out).
  void run(uint32_t ms, const std::function<void(uint32_t)>& per = nullptr) {
    w.step(ms, [&](uint32_t now) {
      std::vector<Sig> due;
      for (size_t i = 0; i < sigs.size();) {
        if (int32_t(now - sigs[i].at) >= 0) {
          due.push_back(sigs[i]);
          sigs.erase(sigs.begin() + long(i));
        } else {
          i++;
        }
      }
      for (const Sig& s : due)
        for (Dev* d : devs)
          if (std::memcmp(d->id.pub().x, s.to.x, 32) == 0)
            d->m->on_signal(s.from->id.pub(), s.b.data(), s.b.size(), now);
      for (Dev* d : devs) d->m->tick(now);
      if (per && now % 20 == 0) per(now);
    });
  }

  // Runs until cond holds (or limit ms pass).
  bool until(const std::function<bool()>& cond, uint32_t limit) {
    for (uint32_t t = 0; t < limit; t += 20) {
      if (cond()) return true;
      run(20);
    }
    return cond();
  }
};

mc::Caps voice(std::initializer_list<uint8_t> codecs, bool video = false) {
  mc::Caps c;
  c.n = 1;
  c.streams[0].id = 0;
  c.streams[0].media = mc::kMediaAudio;
  for (uint8_t k : codecs) c.streams[0].codecs[c.streams[0].n_codecs++] = k;
  c.streams[0].max_kbps = 24;
  if (video) {
    c.n = 2;
    c.streams[1].id = 1;
    c.streams[1].media = mc::kMediaVideo;
    c.streams[1].codecs[0] = mc::kCodecH264;
    c.streams[1].n_codecs = 1;
  }
  return c;
}

bool active(const Dev& d) { return d.info().state == mc::State::Active; }
bool ended(const Dev& d) { return d.info().state == mc::State::Ended; }

// Rings b from a, answers, and waits for both sides to be active.
bool connect(Lab& lab, Dev& a, Dev& b, uint32_t limit = 15000) {
  CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus, mc::kCodecAmrWb}), lab.w.now));
  if (!lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000)) return false;
  CHECK(b.m->answer(voice({mc::kCodecAmrWb}), lab.w.now));
  return lab.until([&] { return active(a) && active(b); }, limit);
}

// 1 s of audio both ways; returns frames each side played in order.
std::pair<int, int> talk(Lab& lab, Dev& a, Dev& b) {
  int got_a = 0, got_b = 0;
  uint32_t n = 0, want_a = 0, want_b = 0;
  lab.run(1000, [&](uint32_t now) {
    uint8_t f[8] = {'a', 0, 0, 0, 0};
    std::memcpy(f + 1, &n, 4);
    a.m->send_audio(f, 5, now);
    f[0] = 'b';
    b.m->send_audio(f, 5, now);
    n++;
    uint8_t out[mc::kMaxAudio];
    size_t k;
    if (b.m->pull_audio(out, sizeof out, &k) == mc::Jitter::kFrame && k == 5 && out[0] == 'a') {
      uint32_t v;
      std::memcpy(&v, out + 1, 4);
      if (got_b == 0 || v == want_b) got_b++;
      want_b = v + 1;
    }
    if (a.m->pull_audio(out, sizeof out, &k) == mc::Jitter::kFrame && k == 5 && out[0] == 'b') {
      uint32_t v;
      std::memcpy(&v, out + 1, 4);
      if (got_a == 0 || v == want_a) got_a++;
      want_a = v + 1;
    }
  });
  return {got_a, got_b};
}

void ring_answer_talk_hangup() {
  Lab lab;
  Dev a(1), b(2);
  lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
  lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
  CHECK(connect(lab, a, b));
  CHECK(a.saw("outgoing") && b.saw("ringing") && a.saw("connecting") && b.saw("connecting"));
  CHECK(a.info().codec == mc::kCodecAmrWb && b.info().codec == mc::kCodecAmrWb);  // the caller's first that b has
  CHECK(a.info().code[0] && std::strcmp(a.info().code, b.info().code) == 0);
  CHECK(a.info().path.cls == 1 && b.info().path.cls == 1);
  CHECK(b.info().incoming && !a.info().incoming);
  auto got = talk(lab, a, b);
  CHECKC(got.first >= 40 && got.second >= 40, std::to_string(got.first) + "/" + std::to_string(got.second));
  CHECK(b.m->jitter().stats().lost == 0 && b.m->jitter().stats().late == 0);
  // Mute shows on the other side.
  a.m->set_muted(true);
  lab.run(200);
  CHECK(b.info().peer_muted);
  a.m->set_muted(false);
  lab.run(200);
  CHECK(!b.info().peer_muted);
  // Quiet for 10 s: keepalives hold the call.
  lab.run(10000);
  CHECK(active(a) && active(b));
  a.m->hangup(lab.w.now);
  CHECK(lab.until([&] { return ended(b); }, 1000));
  CHECK(a.saw("ended:hung up") && b.saw("ended:hung up"));
  CHECK(!a.m->send_audio((const uint8_t*)"x", 1, lab.w.now));
  // A new call after the last one ended.
  CHECK(connect(lab, b, a));
}

void across_nats() {
  Lab lab;
  lab.w.reflectors = {mc::Addr::v4(203, 0, 113, 50, 3478), mc::Addr::v4(203, 0, 113, 50, 3479)};
  Nat na, nb;
  na.pub = mc::Addr::v4(198, 51, 100, 1, 0);
  nb.pub = mc::Addr::v4(203, 0, 113, 7, 0);
  Dev a(3), b(4);
  lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0), &na);
  lab.add(b, mc::Addr::v4(10, 0, 0, 5, 0), &nb);
  CHECK(connect(lab, a, b));
  CHECK(a.info().path.cls == 4 && b.info().path.cls == 4);  // hole punched
  auto got = talk(lab, a, b);
  CHECK(got.first >= 40 && got.second >= 40);
  CHECK(std::strcmp(a.info().code, b.info().code) == 0);
}

void symmetric_nat() {
  Lab lab;
  lab.w.reflectors = {mc::Addr::v4(203, 0, 113, 50, 3478), mc::Addr::v4(203, 0, 113, 50, 3479)};
  Nat na, nb;
  na.pub = mc::Addr::v4(198, 51, 100, 1, 0);
  nb.pub = mc::Addr::v4(203, 0, 113, 7, 0);
  nb.symmetric = true;
  nb.port_step = 2;
  Dev a(5), b(6);
  lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0), &na);
  lab.add(b, mc::Addr::v4(10, 0, 0, 5, 0), &nb);
  CHECK(connect(lab, a, b, 30000));
  auto got = talk(lab, a, b);
  CHECK(got.first >= 40 && got.second >= 40);
}

void endings() {
  {  // declined
    Lab lab;
    Dev a(7), b(8);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000));
    CHECK(!a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));  // busy here
    b.m->hangup(lab.w.now);
    CHECK(lab.until([&] { return ended(a); }, 1000));
    CHECK(a.saw("ended:declined") && b.saw("ended:declined"));
  }
  {  // cancelled by the caller: missed
    Lab lab;
    Dev a(9), b(10);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000));
    a.m->hangup(lab.w.now);
    CHECK(lab.until([&] { return ended(b); }, 1000));
    CHECK(b.info().missed && b.saw("ended:missed"));
  }
  {  // nobody answers
    Lab lab;
    Dev a(11), b(12);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    lab.run(46000);
    CHECK(a.saw("ended:no answer") && b.saw("ended:missed") && b.info().missed);
  }
  {  // no common codec
    Lab lab;
    Dev a(13), b(14);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000));
    CHECK(!b.m->answer(voice({mc::kCodecAmrWb}), lab.w.now));
    CHECK(lab.until([&] { return ended(a); }, 1000));
    CHECK(b.saw("ended:no common codec") && a.saw("ended:declined"));
  }
  {  // not a contact
    Lab lab;
    Dev a(15), b(16);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    b.allow = false;
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return ended(a); }, 3000));
    CHECK(a.saw("ended:not accepted") && !b.saw("ringing"));
  }
  {  // a stored OFFER delivered after it expired: missed, never rings
    Lab lab;
    Dev a(17), b(18);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    b.clock_skew = 100;
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    lab.run(2000);
    CHECK(b.info().missed && b.saw("ended:missed") && !b.saw("ringing"));
  }
  {  // busy: a third device calls during a call
    Lab lab;
    Dev a(19), b(20), c(21);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    lab.add(c, mc::Addr::v4(192, 168, 1, 12, 0));
    CHECK(connect(lab, a, b));
    CHECK(c.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return ended(c); }, 3000));
    CHECK(c.saw("ended:busy") && active(a) && active(b));
  }
  {  // the other side goes silent: connection lost after 20 s
    Lab lab;
    Dev a(22), b(23);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    CHECK(connect(lab, a, b));
    b.host.udp_blocked = true;
    lab.run(19000);
    CHECK(active(a));
    CHECK(lab.until([&] { return ended(a); }, 3000));
    CHECK(a.saw("ended:connection lost"));
    CHECK(lab.until([&] { return ended(b); }, 1000));  // told by END
  }
  {  // no path at all
    Lab lab;
    Dev a(24), b(25);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    a.host.udp_blocked = true;
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000));
    CHECK(b.m->answer(voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return ended(a) && ended(b); }, 35000));
    CHECK(a.saw("ended:no direct path") && b.saw("ended:no direct path"));
  }
  {  // a video call answered as voice; repeats and strangers' signals are ignored
    Lab lab;
    Dev a(26), b(27), c(28);
    lab.add(a, mc::Addr::v4(192, 168, 1, 10, 0));
    lab.add(b, mc::Addr::v4(192, 168, 1, 11, 0));
    lab.add(c, mc::Addr::v4(192, 168, 1, 12, 0));
    CHECK(a.m->start(b.id.pub(), voice({mc::kCodecOpus}, true), lab.w.now));
    CHECK(lab.until([&] { return b.info().state == mc::State::Ringing; }, 3000));
    CHECK(b.info().video_offered);
    // c replays an END for this call: not from the peer, ignored.
    mc::Signal end;
    end.op = mc::Op::End;
    uint8_t raw[mc::kMaxSignal];
    // the call id is only known to a and b; c guesses wrong, and even the right id from c is ignored
    size_t n = mc::encode_signal(end, raw, sizeof raw);
    b.m->on_signal(c.id.pub(), raw, n, lab.w.now);
    CHECK(b.info().state == mc::State::Ringing);
    CHECK(b.m->answer(voice({mc::kCodecOpus}), lab.w.now));
    CHECK(lab.until([&] { return active(a) && active(b); }, 5000));
    CHECK(talk(lab, a, b).second >= 40);
  }
}

void jitter() {
  mc::Jitter j;
  uint8_t out[mc::kMaxAudio];
  size_t n;
  uint8_t f[1];
  // Waits for the 60 ms start target; steady arrivals bring it down to 40 ms.
  f[0] = 1;
  j.push(1, 20, f, 1, 20);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kEmpty);
  f[0] = 2;
  j.push(2, 40, f, 1, 40);
  CHECK(j.delay_ms() == 40);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && n == 1 && out[0] == 1);
  f[0] = 3;
  j.push(3, 60, f, 1, 60);
  // Out of order: 5 before 4.
  f[0] = 5;
  j.push(5, 100, f, 1, 100);
  f[0] = 4;
  j.push(4, 80, f, 1, 101);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 2);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 3);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 4);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 5);
  // A gap: 6 lost, 7 there → conceal, then 7.
  f[0] = 7;
  j.push(7, 140, f, 1, 140);
  f[0] = 8;
  j.push(8, 160, f, 1, 160);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kLost);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 7);
  CHECK(j.stats().lost == 1);
  // 6 arrives too late.
  f[0] = 6;
  j.push(6, 120, f, 1, 170);
  CHECK(j.stats().late == 1);
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kFrame && out[0] == 8);
  // Empty: underrun, rebuffers.
  CHECK(j.pull(out, sizeof out, &n) == mc::Jitter::kEmpty && j.stats().underruns == 1);
  // Arrival jitter raises the target delay (max 200 ms).
  mc::Jitter k;
  CHECK(k.delay_ms() == 60);
  for (uint32_t s = 0; s < 200; s++) k.push(s, s * 20, f, 1, s * 20 + (s % 2 ? 90 : 0));
  CHECKC(k.delay_ms() > 100 && k.delay_ms() <= 200, std::to_string(k.delay_ms()));
  mc::Jitter e;
  for (uint32_t s = 0; s < 200; s++) e.push(s, s * 20, f, 1, s * 20);
  CHECK(e.delay_ms() == 40);  // steady arrivals: the floor
}

}  // namespace

void run_callmgr() {
  jitter();
  ring_answer_talk_hangup();
  across_nats();
  symmetric_nat();
  endings();
}

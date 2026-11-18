#include "call.h"

#include <cstring>
#include <new>

namespace mesh {
namespace call {
namespace {

constexpr uint8_t kHello1 = 1, kHello2 = 2, kFrameType = 3;
constexpr uint32_t kConnectMs = 30000;  // path checks (10 s) + symmetric-NAT methods
constexpr uint32_t kHandshakeMs = 10000, kHelloEvery = 250, kKeepEvery = 2000, kSilentEnd = 20000;

bool after(uint32_t now, uint32_t t) { return int32_t(now - t) >= 0; }
uint32_t get32(const uint8_t* p) { return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3]; }
void put32(uint8_t* p, uint32_t v) {
  p[0] = uint8_t(v >> 24), p[1] = uint8_t(v >> 16), p[2] = uint8_t(v >> 8), p[3] = uint8_t(v);
}

}  // namespace

// ---- jitter buffer ----

void Jitter::reset() { *this = Jitter(); }

Jitter::Slot* Jitter::find(uint32_t seq) {
  for (auto& s : slots_)
    if (s.used && s.seq == seq) return &s;
  return nullptr;
}

size_t Jitter::count() const {
  size_t n = 0;
  for (const auto& s : slots_) n += s.used;
  return n;
}

void Jitter::push(uint32_t seq, uint32_t ts, const uint8_t* p, size_t n, uint32_t now) {
  if (n == 0 || n > kMaxAudio) return;
  if (!started_) started_ = true, next_ = seq;
  if (int32_t(seq - next_) < 0) {
    stats_.late++;
    return;
  }
  if (find(seq)) return;
  Slot* free = nullptr;
  for (auto& s : slots_)
    if (!s.used) {
      free = &s;
      break;
    }
  if (!free) {  // far behind: start over from this frame
    for (auto& s : slots_) s.used = false;
    next_ = seq;
    playing_ = false;
    free = &slots_[0];
  }
  free->used = true;
  free->seq = seq;
  free->len = uint8_t(n);
  std::memcpy(free->d, p, n);
  stats_.received++;
  if (have_last_) {
    float d = float(int32_t(now - last_at_)) - float(int32_t(ts - last_ts_));
    if (d < 0) d = -d;
    jitter_ += (d - jitter_) / 16;
  }
  last_ts_ = ts, last_at_ = now, have_last_ = true;
  float want = 2 * jitter_ + kPeriod;
  if (want < 40) want = 40;
  if (want > 200) want = 200;
  target_ = int((uint32_t(want) + kPeriod - 1) / kPeriod);
  if (target_ < 1) target_ = 1;
}

int Jitter::pull(uint8_t* out, size_t cap, size_t* n) {
  *n = 0;
  size_t queued = count();
  if (!playing_) {
    if (queued < size_t(target_)) return kEmpty;
    playing_ = true;
  }
  // Far more queued than the target (a burst after a stall): skip one.
  if (queued > size_t(target_) + 10) {
    if (Slot* s = find(next_)) {
      s->used = false;
      stats_.dropped++;
      queued--;
    }
    next_++;
  }
  if (Slot* s = find(next_)) {
    size_t k = s->len < cap ? s->len : cap;
    std::memcpy(out, s->d, k);
    *n = k;
    s->used = false;
    next_++;
    return kFrame;
  }
  if (queued == 0) {  // underrun: rebuffer up to the target
    playing_ = false;
    stats_.underruns++;
    return kEmpty;
  }
  next_++;
  stats_.lost++;
  return kLost;
}

// ---- manager ----

const char* state_name(State s) {
  static const char* n[] = {"idle", "outgoing", "ringing", "connecting", "active", "ended"};
  return n[uint8_t(s) < 6 ? uint8_t(s) : 5];
}

Manager::~Manager() {
  if (agent_) agent_->~Agent();
  if (noise_) noise_->~NoiseKK();
}

void Manager::notify() {
  if (env_.event) env_.event(env_.ctx);
}

void Manager::set_state(State s, uint32_t now) {
  if (info_.state == State::Ended) return;
  info_.state = s;
  if (s == State::Active) info_.active_ms = now;
  notify();
}

void Manager::send_signal(Signal& s) {
  std::memcpy(s.call_id, id_, 16);
  uint8_t b[kMaxSignal];
  size_t n = encode_signal(s, b, sizeof b);
  if (n && env_.send) env_.send(env_.ctx, info_.peer, b, n);
}

void Manager::make_agent(bool controlling, uint32_t now) {
  if (agent_) agent_->~Agent();
  agent_ = new (agent_mem_) Agent(*env_.net, env_.direct, id_, key_, controlling, env_.random, now);
  agent_->set_receiver(link_rx, this);
}

void Manager::end(const char* reason, bool tell, uint32_t now) {
  (void)now;
  if (info_.state == State::Ended || info_.state == State::Idle) return;
  State prev = info_.state;
  info_.state = State::Ended;
  info_.reason = reason;
  if (tell) {
    Signal s;
    if (prev == State::Outgoing) {
      s.op = Op::Cancel;
    } else if (prev == State::Ringing) {
      s.op = Op::Reject;
      s.reason = kRejectDeclined;
    } else {
      s.op = Op::End;
      s.reason = std::strcmp(reason, "no direct path") == 0 ? kEndNoPath
                 : std::strcmp(reason, "timed out") == 0    ? kEndTimeout
                 : std::strcmp(reason, "error") == 0        ? kEndError
                                                            : kEndHangup;
    }
    send_signal(s);
  }
  if (agent_) {
    agent_->~Agent();
    agent_ = nullptr;
  }
  if (noise_) {
    noise_->~NoiseKK();
    noise_ = nullptr;
  }
  notify();
}

bool Manager::start(const PublicKey& peer, const Caps& caps, uint32_t now) {
  if (live() || !env_.net || !env_.self) return false;
  info_ = Info();
  info_.peer = peer;
  info_.incoming = false;
  info_.state = State::Outgoing;
  env_.random(id_, sizeof id_);
  env_.random(key_, sizeof key_);
  ours_ = caps;
  offer_sent_ = answered_ = answer_sent_ = false;
  start_ms_ = now;
  jitter_.reset();
  seq_ = 0;
  make_agent(true, now);
  notify();
  tick(now);
  return true;
}

bool Manager::answer(const Caps& caps, uint32_t now) {
  if (info_.state != State::Ringing) return false;
  const StreamCap* theirs = offer_.caps.audio();
  const StreamCap* mine = caps.audio();
  uint8_t codec = theirs && mine ? pick_codec(theirs->codecs, theirs->n_codecs, mine->codecs, mine->n_codecs) : 0;
  if (!codec) {
    end("no common codec", true, now);
    return false;
  }
  info_.codec = codec;
  ours_ = Caps();
  ours_.n = 1;
  ours_.streams[0] = *mine;
  ours_.streams[0].codecs[0] = codec;
  ours_.streams[0].n_codecs = 1;
  // Video: none yet on the board (MJPEG comes next); the call runs as voice.
  answered_ = true;
  answer_sent_ = false;
  jitter_.reset();
  seq_ = 0;
  make_agent(false, now);
  set_state(State::Connecting, now);
  connect_end_ = now + kConnectMs;
  tick(now);
  return true;
}

void Manager::hangup(uint32_t now) {
  end(info_.state == State::Ringing ? "declined" : "hung up", true, now);
}

void Manager::set_muted(bool muted) {
  if (!live()) return;
  Signal s;
  s.op = Op::Control;
  s.stream = 0;
  s.kind = 2;
  s.body[0] = muted ? 1 : 0;
  s.body_n = 1;
  send_signal(s);
}

void Manager::on_signal(const PublicKey& from, const uint8_t* b, size_t n, uint32_t now) {
  Signal s;
  if (decode_signal(b, n, s) != Decoded::Ok) return;  // unknown ops and broken signals are ignored
  bool ours = std::memcmp(s.call_id, id_, 16) == 0 && info_.state != State::Idle &&
              std::memcmp(from.x, info_.peer.x, 32) == 0 && std::memcmp(from.ed, info_.peer.ed, 32) == 0;
  if (s.op == Op::Offer) {
    if (ours) return;  // a repeat
    if (live()) {      // busy: tell the caller, keep our call
      uint8_t out[32];
      Signal r;
      std::memcpy(r.call_id, s.call_id, 16);
      r.op = Op::Reject;
      r.reason = kRejectBusy;
      size_t k = encode_signal(r, out, sizeof out);
      if (k && env_.send) env_.send(env_.ctx, from, out, k);
      return;
    }
    info_ = Info();
    info_.peer = from;
    info_.incoming = true;
    std::memcpy(id_, s.call_id, 16);
    std::memcpy(key_, s.key, 32);
    offer_ = s;
    info_.video_offered = s.caps.video() != nullptr;
    start_ms_ = now;
    answered_ = false;
    if (env_.allowed && !env_.allowed(env_.ctx, from)) {
      info_.state = State::Ended;
      info_.reason = "not a contact";
      Signal r;
      r.op = Op::Reject;
      r.reason = kRejectNotContact;
      send_signal(r);
      return;
    }
    uint32_t unix_now = env_.unix_now ? env_.unix_now(env_.ctx) : 0;
    if (unix_now && s.expires < unix_now) {
      // Delivered late (a relay stored it): a missed call, never rings.
      info_.state = State::Ended;
      info_.reason = "missed";
      info_.missed = true;
      notify();
      return;
    }
    info_.state = State::Ringing;
    notify();
    return;
  }
  if (!ours) return;
  switch (s.op) {
    case Op::Answer: {
      if (info_.incoming || info_.state != State::Outgoing) return;
      const StreamCap* a = s.caps.audio();
      if (!a || !a->n_codecs) {
        end("no common codec", true, now);
        return;
      }
      info_.codec = a->codecs[0];
      remote_offer_ = s.offer;
      set_state(State::Connecting, now);
      connect_end_ = now + kConnectMs;
      if (agent_ && agent_->state() == Agent::State::Gathered) agent_->connect(remote_offer_, now);
      return;
    }
    case Op::Reject:
      end(s.reason == kRejectBusy ? "busy" : s.reason == kRejectNotContact ? "not accepted" : "declined", false, now);
      return;
    case Op::Cancel:
      if (info_.state == State::Ringing) info_.missed = true;
      end("missed", false, now);
      return;
    case Op::End:
      end(s.reason == kEndHangup    ? "hung up"
          : s.reason == kEndNoPath  ? "no direct path"
          : s.reason == kEndTimeout ? "timed out"
          : s.reason == kEndError   ? "error"
                                    : "ended",
          false, now);
      return;
    case Op::Control:
      if (s.stream == 0 && s.kind == 2 && s.body_n > 0) {
        info_.peer_muted = s.body[0] != 0;
        notify();
      }
      return;
    default:
      return;
  }
}

void Manager::on_datagram(const Addr& from, const uint8_t* d, size_t n, uint32_t now) {
  now_ = now;
  if (agent_) agent_->on_datagram(from, d, n, now);
}

void Manager::start_handshake(uint32_t now) {
  if (noise_ || !agent_ || agent_->state() != Agent::State::Connected) return;
  uint8_t prologue[12 + 16];
  std::memcpy(prologue, "mava call v1", 12);
  std::memcpy(prologue + 12, id_, 16);
  uint8_t eph[32];
  env_.random(eph, sizeof eph);
  noise_ = new (noise_mem_) NoiseKK(!info_.incoming, *env_.self, info_.peer.x, prologue, sizeof prologue, eph);
  hello_n_ = 0;
  if (noise_->initiator()) {
    hello_[0] = kHello1;
    if (noise_->write(hello_ + 1)) hello_n_ = 1 + NoiseKK::kMsgSize;
    hello_next_ = now;
  }
  hello_end_ = now + kHandshakeMs;
}

void Manager::link_rx(void* ctx, const uint8_t* d, size_t n) { static_cast<Manager*>(ctx)->on_link(d, n); }

void Manager::on_link(const uint8_t* d, size_t n) {
  uint32_t now = now_;
  if (n && !noise_ && info_.state == State::Connecting) start_handshake(now);  // the path came up just now
  if (!n || !noise_ || !agent_) return;
  if (d[0] == kHello1 && !noise_->initiator()) {
    if (noise_->done()) {
      agent_->send(hello_, hello_n_);  // our answer was lost
      return;
    }
    if (!noise_->read(d + 1, n - 1)) return;
    hello_[0] = kHello2;
    if (!noise_->write(hello_ + 1)) return;
    hello_n_ = 1 + NoiseKK::kMsgSize;
    agent_->send(hello_, hello_n_);
    char c[8];
    call_code(noise_->handshake_hash(), c);
    std::memcpy(info_.code, c, sizeof c);
    info_.path = agent_->path();
    t0_ = last_rx_ = last_keep_ = now;
    set_state(State::Active, now);
    return;
  }
  if (d[0] == kHello2 && noise_->initiator() && !noise_->done()) {
    if (!noise_->read(d + 1, n - 1)) return;
    char c[8];
    call_code(noise_->handshake_hash(), c);
    std::memcpy(info_.code, c, sizeof c);
    info_.path = agent_->path();
    t0_ = last_rx_ = last_keep_ = now;
    set_state(State::Active, now);
    return;
  }
  if (d[0] != kFrameType || !noise_->done() || n < 1 + kFrameHead + kTag) return;
  uint8_t stream = d[1];
  uint32_t seq = get32(d + 2), ts = get32(d + 6);
  uint8_t p[kMaxPayload];
  size_t len = n - 1 - kFrameHead - kTag;
  if (!noise_->decrypt(uint64_t(stream) << 32 | seq, d + 1, kFrameHead, d + 1 + kFrameHead, n - 1 - kFrameHead, p))
    return;
  last_rx_ = now;
  if (stream == kStreamAudio && len > 0) jitter_.push(seq, ts, p, len, now);
  // Video (stream 1) and its control (255) are not taken yet: the call is voice.
}

void Manager::send_frame(uint8_t stream, const uint8_t* p, size_t n, uint32_t now) {
  if (!agent_ || !noise_ || !noise_->done()) return;
  uint8_t b[1 + kFrameHead + kMaxAudio + kTag];
  if (n > kMaxAudio) return;
  b[0] = kFrameType;
  b[1] = stream;
  uint32_t seq = ++seq_;
  put32(b + 2, seq);
  put32(b + 6, now - t0_);
  b[10] = 0;
  noise_->encrypt(uint64_t(stream) << 32 | seq, b + 1, kFrameHead, p, n, b + 1 + kFrameHead);
  agent_->send(b, 1 + kFrameHead + n + kTag);
}

bool Manager::send_audio(const uint8_t* p, size_t n, uint32_t now) {
  if (info_.state != State::Active || n == 0 || n > kMaxAudio) return false;
  send_frame(kStreamAudio, p, n, now);
  return true;
}

void Manager::tick(uint32_t now) {
  now_ = now;
  if (!live()) return;
  if (agent_) agent_->tick(now);
  switch (info_.state) {
    case State::Outgoing:
      if (agent_ && agent_->state() == Agent::State::Gathered && !offer_sent_) {
        Signal s;
        s.op = Op::Offer;
        uint32_t unix_now = env_.unix_now ? env_.unix_now(env_.ctx) : 0;
        s.expires = unix_now + kRingMs / 1000;
        s.caps = ours_;
        std::memcpy(s.key, key_, 32);
        s.offer = agent_->offer();
        send_signal(s);
        offer_sent_ = true;
      }
      if (agent_ && agent_->state() == Agent::State::Failed) end("error", true, now);
      else if (after(now, start_ms_ + kRingMs)) end("no answer", true, now);
      return;
    case State::Ringing:
      if (after(now, start_ms_ + kRingMs)) {
        info_.missed = true;
        end("missed", false, now);
      }
      return;
    case State::Connecting: {
      if (!agent_) return;
      Agent::State as = agent_->state();
      if (info_.incoming && as == Agent::State::Gathered && !answer_sent_) {
        Signal s;
        s.op = Op::Answer;
        s.caps = ours_;
        s.offer = agent_->offer();
        send_signal(s);
        answer_sent_ = true;
        agent_->connect(offer_.offer, now);
        as = agent_->state();
      }
      if (!info_.incoming && as == Agent::State::Gathered && remote_offer_.n) {
        agent_->connect(remote_offer_, now);
        as = agent_->state();
      }
      if (as == Agent::State::Failed) {
        end("no direct path", true, now);
        return;
      }
      if (as == Agent::State::Connected) start_handshake(now);
      if (noise_ && noise_->initiator() && !noise_->done() && after(now, hello_next_) && hello_n_) {
        agent_->send(hello_, hello_n_);  // repeated until the answer comes
        hello_next_ = now + kHelloEvery;
      }
      if (noise_ && !noise_->done() && after(now, hello_end_)) {
        end("error", true, now);
        return;
      }
      if (after(now, connect_end_) && info_.state == State::Connecting) end("no direct path", true, now);
      return;
    }
    case State::Active:
      if (!agent_ || agent_->state() != Agent::State::Connected) {
        end("connection lost", false, now);
        return;
      }
      if (now - last_rx_ > kSilentEnd) {
        end("connection lost", true, now);
        return;
      }
      if (now - last_keep_ >= kKeepEvery) {  // keeps NAT mappings open in silence
        last_keep_ = now;
        send_frame(kStreamAudio, nullptr, 0, now);
      }
      return;
    default:
      return;
  }
}

}  // namespace call
}  // namespace mesh

// One-to-one calls on the board (docs/calls.md, port of core/call): signals
// in end-to-end encrypted messages (content type 7, sent by the owner), a
// direct path (direct.h), a Noise KK handshake on it, sealed media frames,
// keepalives, and an adaptive jitter buffer. One call at a time. Audio is
// opaque payloads here: the board encodes and decodes (Opus).
//
// Event driven, no threads, no heap: on_signal(), on_datagram(), tick();
// state changes go to Env::event. Pure C++, no Arduino: host-tested, and
// interoperates with core/call (tools/callinterop).
#pragma once

#include <cstddef>
#include <cstdint>

#include "callcrypto.h"
#include "direct.h"
#include "identity.h"
#include "signal.h"

namespace mesh {
namespace call {

constexpr uint8_t kStreamAudio = 0, kStreamVideo = 1, kStreamControl = 255;
constexpr size_t kFrameHead = 1 + 4 + 4 + 1;  // stream | seq | ts | flags
constexpr size_t kMaxAudio = 256;             // one encoded 20 ms frame

// Adaptive jitter buffer for 20 ms frames (core/call jitter.go): the target
// delay follows the RFC 3550 inter-arrival jitter between 40 and 200 ms.
class Jitter {
 public:
  enum Pull { kFrame = 0, kLost = 1, kEmpty = 2 };
  struct Stats {
    uint32_t received = 0, late = 0, lost = 0, underruns = 0, dropped = 0;
  };
  void reset();
  void push(uint32_t seq, uint32_t ts, const uint8_t* p, size_t n, uint32_t now_ms);
  // Next frame to play (every 20 ms): kFrame with its bytes, kLost (conceal), kEmpty (silence).
  int pull(uint8_t* out, size_t cap, size_t* n);
  uint32_t delay_ms() const { return uint32_t(target_) * kPeriod; }
  const Stats& stats() const { return stats_; }

 private:
  static constexpr uint32_t kPeriod = 20;
  static constexpr size_t kSlots = 32;
  struct Slot {
    bool used = false;
    uint32_t seq = 0;
    uint8_t len = 0;
    uint8_t d[kMaxAudio];
  };
  Slot* find(uint32_t seq);
  size_t count() const;
  Slot slots_[kSlots];
  uint32_t next_ = 0;
  bool started_ = false, playing_ = false;
  int target_ = 3;
  float jitter_ = 0;
  uint32_t last_ts_ = 0, last_at_ = 0;
  bool have_last_ = false;
  Stats stats_;
};

enum class State : uint8_t { Idle, Outgoing, Ringing, Connecting, Active, Ended };
const char* state_name(State s);

struct Info {
  State state = State::Idle;
  bool incoming = false;
  PublicKey peer;
  uint8_t codec = 0;
  char code[8] = {};  // compare by voice
  Path path;          // once active
  const char* reason = "";
  bool missed = false;
  bool peer_muted = false;
  bool video_offered = false;  // the caller offered video (the board answers voice only for now)
  uint32_t active_ms = 0;      // when it became active
};

struct Env {
  const Identity* self = nullptr;
  Net* net = nullptr;
  AgentConfig direct;
  void* ctx = nullptr;
  // Delivers a signal to the peer (content type 7, any path, priority).
  bool (*send)(void* ctx, const PublicKey& peer, const uint8_t* sig, size_t n) = nullptr;
  // May peer call us? (null: anyone)
  bool (*allowed)(void* ctx, const PublicKey& peer) = nullptr;
  // The state changed (read Manager::info()).
  void (*event)(void* ctx) = nullptr;
  uint32_t (*unix_now)(void* ctx) = nullptr;
  void (*random)(uint8_t* out, size_t n) = nullptr;
};

class Manager {
 public:
  explicit Manager(const Env& env) : env_(env) {}
  ~Manager();
  Manager(const Manager&) = delete;
  Manager& operator=(const Manager&) = delete;

  // Calls peer with caps (stream 0 audio, codecs best first). False: busy.
  bool start(const PublicKey& peer, const Caps& caps, uint32_t now_ms);
  // Accepts the ringing call with our caps (codec: the caller's first we have).
  bool answer(const Caps& caps, uint32_t now_ms);
  // Hangs up, cancels or declines.
  void hangup(uint32_t now_ms);
  void set_muted(bool muted);

  void on_signal(const PublicKey& from, const uint8_t* b, size_t n, uint32_t now_ms);
  void on_datagram(const Addr& from, const uint8_t* d, size_t n, uint32_t now_ms);
  void tick(uint32_t now_ms);
  // The call's UDP socket (-1: none): the owner reads it and calls on_datagram.
  int socket() const { return agent_ ? agent_->socket() : -1; }

  // One encoded 20 ms frame while active.
  bool send_audio(const uint8_t* p, size_t n, uint32_t now_ms);
  // Every 20 ms: Jitter::kFrame / kLost / kEmpty.
  int pull_audio(uint8_t* out, size_t cap, size_t* n) { return jitter_.pull(out, cap, n); }
  const Jitter& jitter() const { return jitter_; }

  const Info& info() const { return info_; }
  bool live() const { return info_.state != State::Idle && info_.state != State::Ended; }
  static constexpr uint32_t kRingMs = 45000;

 private:
  void set_state(State s, uint32_t now);
  void end(const char* reason, bool tell, uint32_t now);
  void send_signal(Signal& s);
  void make_agent(bool controlling, uint32_t now);
  void start_handshake(uint32_t now);
  void on_link(const uint8_t* d, size_t n);
  static void link_rx(void* ctx, const uint8_t* d, size_t n);
  void send_frame(uint8_t stream, const uint8_t* p, size_t n, uint32_t now);
  void notify();

  Env env_;
  Info info_;
  uint8_t id_[16] = {};
  uint8_t key_[32] = {};
  Caps ours_;
  Signal offer_;  // incoming: the caller's OFFER
  bool offer_sent_ = false, answer_sent_ = false, answered_ = false;
  Offer remote_offer_;
  uint32_t start_ms_ = 0, connect_end_ = 0, hello_next_ = 0, hello_end_ = 0;
  // agent and handshake (placement storage: no heap)
  alignas(Agent) uint8_t agent_mem_[sizeof(Agent)];
  Agent* agent_ = nullptr;
  alignas(NoiseKK) uint8_t noise_mem_[sizeof(NoiseKK)];
  NoiseKK* noise_ = nullptr;
  uint8_t hello_[1 + NoiseKK::kMsgSize] = {};
  size_t hello_n_ = 0;
  uint32_t now_ = 0;
  uint32_t t0_ = 0, last_rx_ = 0, last_keep_ = 0, seq_ = 0;
  Jitter jitter_;
};

}  // namespace call
}  // namespace mesh

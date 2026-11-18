// Call signaling (docs/calls.md §2, core/call/signal.go) and the direct
// path's offer (core/direct/candidate.go): what two devices exchange in
// end-to-end encrypted messages (content type 7) to set up a call.
// Unknown ops, media kinds, codecs and trailing bytes are ignored, never an
// error. Fixed-size, no heap: extra candidates, streams, codecs and params
// beyond the limits below are skipped while decoding.
// Pure C++, no Arduino: host-tested against spec/test-vectors/calls.json.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace call {

// ---- addresses and candidates (core/direct) ----

struct Addr {
  uint8_t family = 0;  // 0 none, 4, 6
  uint8_t ip[16] = {};
  uint16_t port = 0;

  bool valid() const { return family == 4 || family == 6; }
  bool is4() const { return family == 4; }
  bool same(const Addr& o) const;  // address and port
  bool same_ip(const Addr& o) const;
  bool loopback() const;
  static Addr v4(uint8_t a, uint8_t b, uint8_t c, uint8_t d, uint16_t port);
  // "1.2.3.4:5" / "[::1]:5", for logs.
  void str(char* out, size_t cap) const;
};

enum class Kind : uint8_t { Host = 0, Reflexive = 1, PeerRefl = 2, Local = 3, Mapped = 4, Predicted = 5, Relayed = 6 };
const char* kind_name(Kind k);

struct Candidate {
  Kind kind = Kind::Host;
  Addr addr;
  uint32_t priority = 0;
};

enum class NatType : uint8_t { Unknown = 0, Open = 1, Indep = 2, SymSeq = 3, SymRandom = 4, Blocked = 5 };
const char* nat_name(NatType t);

struct NatInfo {
  NatType type = NatType::Unknown;
  int16_t step = 0;
  Addr last;
};

constexpr size_t kMaxCandidates = 16;

struct Offer {
  NatInfo nat;
  Candidate cand[kMaxCandidates];
  size_t n = 0;
  void add(const Candidate& c);  // skips duplicates and beyond the limit
};

// A candidate's default priority (higher first), as core/direct.
uint32_t priority(Kind k, const Addr& a);
// connectivity.md §5 class of a succeeded pair: 1 local network … 6 relayed.
int pair_class(Kind local, Kind remote, const Addr& remote_addr);

// nat_type(1) | step(2) | last | n(1) | n × (kind(1) | addr | priority(4)),
// addr = family(1) | ip(0, 4, 16) | port(2). Returns the size, 0 if cap is short.
size_t encode_offer(const Offer& o, uint8_t* out, size_t cap);
// used: bytes consumed. False if short or malformed.
bool decode_offer(const uint8_t* b, size_t n, Offer& o, size_t* used);

// ---- signals (core/call) ----

// Ringing (8): the callee's device got the OFFER and rings; no body.
enum class Op : uint8_t {
  Offer = 0,
  Answer = 1,
  Candidates = 2,
  Reject = 3,
  End = 4,
  Cancel = 5,
  Update = 6,
  Control = 7,
  Ringing = 8
};

constexpr uint8_t kRejectDeclined = 0, kRejectBusy = 1, kRejectNotContact = 2;
constexpr uint8_t kEndHangup = 0, kEndNoPath = 1, kEndTimeout = 2, kEndError = 3;
constexpr uint8_t kMediaAudio = 0, kMediaVideo = 1;
constexpr uint8_t kDirSend = 1, kDirReceive = 2, kDirBoth = 3;
constexpr uint8_t kCodecOpus = 1, kCodecAmrWb = 2, kCodecVp8 = 16, kCodecH264 = 17;
// Motion JPEG: every frame a JPEG on its own (calls.md §7b), what a small
// device like the Mava board can decode.
constexpr uint8_t kCodecMjpeg = 20;
constexpr uint8_t kVersion = 1;

struct StreamCap {
  uint8_t id = 0, media = 0, dir = kDirBoth;
  uint8_t codecs[8] = {};
  uint8_t n_codecs = 0;
  uint16_t max_kbps = 0;
  uint8_t params[16] = {};
  uint8_t n_params = 0;
};

struct Caps {
  uint8_t version = kVersion;
  StreamCap streams[4];
  uint8_t n = 0;
  const StreamCap* audio() const;  // stream 0, audio
  const StreamCap* video() const;  // the first video stream
};

struct Signal {
  uint8_t call_id[16] = {};
  Op op = Op::Offer;
  uint32_t expires = 0;  // OFFER: unix seconds
  Caps caps;             // OFFER, ANSWER, UPDATE
  uint8_t key[32] = {};  // OFFER: the path checks' key
  Offer offer;           // OFFER, ANSWER, CANDIDATES
  uint8_t reason = 0;    // REJECT, END
  uint8_t stream = 0, kind = 0;  // CONTROL
  uint8_t body[32] = {};
  uint8_t body_n = 0;
};

// The largest encoded signal (an OFFER with every candidate as IPv6).
constexpr size_t kMaxSignal = 17 + 4 + 2 + 4 * (4 + 8 + 3 + 16) + 32 + 3 + 19 + 1 + kMaxCandidates * (1 + 19 + 4) + 64;

size_t encode_signal(const Signal& s, uint8_t* out, size_t cap);

enum class Decoded { Ok, Short, UnknownOp };
Decoded decode_signal(const uint8_t* b, size_t n, Signal& s);

// The first of pref that have also has (0: none): the callee's codec choice.
uint8_t pick_codec(const uint8_t* pref, size_t pn, const uint8_t* have, size_t hn);

}  // namespace call
}  // namespace mesh

// Voice calls on the board (docs/calls.md, "The board"): lib/meshcall's call
// manager in its own task with a UDP socket on Wi-Fi (or the hotspot),
// Opus at 16 kHz in 20 ms frames, signals as content type 7 through the
// board's messaging client (any path: Bluetooth, the relay, USB). Audio comes
// and goes in 10 ms frames through board_audio: the phone simulator over USB
// in pre-releases, the WM8960 codec later. One call at a time; voice only
// (a video offer is answered with voice).
#pragma once

#include <cstddef>
#include <cstdint>

#include "call.h"  // lib/meshcall
#include "identity.h"

namespace calls {

struct View {
  mesh::call::State state = mesh::call::State::Idle;
  bool incoming = false;
  mesh::PublicKey peer;
  char code[8] = {};  // compare by voice: if it matches, nobody is in the middle
  uint8_t codec = 0;
  int path_cls = 0;   // 1 same network … 5 across NAT (connectivity.md §5)
  char reason[32] = {};
  bool missed = false, peer_muted = false, muted = false, video_offered = false;
  uint32_t active_ms = 0;  // millis() when it became active
};

// Starts the call task; self is the user identity (the messaging client's).
bool begin(const mesh::Identity& self);

// ---- main loop ----
// A call signal from the client; contact: a saved contact (only they may call).
void on_signal(const mesh::PublicKey& from, bool contact, const uint8_t* sig, size_t n);
// Calls peer; false: already in a call.
bool start(const mesh::PublicKey& peer);
bool answer();
void hangup();  // also declines a ringing call
void set_muted(bool muted);
// Signals for the client to send, oldest first.
bool take_signal(mesh::PublicKey& to, uint8_t* out, size_t cap, size_t* n);
View view();
uint32_t revision();  // grows with every change
bool live();

// ---- audio (board_audio, main loop): 16 kHz mono, any frame size ----
void mic(const int16_t* pcm, size_t n);
// The far side's voice; false when there is none now (play silence).
bool speaker(int16_t* out, size_t n);

// For the log.
void status(char* out, size_t cap);

}  // namespace calls

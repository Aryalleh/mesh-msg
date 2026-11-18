// Pre-release hardware simulators (docs/screen.md): over the host link the
// phone (Mava Screen) stands in for the handheld's
//   screen      SCREEN: the display mirrored to the phone (lib/hwsim mirror.h)
//   keys        INPUT: touches, the handheld's keys, typing from the phone's keyboard
//   audio       AUDIO: the phone's speaker plays the board's sounds, its
//               microphone feeds the board (lib/hwsim audiolink.h)
// Built with MAVA_HWSIM=1 (env mava_handheld_s3, every pre-release). The v1
// build (env mava_handheld_s3_v1, MAVA_HWSIM=0) compiles none of it: the
// stubs below answer these ops "unknown" and the board runs on its own
// screen, keyboard and codec.
#pragma once

#include <cstddef>
#include <cstdint>

#include "hostlink.h"
#include "node.h"

#ifndef MAVA_HWSIM
#define MAVA_HWSIM 0
#endif

namespace hwsim {

#if MAVA_HWSIM

// Allocates the frame copy in PSRAM; false without PSRAM (no screen mirror then).
bool begin();
// A flushed area from the display driver (LVGL's RGB565, before the panel's byte swap).
void blit(int x1, int y1, int x2, int y2, const uint16_t* px);
// From Platform::board_command (main loop, node lock held).
uint8_t command(mesh::node::Node& node, size_t port, uint8_t op, const uint8_t* args, size_t n);
// Sends changed tiles and speaker frames while the subscribed port has room
// (main loop, node lock held).
void pump(mesh::node::Node& node);
// A host uses a simulator: keep its USB port attached.
bool active();

#else

inline bool begin() { return false; }
inline void blit(int, int, int, int, const uint16_t*) {}
inline uint8_t command(mesh::node::Node&, size_t, uint8_t, const uint8_t*, size_t) { return mesh::host::kAckUnknownOp; }
inline void pump(mesh::node::Node&) {}
inline bool active() { return false; }

#endif

}  // namespace hwsim

// The handheld's screen on a phone or PC over the host link (docs/screen.md):
// SCREEN starts / stops the mirror on a host port, INPUT feeds touches and
// keys to the UI. The frame copy lives in PSRAM (lib/meshui mirror.h).
#pragma once

#include <cstddef>
#include <cstdint>

#include "node.h"

namespace remote {

// Allocates the frame copy; false without PSRAM (no mirror then).
bool begin();
// A flushed area from the display driver (LVGL's RGB565, before the panel's byte swap).
void blit(int x1, int y1, int x2, int y2, const uint16_t* px);

// From Platform::board_command (main loop, node lock held).
uint8_t command(mesh::node::Node& node, size_t port, uint8_t op, const uint8_t* args, size_t n);
// Sends changed tiles while the subscribed port has room (main loop, node lock held).
void pump(mesh::node::Node& node);
bool active();

}  // namespace remote

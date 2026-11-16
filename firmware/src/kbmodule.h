// The keyboard module seen from the ESP32-S3 (docs/handheld-proto.md §3):
// an RP2040 at I2C 0x1F that reports key positions and trackpad motion
// (mesh::keys::kbproto). This side applies the layout (Persian / Latin,
// Alt, Shift, Sym) and hands text and actions to the UI.
#pragma once

#include <cstddef>
#include <cstdint>

#include "keys.h"

namespace kbmodule {

// Probes the module on the shared I2C bus (Wire must be started). False:
// no module (the plain Mava board, or not connected).
bool begin();
bool present();
mesh::keys::Variant variant();
mesh::keys::Layout& layout();

// Reads what is pending (when INT is low, or every 50 ms as a fallback):
// writes up to cap outputs, adds trackpad motion to *dx / *dy. Returns the
// number of outputs.
size_t poll(uint32_t now_ms, mesh::keys::Output* out, size_t cap, int* dx, int* dy);
void backlight(uint8_t level);

}  // namespace kbmodule

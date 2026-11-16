// Board glue for the Mava handheld (docs/board.md, docs/handheld-proto.md):
// ESP32-S3-N16R8, SX1262, ILI9341, WM8960 and one of three input sets:
// 7 × 7 key matrix + touch (A), BlackBerry 9900 keyboard + touch (B),
// BlackBerry 9900 keyboard + trackpad (C). Pins: build flags in platformio.ini.
#pragma once

#include <cstddef>
#include <cstdint>

#include "keys.h"

namespace board {

// Monotonic milliseconds that do not wrap after 49 days.
uint64_t now_ms();

// Hardware random number (RNG peripheral); never a PRNG.
uint32_t random32();
void random_bytes(uint8_t* out, size_t n);

void init();  // chip selects, strapping pins, keyboard matrix
void led(bool on);
// The keyboard this build is wired for (version A: 7 × 7 matrix; B and C:
// BlackBerry 9900), and one scan of it: out[col] bit row = pressed.
const mesh::keys::Keyboard& keyboard();
void scan_keys(uint8_t out[]);
// Keyboard (and trackpad) backlight, 0–255; no-op without one.
void keyboard_backlight(uint8_t level);
uint16_t battery_mv();  // 0 = unknown / mains

// The physical setup switch (docs/board.md §2): on = hotspot + setup panel,
// off = join the saved Wi-Fi. A position of the keyboard matrix on the
// handheld (docs/handheld-proto.md §4). Debounced by the caller.
bool setup_switch_on();

void reboot();

// Persistent files (LittleFS). read returns bytes read (0 = missing / error);
// write replaces the file; format erases everything.
bool storage_begin();
size_t storage_read(const char* path, void* buf, size_t cap);
bool storage_write(const char* path, const void* buf, size_t n);
bool storage_remove(const char* path);
bool storage_format();

}  // namespace board

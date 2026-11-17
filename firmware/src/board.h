// Board glue for the Mava handheld (docs/board.md, docs/handheld-proto.md):
// ESP32-S3-N16R8, SX1262, ILI9341 + touch, WM8960, and the keyboard module
// (RP2040, firmware/keyboard) on I2C. Pins: build flags in platformio.ini.
#pragma once

#include <cstddef>
#include <cstdint>


namespace board {

// Monotonic milliseconds that do not wrap after 49 days.
uint64_t now_ms();

// Hardware random number (RNG peripheral); never a PRNG.
uint32_t random32();
void random_bytes(uint8_t* out, size_t n);

void init();  // chip selects, strapping pins, switch and INT inputs
void led(bool on);
uint16_t battery_mv();  // 0 = unknown / mains

// The physical setup switch (docs/board.md §2): on = hotspot + setup panel,
// off = join the saved Wi-Fi. Its own GPIO (switch to GND). Debounced by
// the caller.
bool setup_switch_on();

void reboot();

// Persistent files (LittleFS). read returns bytes read (0 = missing / error);
// write replaces the file; format erases everything but the browser client
// (/web), which only the flasher installs.
bool storage_begin();
size_t storage_read(const char* path, void* buf, size_t cap);
bool storage_write(const char* path, const void* buf, size_t n);
bool storage_remove(const char* path);
bool storage_format();
bool storage_has(const char* path);

}  // namespace board

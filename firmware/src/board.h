// Board glue for the Mava custom board (docs/board.md): ESP32-S3-N16R8 with an
// SX1262. Pins come from the build flags in platformio.ini.
#pragma once

#include <cstddef>
#include <cstdint>

namespace board {

// Monotonic milliseconds that do not wrap after 49 days.
uint64_t now_ms();

// Hardware random number (RNG peripheral); never a PRNG.
uint32_t random32();
void random_bytes(uint8_t* out, size_t n);

void init();  // radio power pin, LED, ADC, setup switch
void led(bool on);
uint16_t battery_mv();  // 0 = unknown / mains

// The physical setup switch (docs/board.md §2): on = hotspot + setup panel,
// off = join the saved Wi-Fi. Debounced by the caller.
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

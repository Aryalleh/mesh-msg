// The board's wall clock (Unix time), kept across boots: TLS needs it to
// check the relay's certificate, announces and messages carry it.
//
// At boot an RTC module on the I2C bus (47/48, next to the keyboard module)
// sets it: a DS3231 or DS1307 module (0x68, with its coin cell) or a
// PCF85063A (0x51, the handheld's part; a PCF8563 is not supported). Without
// one, the clock starts at the last time saved to flash, or the firmware's
// build date if later: not trusted (nothing is stamped with it), but close
// enough for the certificate check. Network time (NTP, the relay's Date
// header) and a phone's HELLO make it trusted, and are written to the RTC.
#pragma once

#include <cstddef>
#include <cstdint>

namespace wallclock {

// After Wire.begin().
void begin();
// The time came from the network or a host. Any task (NTP's callback, the
// relay worker); the main loop writes it to the RTC and flash.
void set(uint32_t unix_now, const char* source);
// Unix time when trusted (a valid RTC, or set()), else 0.
uint32_t trusted_now();
bool trusted();
// Main loop: writes a new time to the RTC, saves to flash now and then.
void loop();
// For the log: the RTC and where the time came from.
void status(char* out, size_t cap);

}  // namespace wallclock

#include "wallclock.h"

#include <Arduino.h>
#include <Wire.h>
#include <sys/time.h>
#include <time.h>

#include <atomic>
#include <cstdio>
#include <cstring>
#include <string>

#include "board.h"

namespace wallclock {
namespace {

constexpr const char* kFile = "/clock";
constexpr uint32_t kSaveEvery = 6 * 3600;  // flash wear: a few writes a day
constexpr uint32_t kMinValid = 1735689600;  // 2025-01-01
constexpr uint32_t kMaxValid = 4102444800;  // 2100-01-01

enum class Chip : uint8_t { None, DS3231, PCF85063A };
Chip g_chip = Chip::None;
uint8_t g_addr = 0;
bool g_rtc_valid = false;  // the RTC had a time at boot
std::atomic<bool> g_trusted{false};
std::atomic<uint32_t> g_pending{0};  // set() for loop() to write
const char* volatile g_source = "none";
uint32_t g_saved = 0;  // last time saved to flash

uint8_t bcd(uint8_t v) { return uint8_t((v / 10) << 4 | (v % 10)); }
uint8_t unbcd(uint8_t v) { return uint8_t((v >> 4) * 10 + (v & 15)); }

bool read_regs(uint8_t addr, uint8_t reg, uint8_t* out, size_t n) {
  Wire.beginTransmission(addr);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(int(addr), int(n)) != int(n)) return false;
  for (size_t i = 0; i < n; i++) out[i] = uint8_t(Wire.read());
  return true;
}

bool write_regs(uint8_t addr, uint8_t reg, const uint8_t* d, size_t n) {
  Wire.beginTransmission(addr);
  Wire.write(reg);
  Wire.write(d, n);
  return Wire.endTransmission() == 0;
}

bool present(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

// Days from 1970-01-01 (civil calendar, proleptic Gregorian).
int64_t days_from_civil(int y, int m, int d) {
  y -= m <= 2;
  int era = (y >= 0 ? y : y - 399) / 400;
  int yoe = y - era * 400;
  int doy = (153 * (m + (m > 2 ? -3 : 9)) + 2) / 5 + d - 1;
  int doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  return int64_t(era) * 146097 + doe - 719468;
}

uint32_t to_unix(int y, int mo, int d, int h, int mi, int s) {
  return uint32_t(days_from_civil(y, mo, d) * 86400 + h * 3600 + mi * 60 + s);
}

void from_unix(uint32_t t, int& y, int& mo, int& d, int& h, int& mi, int& s, int& wday) {
  time_t tt = time_t(t);
  struct tm tm;
  gmtime_r(&tt, &tm);
  y = tm.tm_year + 1900, mo = tm.tm_mon + 1, d = tm.tm_mday, h = tm.tm_hour, mi = tm.tm_min, s = tm.tm_sec;
  wday = tm.tm_wday;
}

// Seconds, minutes, hours, (weekday), date, month, year in BCD, 24-hour.
uint32_t rtc_read() {
  uint8_t r[7];
  int y, mo, d, h, mi, s;
  if (g_chip == Chip::DS3231) {  // also DS1307: same registers 0x00–0x06
    if (!read_regs(g_addr, 0x00, r, 7)) return 0;
    if (r[0] & 0x80) return 0;  // DS1307: clock halted
    uint8_t st = 0;
    if (read_regs(g_addr, 0x0F, &st, 1) && (st & 0x80)) return 0;  // DS3231: oscillator stopped
    s = unbcd(r[0] & 0x7F), mi = unbcd(r[1] & 0x7F);
    h = (r[2] & 0x40) ? unbcd(r[2] & 0x1F) % 12 + ((r[2] & 0x20) ? 12 : 0) : unbcd(r[2] & 0x3F);
    d = unbcd(r[4] & 0x3F), mo = unbcd(r[5] & 0x1F), y = 2000 + unbcd(r[6]);
  } else if (g_chip == Chip::PCF85063A) {
    if (!read_regs(g_addr, 0x04, r, 7)) return 0;
    if (r[0] & 0x80) return 0;  // OS: the oscillator stopped, time not valid
    s = unbcd(r[0] & 0x7F), mi = unbcd(r[1] & 0x7F), h = unbcd(r[2] & 0x3F);
    d = unbcd(r[3] & 0x3F), mo = unbcd(r[5] & 0x1F), y = 2000 + unbcd(r[6]);
  } else {
    return 0;
  }
  if (mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59) return 0;
  uint32_t t = to_unix(y, mo, d, h, mi, s);
  return t >= kMinValid && t < kMaxValid ? t : 0;
}

bool rtc_write(uint32_t t) {
  int y, mo, d, h, mi, s, wd;
  from_unix(t, y, mo, d, h, mi, s, wd);
  if (y < 2000 || y > 2099) return false;
  if (g_chip == Chip::DS3231) {
    uint8_t r[7] = {bcd(uint8_t(s)), bcd(uint8_t(mi)), bcd(uint8_t(h)), uint8_t(wd + 1),
                    bcd(uint8_t(d)), bcd(uint8_t(mo)), bcd(uint8_t(y - 2000))};
    if (!write_regs(g_addr, 0x00, r, 7)) return false;
    uint8_t st = 0;
    if (read_regs(g_addr, 0x0F, &st, 1)) {  // clear OSF (DS3231)
      st &= 0x7F;
      write_regs(g_addr, 0x0F, &st, 1);
    }
    return true;
  }
  if (g_chip == Chip::PCF85063A) {
    uint8_t r[7] = {bcd(uint8_t(s)), bcd(uint8_t(mi)), bcd(uint8_t(h)), bcd(uint8_t(d)),
                    uint8_t(wd), bcd(uint8_t(mo)), bcd(uint8_t(y - 2000))};
    return write_regs(g_addr, 0x04, r, 7);  // seconds bit 7 = 0 clears OS
  }
  return false;
}

uint32_t build_time() {
  static const char* months = "JanFebMarAprMayJunJulAugSepOctNovDec";
  const char* date = __DATE__;  // "Nov 18 2026"
  const char* tm = __TIME__;    // "16:40:05"
  const char* p = strstr(months, std::string(date, 3).c_str());
  int mo = p ? int(p - months) / 3 + 1 : 1;
  int d = atoi(date + 4), y = atoi(date + 7);
  return to_unix(y, mo, d, atoi(tm), atoi(tm + 3), atoi(tm + 6));
}

void set_system(uint32_t t) {
  struct timeval tv = {time_t(t), 0};
  settimeofday(&tv, nullptr);
}

}  // namespace

void begin() {
#if defined(I2C_SDA) && defined(I2C_SCL)
  if (present(0x68)) {
    g_chip = Chip::DS3231, g_addr = 0x68;
  } else if (present(0x51)) {
    g_chip = Chip::PCF85063A, g_addr = 0x51;
    uint8_t c1 = 0;
    if (read_regs(g_addr, 0x00, &c1, 1) && (c1 & 0x20)) {  // STOP set: start the clock
      c1 &= uint8_t(~0x20);
      write_regs(g_addr, 0x00, &c1, 1);
    }
  }
#endif
  uint32_t t = rtc_read();
  if (t) {
    g_rtc_valid = true;
    set_system(t);
    g_trusted = true;
    g_source = "RTC";
    return;
  }
  // No RTC time: start from the last saved time or the build date, whichever
  // is later. Not trusted: only for checking certificates.
  uint32_t saved = 0;
  board::storage_read(kFile, &saved, sizeof saved);
  uint32_t floor = build_time();
  if (saved > floor && saved < kMaxValid) floor = saved;
  if (uint32_t(time(nullptr)) < floor) set_system(floor);
  g_saved = saved;
}

void set(uint32_t unix_now, const char* source) {
  if (unix_now < kMinValid || unix_now >= kMaxValid) return;
  if (uint32_t(time(nullptr)) + 2 < unix_now || uint32_t(time(nullptr)) > unix_now + 2) set_system(unix_now);
  g_source = source;
  g_pending = unix_now;
  g_trusted = true;
}

uint32_t trusted_now() { return g_trusted ? uint32_t(time(nullptr)) : 0; }
bool trusted() { return g_trusted; }

void loop() {
  uint32_t p = g_pending.exchange(0);
  uint32_t now = uint32_t(time(nullptr));
  if (p) {
    if (g_chip != Chip::None) rtc_write(now);
    board::storage_write(kFile, &now, sizeof now);
    g_saved = now;
    return;
  }
  if (g_trusted && now > g_saved + kSaveEvery) {
    board::storage_write(kFile, &now, sizeof now);
    g_saved = now;
  }
}

void status(char* out, size_t cap) {
  const char* chip = g_chip == Chip::DS3231 ? "DS3231/DS1307" : g_chip == Chip::PCF85063A ? "PCF85063A" : "none";
  time_t t = time(nullptr);
  struct tm tm;
  gmtime_r(&t, &tm);
  char when[24];
  strftime(when, sizeof when, "%Y-%m-%d %H:%M UTC", &tm);
  snprintf(out, cap, "RTC %s%s; clock %s (%s%s)", chip,
           g_chip == Chip::None ? "" : g_rtc_valid ? ", had the time" : ", had no valid time", when,
           g_trusted ? "from " : "not set yet: last saved time or build date",
           g_trusted ? (const char*)g_source : "");
}

}  // namespace wallclock

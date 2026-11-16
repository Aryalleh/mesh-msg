#include "board.h"

#include <Arduino.h>
#include <FS.h>
#include <LittleFS.h>
#include <esp_random.h>
#include <esp_system.h>

#ifndef SETUP_SWITCH_ACTIVE
#define SETUP_SWITCH_ACTIVE LOW  // switch closes to GND
#endif

namespace board {

uint64_t now_ms() {
  static uint32_t last = 0;
  static uint64_t high = 0;
  uint32_t m = millis();
  if (m < last) high += uint64_t(1) << 32;
  last = m;
  return high | m;
}

uint32_t random32() { return esp_random(); }

void random_bytes(uint8_t* out, size_t n) {
  while (n > 0) {
    uint32_t r = random32();
    size_t k = n < 4 ? n : 4;
    memcpy(out, &r, k);
    out += k;
    n -= k;
  }
}

void init() {
#ifdef RADIO_POWER_PIN
  pinMode(RADIO_POWER_PIN, OUTPUT);
  digitalWrite(RADIO_POWER_PIN, HIGH);
  delay(10);
#endif
#ifdef LED_PIN
  pinMode(LED_PIN, OUTPUT);
  digitalWrite(LED_PIN, LOW);
#endif
#ifdef SETUP_SWITCH_PIN
  pinMode(SETUP_SWITCH_PIN, SETUP_SWITCH_ACTIVE == LOW ? INPUT_PULLUP : INPUT_PULLDOWN);
#endif
}

void led(bool on) {
#ifdef LED_PIN
  digitalWrite(LED_PIN, on ? HIGH : LOW);
#else
  (void)on;
#endif
}

bool setup_switch_on() {
#ifdef SETUP_SWITCH_PIN
  return digitalRead(SETUP_SWITCH_PIN) == SETUP_SWITCH_ACTIVE;
#else
  return false;
#endif
}

uint16_t battery_mv() {
#ifdef BATTERY_ADC_PIN
  uint32_t mv = analogReadMilliVolts(BATTERY_ADC_PIN);
  return uint16_t(mv * BATTERY_MULT_X100 / 100);
#else
  return 0;
#endif
}

void reboot() {
  delay(50);
  ESP.restart();
  for (;;) {
  }
}

// ---- storage ----

bool storage_begin() { return LittleFS.begin(true); }

size_t storage_read(const char* path, void* buf, size_t cap) {
  File f = LittleFS.open(path, "r");
  if (!f) return 0;
  size_t n = f.read(static_cast<uint8_t*>(buf), cap);
  f.close();
  return n;
}

bool storage_write(const char* path, const void* buf, size_t n) {
  File f = LittleFS.open(path, "w");
  if (!f) return false;
  bool ok = f.write(static_cast<const uint8_t*>(buf), n) == n;
  f.close();
  return ok;
}

bool storage_remove(const char* path) { return LittleFS.remove(path); }

bool storage_format() { return LittleFS.format(); }

}  // namespace board

#include "board.h"

#include <Arduino.h>
#include <FS.h>
#include <LittleFS.h>
#include <esp_random.h>
#include <esp_system.h>

#include "keys.h"

namespace board {

namespace {

// Keyboard matrix (docs/handheld-proto.md §4): rows are inputs with pull-ups
// (RTC GPIOs, so a key can wake the chip), columns are driven low one at a
// time and float otherwise; a diode per key, cathode towards the column.
constexpr uint8_t kRowPins[mesh::keys::kRows] = {KB_ROW_PINS};
constexpr uint8_t kColPins[mesh::keys::kCols] = {KB_COL_PINS};

uint8_t scan_column(int c) {
  pinMode(kColPins[c], OUTPUT);
  digitalWrite(kColPins[c], LOW);
  delayMicroseconds(10);
  uint8_t rows = 0;
  for (int r = 0; r < mesh::keys::kRows; r++)
    if (digitalRead(kRowPins[r]) == LOW) rows |= uint8_t(1u << r);
  pinMode(kColPins[c], INPUT);
  return rows;
}

void deselect(int pin) {
  pinMode(pin, OUTPUT);
  digitalWrite(pin, HIGH);
}

}  // namespace

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
  // Shared SPI bus (LoRa, display, touch): every other chip select high
  // before the radio uses it.
  deselect(LCD_CS);
  deselect(TOUCH_CS);
  // Strapping pins 45 (backlight) and 46 (I2S DOUT) stay low: off, no pop.
  pinMode(LCD_BL, OUTPUT);
  digitalWrite(LCD_BL, LOW);
  pinMode(I2S_DOUT, OUTPUT);
  digitalWrite(I2S_DOUT, LOW);
  for (uint8_t p : kRowPins) pinMode(p, INPUT_PULLUP);
  for (uint8_t p : kColPins) pinMode(p, INPUT);
}

void scan_keys(uint8_t out[]) {
  for (int c = 0; c < mesh::keys::kCols; c++) out[c] = scan_column(c);
}

void led(bool on) {
#ifdef LED_PIN
  digitalWrite(LED_PIN, on ? HIGH : LOW);
#else
  (void)on;  // the handheld has no LED: activity shows on the screen
#endif
}

bool setup_switch_on() {
  mesh::keys::Pos p = mesh::keys::find(mesh::keys::Key::Setup);
  return scan_column(p.col) >> p.row & 1;
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

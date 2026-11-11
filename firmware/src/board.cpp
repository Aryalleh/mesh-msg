#include "board.h"

#include <Arduino.h>

#if defined(ESP32)
#include <FS.h>
#include <LittleFS.h>
#include <esp_random.h>
#include <esp_system.h>
#if HAS_PMU_AXP2101
#include <Wire.h>
#define XPOWERS_CHIP_AXP2101
#include <XPowersLib.h>
static XPowersPMU g_pmu;
static bool g_pmu_ok = false;
#endif
#elif defined(NRF52_SERIES)
#include <Adafruit_LittleFS.h>
#include <InternalFileSystem.h>
#include <nrf.h>
using namespace Adafruit_LittleFS_Namespace;
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

uint32_t random32() {
#if defined(ESP32)
  return esp_random();
#elif defined(NRF52_SERIES)
  // The SoftDevice is not started (no BLE yet), so the RNG is ours.
  NRF_RNG->CONFIG = RNG_CONFIG_DERCEN_Msk;
  NRF_RNG->TASKS_START = 1;
  uint32_t v = 0;
  for (int i = 0; i < 4; i++) {
    NRF_RNG->EVENTS_VALRDY = 0;
    while (!NRF_RNG->EVENTS_VALRDY) {
    }
    v = v << 8 | uint8_t(NRF_RNG->VALUE);
  }
  NRF_RNG->TASKS_STOP = 1;
  return v;
#else
#error "no hardware RNG for this MCU"
#endif
}

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
#if HAS_PMU_AXP2101
  Wire.begin(PMU_SDA, PMU_SCL);
  g_pmu_ok = g_pmu.begin(Wire, AXP2101_SLAVE_ADDRESS, PMU_SDA, PMU_SCL);
  if (g_pmu_ok) {
    // T-Beam S3: ALDO2 feeds the SX1262, ALDO3 the GPS, ALDO1/BLDO1 sensors
    // and the display. Keep the GPS off until it is used.
    g_pmu.setALDO2Voltage(3300);
    g_pmu.enableALDO2();
    g_pmu.setALDO1Voltage(3300);
    g_pmu.enableALDO1();
    g_pmu.setBLDO1Voltage(3300);
    g_pmu.enableBLDO1();
    g_pmu.disableALDO3();
    g_pmu.enableBattVoltageMeasure();
    delay(20);  // rail settling before the radio reset
  }
#endif
#ifdef RADIO_POWER_PIN
  pinMode(RADIO_POWER_PIN, OUTPUT);
  digitalWrite(RADIO_POWER_PIN, HIGH);
  delay(10);
#endif
#ifdef LED_PIN
  pinMode(LED_PIN, OUTPUT);
  digitalWrite(LED_PIN, LOW);
#endif
#ifdef BATTERY_CTRL_PIN
  pinMode(BATTERY_CTRL_PIN, INPUT);  // floating = divider off
#endif
#if defined(NRF52_SERIES) && defined(BATTERY_ADC_PIN)
  analogReference(AR_INTERNAL_3_0);
  analogReadResolution(12);
#endif
}

void led(bool on) {
#ifdef LED_PIN
  digitalWrite(LED_PIN, on ? HIGH : LOW);
#else
  (void)on;
#endif
}

uint16_t battery_mv() {
#if BATTERY_FROM_PMU && HAS_PMU_AXP2101
  if (!g_pmu_ok || !g_pmu.isBatteryConnect()) return 0;
  return uint16_t(g_pmu.getBattVoltage());
#elif defined(BATTERY_ADC_PIN)
#ifdef BATTERY_CTRL_PIN
  pinMode(BATTERY_CTRL_PIN, OUTPUT);
  digitalWrite(BATTERY_CTRL_PIN, BATTERY_CTRL_ACTIVE);
  delay(2);
#endif
  uint32_t mv;
#if defined(ESP32)
  mv = analogReadMilliVolts(BATTERY_ADC_PIN);
#else
  mv = uint32_t(analogRead(BATTERY_ADC_PIN)) * 3000 / 4095;
#endif
#ifdef BATTERY_CTRL_PIN
  pinMode(BATTERY_CTRL_PIN, INPUT);
#endif
  return uint16_t(mv * BATTERY_MULT_X100 / 100);
#else
  return 0;
#endif
}

void reboot() {
  delay(50);
#if defined(ESP32)
  ESP.restart();
#elif defined(NRF52_SERIES)
  NVIC_SystemReset();
#endif
  for (;;) {
  }
}

// ---- storage ----

bool storage_begin() {
#if defined(ESP32)
  return LittleFS.begin(true);
#elif defined(NRF52_SERIES)
  return InternalFS.begin();
#endif
}

size_t storage_read(const char* path, void* buf, size_t cap) {
#if defined(ESP32)
  File f = LittleFS.open(path, "r");
  if (!f) return 0;
  size_t n = f.read(static_cast<uint8_t*>(buf), cap);
  f.close();
  return n;
#elif defined(NRF52_SERIES)
  File f(InternalFS);
  if (!f.open(path, FILE_O_READ)) return 0;
  int n = f.read(buf, uint16_t(cap));
  f.close();
  return n < 0 ? 0 : size_t(n);
#endif
}

bool storage_write(const char* path, const void* buf, size_t n) {
#if defined(ESP32)
  File f = LittleFS.open(path, "w");
  if (!f) return false;
  bool ok = f.write(static_cast<const uint8_t*>(buf), n) == n;
  f.close();
  return ok;
#elif defined(NRF52_SERIES)
  InternalFS.remove(path);  // FILE_O_WRITE appends
  File f(InternalFS);
  if (!f.open(path, FILE_O_WRITE)) return false;
  bool ok = f.write(static_cast<const uint8_t*>(buf), n) == n;
  f.close();
  return ok;
#endif
}

bool storage_remove(const char* path) {
#if defined(ESP32)
  return LittleFS.remove(path);
#elif defined(NRF52_SERIES)
  return InternalFS.remove(path);
#endif
}

bool storage_format() {
#if defined(ESP32)
  return LittleFS.format();
#elif defined(NRF52_SERIES)
  return InternalFS.format();
#endif
}

}  // namespace board

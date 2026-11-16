// Keyboard module (docs/handheld-proto.md §3): Raspberry Pi Pico.
//
//   GP0 / GP1     I2C target SDA / SCL → the handheld's I2C bus (ESP32-S3 47 / 48)
//   GP2           INT → ESP32-S3 GPIO 9, low while events or motion are pending
//   GP3 – GP9     rows R0–R6 (inputs, pull-ups)
//   GP10 – GP16   columns C0–C6 (driven low one at a time; C6 unused on the 9900)
//   GP17          keyboard / trackpad backlight PWM (gate of an N-FET)
//   GP18 / GP19   I2C controller SDA / SCL → trackpad (version C)
//   GP21 / GP22   trackpad NRST / SHTDWN (version C)
//
// The module only reports matrix positions (debounced) and motion; the
// ESP32 applies the layout (mesh::keys::Layout).
#include <Arduino.h>
#include <Wire.h>

#include "kbproto.h"
#include "keys.h"

namespace {

namespace k = mesh::keys;

constexpr uint8_t kSda = 0, kScl = 1, kInt = 2, kBacklight = 17;
constexpr uint8_t kRowPins[7] = {3, 4, 5, 6, 7, 8, 9};
constexpr uint8_t kColPins[KB_COLS] = {10, 11, 12, 13, 14, 15
#if KB_COLS > 6
                                       ,
                                       16
#endif
};
constexpr uint8_t kTpSda = 18, kTpScl = 19, kTpReset = 21, kTpShutdown = 22;
constexpr uint8_t kTpAddr = 0x3B;

const k::Variant kVariant = k::Variant(KB_VARIANT);
k::ModuleState g_state(kVariant);
k::Matrix g_matrix(k::keyboard(kVariant));
volatile uint8_t g_reg = k::kRegEvents;
bool g_trackpad = false;

// I2C target callbacks run in interrupt context: keep them short and guard
// the shared state against the main loop.
void on_receive(int n) {
  uint8_t b[4];
  int got = 0;
  while (Wire.available() && got < int(sizeof b)) b[got++] = uint8_t(Wire.read());
  while (Wire.available()) Wire.read();
  (void)n;
  if (got < 1) return;
  g_reg = b[0];
  noInterrupts();
  int level = g_state.write(b, size_t(got));
  interrupts();
  if (level >= 0) analogWrite(kBacklight, level);
}

void on_request() {
  uint8_t out[1 + k::kEventsPerRead];
  noInterrupts();
  size_t n = g_state.read(g_reg, out, sizeof out);
  interrupts();
  if (n == 0) out[n++] = 0;
  Wire.write(out, n);
}

uint8_t scan_column(int c) {
  pinMode(kColPins[c], OUTPUT);
  digitalWrite(kColPins[c], LOW);
  delayMicroseconds(10);
  uint8_t rows = 0;
  for (int r = 0; r < 7; r++)
    if (digitalRead(kRowPins[r]) == LOW) rows |= uint8_t(1u << r);
  pinMode(kColPins[c], INPUT);
  return rows;
}

// ---- trackpad (Avago ADBS-A320, version C) ----

bool tp_reg(uint8_t r, uint8_t* v) {
  Wire1.beginTransmission(kTpAddr);
  Wire1.write(r);
  if (Wire1.endTransmission(false) != 0) return false;
  if (Wire1.requestFrom(kTpAddr, uint8_t(1)) != 1) return false;
  *v = uint8_t(Wire1.read());
  return true;
}

bool tp_begin() {
  pinMode(kTpShutdown, OUTPUT);
  digitalWrite(kTpShutdown, LOW);  // out of shutdown
  pinMode(kTpReset, OUTPUT);
  digitalWrite(kTpReset, LOW);
  delay(100);
  digitalWrite(kTpReset, HIGH);
  delay(50);
  Wire1.setSDA(kTpSda);
  Wire1.setSCL(kTpScl);
  Wire1.begin();
  Wire1.setClock(400000);
  uint8_t id = 0;
  return tp_reg(0x00, &id) && id == 0x83;
}

void tp_poll() {
  uint8_t m = 0, x = 0, y = 0;
  if (!tp_reg(0x02, &m) || !(m & 0x80)) return;
  if (!tp_reg(0x03, &x) || !tp_reg(0x04, &y)) return;
  noInterrupts();
  g_state.motion(int8_t(y), int8_t(x));  // the sensor's X is the screen's Y in the 9900
  interrupts();
}

}  // namespace

void setup() {
  for (uint8_t p : kRowPins) pinMode(p, INPUT_PULLUP);
  for (uint8_t p : kColPins) pinMode(p, INPUT);
  pinMode(kInt, INPUT);  // released (pulled up on the ESP32 side)
  pinMode(kBacklight, OUTPUT);
  analogWrite(kBacklight, 0);
#if HAS_TRACKPAD
  g_trackpad = tp_begin();
#endif
  Serial.begin(115200);  // USB: key positions, for finding a keypad's flex order
  Wire.setSDA(kSda);
  Wire.setSCL(kScl);
  Wire.begin(k::kModuleAddr);
  Wire.onReceive(on_receive);
  Wire.onRequest(on_request);
}

void loop() {
  static uint32_t last_scan = 0, last_tp = 0;
  uint32_t now = millis();
  if (now - last_scan >= 2) {
    last_scan = now;
    uint8_t scan[k::kMaxCols] = {};
    for (int c = 0; c < KB_COLS; c++) scan[c] = scan_column(c);
    k::Event ev[8];
    size_t n = g_matrix.update(scan, now, ev, 8);
    noInterrupts();
    for (size_t i = 0; i < n; i++) g_state.push(ev[i]);
    interrupts();
    for (size_t i = 0; i < n; i++)
      Serial.printf("key R%d C%d %s\r\n", ev[i].pos.row, ev[i].pos.col, ev[i].pressed ? "down" : "up");
  }
  if (g_trackpad && now - last_tp >= 8) {
    last_tp = now;
    tp_poll();
  }
  noInterrupts();
  bool pending = g_state.pending();
  interrupts();
  // Open drain: drive low while something is pending, release otherwise.
  if (pending) {
    pinMode(kInt, OUTPUT);
    digitalWrite(kInt, LOW);
  } else {
    pinMode(kInt, INPUT);
  }
}

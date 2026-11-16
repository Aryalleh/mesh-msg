#include "trackpad.h"

#if HAS_TRACKPAD
#include <Arduino.h>
#include <Wire.h>

namespace trackpad {
namespace {

constexpr uint8_t kAddr = 0x3B;
constexpr uint8_t kRegProductId = 0x00;
constexpr uint8_t kRegMotion = 0x02;
constexpr uint8_t kRegDeltaX = 0x03;
constexpr uint8_t kRegDeltaY = 0x04;
constexpr uint8_t kProductId = 0x83;
constexpr uint8_t kMotionBit = 0x80;

bool g_ok = false;

bool reg(uint8_t r, uint8_t* v) {
  Wire.beginTransmission(kAddr);
  Wire.write(r);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(kAddr, uint8_t(1)) != 1) return false;
  *v = uint8_t(Wire.read());
  return true;
}

}  // namespace

bool begin() {
  Wire.begin(I2C_SDA, I2C_SCL, 400000);
  uint8_t id = 0;
  g_ok = reg(kRegProductId, &id) && id == kProductId;
  return g_ok;
}

bool read(int8_t* dx, int8_t* dy) {
  uint8_t m = 0, x = 0, y = 0;
  if (!g_ok || !reg(kRegMotion, &m) || !(m & kMotionBit)) return false;
  if (!reg(kRegDeltaX, &x) || !reg(kRegDeltaY, &y)) return false;
  // Mounted as in the 9900: the sensor's X is the screen's Y.
  *dx = int8_t(y);
  *dy = int8_t(x);
  return true;
}

}  // namespace trackpad
#else
namespace trackpad {
bool begin() { return false; }
bool read(int8_t*, int8_t*) { return false; }
}  // namespace trackpad
#endif

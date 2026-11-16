#include "kbmodule.h"

#include <Arduino.h>
#include <Wire.h>

#include "kbproto.h"

namespace kbmodule {
namespace {

namespace k = mesh::keys;

bool g_present = false;
k::Variant g_variant = k::Variant::Matrix7x7;
k::Layout* g_layout = nullptr;
uint32_t g_last = 0;

size_t read_reg(uint8_t reg, uint8_t* out, size_t n) {
  Wire.beginTransmission(k::kModuleAddr);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return 0;
  size_t got = Wire.requestFrom(k::kModuleAddr, uint8_t(n));
  for (size_t i = 0; i < got && i < n; i++) out[i] = uint8_t(Wire.read());
  return got;
}

}  // namespace

bool begin() {
  uint8_t id[4];
  g_present = read_reg(k::kRegId, id, sizeof id) == sizeof id && k::parse_id(id, sizeof id, &g_variant);
  static k::Layout layout(k::keyboard(g_variant));
  g_layout = &layout;
  return g_present;
}

bool present() { return g_present; }
k::Variant variant() { return g_variant; }
k::Layout& layout() { return *g_layout; }

size_t poll(uint32_t now_ms, k::Output* out, size_t cap, int* dx, int* dy) {
  if (!g_present) return 0;
#ifdef KB_INT
  bool pending = digitalRead(KB_INT) == LOW;
#else
  bool pending = true;
#endif
  if (!pending && now_ms - g_last < 50) return 0;
  g_last = now_ms;
  size_t n = 0;
  uint8_t b[1 + k::kEventsPerRead];
  size_t got = read_reg(k::kRegEvents, b, sizeof b);
  k::Event ev[k::kEventsPerRead];
  size_t m = k::parse_events(b, got, ev, k::kEventsPerRead);
  for (size_t i = 0; i < m && n < cap; i++) {
    k::Output o = g_layout->on_event(ev[i]);
    if (o.action != k::Action::None) out[n++] = o;
  }
  if (g_variant == k::Variant::BB9900Trackpad) {
    uint8_t t[2];
    if (read_reg(k::kRegTrack, t, 2) == 2) {
      *dx += int8_t(t[0]);
      *dy += int8_t(t[1]);
    }
  }
  return n;
}

void backlight(uint8_t level) {
  if (!g_present) return;
  Wire.beginTransmission(k::kModuleAddr);
  Wire.write(k::kRegBacklight);
  Wire.write(level);
  Wire.endTransmission();
}

}  // namespace kbmodule

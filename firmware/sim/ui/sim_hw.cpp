// Host stand-ins for the screen and the keyboard module, so ui.cpp renders
// into memory: the simulator injects keys and taps and writes screenshots.
#include <lvgl.h>

#include <cstring>
#include <deque>
#include <vector>

#include "../../src/kbmodule.h"
#include "../../src/ui/display.h"
#include "kbproto.h"

namespace sim {
uint16_t fb[240 * 320];
std::deque<mesh::keys::Output> keys;
bool module = true;
bool touched = false;
int tx = 0, ty = 0;
}  // namespace sim

namespace display {
namespace {
lv_indev_t* g_touch = nullptr;
uint8_t g_buf[240 * 40 * 2];
void flush(lv_display_t* d, const lv_area_t* a, uint8_t* px) {
  const uint16_t* p = reinterpret_cast<const uint16_t*>(px);
  for (int y = a->y1; y <= a->y2; y++)
    for (int x = a->x1; x <= a->x2; x++) sim::fb[y * 240 + x] = *p++;
  lv_display_flush_ready(d);
}
void touch_cb(lv_indev_t*, lv_indev_data_t* d) {
  d->point.x = sim::tx;
  d->point.y = sim::ty;
  d->state = sim::touched ? LV_INDEV_STATE_PRESSED : LV_INDEV_STATE_RELEASED;
}
}  // namespace
bool begin() {
  lv_display_t* d = lv_display_create(240, 320);
  lv_display_set_flush_cb(d, flush);
  lv_display_set_buffers(d, g_buf, nullptr, sizeof g_buf, LV_DISPLAY_RENDER_MODE_PARTIAL);
  g_touch = lv_indev_create();
  lv_indev_set_type(g_touch, LV_INDEV_TYPE_POINTER);
  lv_indev_set_read_cb(g_touch, touch_cb);
  return true;
}
lv_indev_t* touch() { return g_touch; }
void backlight(uint8_t) {}
}  // namespace display

namespace kbmodule {
namespace {
mesh::keys::Layout g_layout(mesh::keys::keyboard(mesh::keys::Variant::Matrix7x7));
}
bool begin() { return true; }
bool present() { return sim::module; }
mesh::keys::Variant variant() { return mesh::keys::Variant::Matrix7x7; }
mesh::keys::Layout& layout() { return g_layout; }
size_t poll(uint32_t, mesh::keys::Output* out, size_t cap, int*, int*) {
  size_t n = 0;
  while (!sim::keys.empty() && n < cap) {
    out[n++] = sim::keys.front();
    sim::keys.pop_front();
    break;  // one key per loop, as a person types
  }
  return n;
}
void backlight(uint8_t) {}
}  // namespace kbmodule

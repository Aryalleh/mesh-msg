#include "display.h"

#include <Arduino.h>
#include <SPI.h>
#include <esp_heap_caps.h>

#include "../hwsim/hwsim.h"
#include "board.h"
#include "touchcal.h"
#include "src/drivers/display/ili9341/lv_ili9341.h"

namespace display {

#ifdef LCD_CS

namespace {

constexpr int kWidth = 240, kHeight = 320;
constexpr uint32_t kLcdHz = 40000000, kTouchHz = 2000000;
// One buffer of 20 rows (12.5 kB) in internal RAM: rendering into PSRAM made
// the screen visibly slow (beta33). One is enough: the flush writes the panel
// before it returns, so a second buffer was never drawn into meanwhile, and
// internal RAM is short (Wi-Fi, Bluetooth, TLS: beta35 ran out with two).
constexpr int kBufLines = 20;

// Raw XPT2046 readings at the screen edges (portrait x 0 and 239, y 0 and
// 319), used until Settings → Calibrate touch saves a calibration. On the
// owner's panel ("TFT SPI 240x320 V1.2") both raw axes run the other way:
// touches came out mirrored through the middle (2026-11-18), and with only X
// turned, mirrored top to bottom.
#ifndef TOUCH_X_MIN
#define TOUCH_X_MIN 3880
#define TOUCH_X_MAX 220
#endif
#ifndef TOUCH_Y_MIN
#define TOUCH_Y_MIN 3860
#define TOUCH_Y_MAX 260
#endif

constexpr const char* kCalFile = "/touchcal";
// MTC2: raw → the panel's own (portrait) pixels, so it holds in any rotation.
// MTC1 (0.1.1-beta33/34): raw → landscape screen pixels at rotation 90.
constexpr uint32_t kCalMagic = 0x4d544332, kCalMagicV1 = 0x4d544331;
constexpr const char* kRotationFile = "/rotation";
struct CalFile {
  uint32_t magic = kCalMagic;
  mesh::ui::TouchCal cal;
};
bool g_have_cal = false;
mesh::ui::TouchCal g_cal;
// Calibration in progress: the raw readings of each cross.
volatile int g_cal_step = 3;
bool g_cal_on = false;
int g_cal_raw[3][2];
int g_cal_targets[3][2];  // where the crosses are drawn (screen pixels)
int g_cal_phys[3][2];     // the same points on the panel (portrait pixels)

// Screen pixels (as LVGL draws at this rotation) → the panel's own pixels.
void to_panel(lv_display_rotation_t r, int lx, int ly, int* px, int* py) {
  switch (r) {
    case LV_DISPLAY_ROTATION_90: *px = ly, *py = kHeight - 1 - lx; break;
    case LV_DISPLAY_ROTATION_270: *px = kWidth - 1 - ly, *py = lx; break;
    case LV_DISPLAY_ROTATION_180: *px = kWidth - 1 - lx, *py = kHeight - 1 - ly; break;
    default: *px = lx, *py = ly; break;
  }
}
uint32_t g_cal_sx = 0, g_cal_sy = 0, g_cal_n = 0;

SPIClass g_spi(HSPI);
lv_display_t* g_disp = nullptr;
const char* g_error = "";
lv_indev_t* g_touch = nullptr;

void cs(int pin, bool on) { digitalWrite(pin, on ? LOW : HIGH); }

lv_display_rotation_t rotation_of(int deg) {
  return deg == 0 ? LV_DISPLAY_ROTATION_0 : deg == 180 ? LV_DISPLAY_ROTATION_180 : deg == 270 ? LV_DISPLAY_ROTATION_270
                                                                                              : LV_DISPLAY_ROTATION_90;
}

// The window the driver set (CASET / PASET) for the next pixels: the screen
// mirror copies them there (hwsim.h).
int g_x1 = 0, g_x2 = 0, g_y1 = 0, g_y2 = 0;

void send_cmd(lv_display_t*, const uint8_t* cmd, size_t cmd_size, const uint8_t* param, size_t param_size) {
  if (cmd_size == 1 && param_size == 4) {
    int a = param[0] << 8 | param[1], b = param[2] << 8 | param[3];
    if (cmd[0] == 0x2A) g_x1 = a, g_x2 = b;
    if (cmd[0] == 0x2B) g_y1 = a, g_y2 = b;
  }
  g_spi.beginTransaction(SPISettings(kLcdHz, MSBFIRST, SPI_MODE0));
  cs(LCD_CS, true);
  digitalWrite(LCD_DC, LOW);
  g_spi.writeBytes(cmd, cmd_size);
  digitalWrite(LCD_DC, HIGH);
  if (param_size) g_spi.writeBytes(param, param_size);
  cs(LCD_CS, false);
  g_spi.endTransaction();
}

void send_color(lv_display_t* d, const uint8_t* cmd, size_t cmd_size, uint8_t* param, size_t param_size) {
  if (size_t(g_x2 - g_x1 + 1) * size_t(g_y2 - g_y1 + 1) * 2 == param_size)
    hwsim::blit(g_x1, g_y1, g_x2, g_y2, reinterpret_cast<const uint16_t*>(param));
  lv_draw_sw_rgb565_swap(param, param_size / 2);  // the panel wants big-endian RGB565
  send_cmd(d, cmd, cmd_size, param, param_size);
  lv_display_flush_ready(d);
}

uint16_t touch_read(uint8_t cmd) {
  g_spi.transfer(cmd);
  uint16_t v = g_spi.transfer16(0);
  return v >> 3;  // 12 bits
}

void touch_cb(lv_indev_t*, lv_indev_data_t* data) {
  data->state = LV_INDEV_STATE_RELEASED;
  int z = 0;
  uint32_t sx = 0, sy = 0;
  bool down = true;
#ifdef TOUCH_IRQ
  down = digitalRead(TOUCH_IRQ) == LOW;  // PENIRQ: low while touched
#endif
  if (down) {
    g_spi.beginTransaction(SPISettings(kTouchHz, MSBFIRST, SPI_MODE0));
    cs(TOUCH_CS, true);
    uint16_t z1 = touch_read(0xB1), z2 = touch_read(0xC1);
    z = int(z1) + 4095 - int(z2);
    for (int i = 0; i < 4; i++) {
      sx += touch_read(0xD1);
      sy += touch_read(0x91);
    }
    touch_read(0xD0);  // power down, keep PENIRQ on
    cs(TOUCH_CS, false);
    g_spi.endTransaction();
  }
  int rx = int(sx / 4), ry = int(sy / 4);
  if (g_cal_on) {  // calibrating: collect the cross's readings, LVGL sees nothing
    if (z >= 400) {
      if (g_cal_n < 1000) g_cal_n++;
      if (g_cal_n > 2) g_cal_sx += uint32_t(rx), g_cal_sy += uint32_t(ry);  // skip the first, still settling
      return;
    }
    if (g_cal_n > 5) {  // released after a steady press: the next cross
      int k = g_cal_step;
      g_cal_raw[k][0] = int(g_cal_sx / (g_cal_n - 2));
      g_cal_raw[k][1] = int(g_cal_sy / (g_cal_n - 2));
      if (k < 2) {
        g_cal_step = k + 1;
      } else {
        mesh::ui::TouchCal c;
        g_cal_on = false;
        if (mesh::ui::touch_cal_solve(g_cal_raw, g_cal_phys, c)) {
          CalFile f;
          f.cal = c;
          board::storage_write(kCalFile, &f, sizeof f);
          g_cal = c;
          g_have_cal = true;
          g_cal_step = 3;
        } else {
          g_cal_step = -1;
        }
      }
    }
    g_cal_sx = g_cal_sy = g_cal_n = 0;
    return;
  }
  if (z < 400) return;
  if (g_have_cal) {
    // Calibrated to the panel's own pixels; LVGL turns them for the rotation.
    int px, py;
    mesh::ui::touch_cal_apply(g_cal, rx, ry, &px, &py);
    data->point.x = int32_t(constrain(px, 0, kWidth - 1));
    data->point.y = int32_t(constrain(py, 0, kHeight - 1));
    data->state = LV_INDEV_STATE_PRESSED;
    return;
  }
  long x = map(long(rx), TOUCH_X_MIN, TOUCH_X_MAX, 0, kWidth - 1);
  long y = map(long(ry), TOUCH_Y_MIN, TOUCH_Y_MAX, 0, kHeight - 1);
  data->point.x = int32_t(constrain(x, 0, kWidth - 1));
  data->point.y = int32_t(constrain(y, 0, kHeight - 1));
  data->state = LV_INDEV_STATE_PRESSED;
}

}  // namespace

bool begin() {
  pinMode(LCD_CS, OUTPUT);
  cs(LCD_CS, false);
  pinMode(LCD_DC, OUTPUT);
#ifdef LCD_RST
  pinMode(LCD_RST, OUTPUT);
  digitalWrite(LCD_RST, LOW);
  delay(10);
  digitalWrite(LCD_RST, HIGH);
  delay(120);
#endif
  g_spi.begin(LCD_SCK, LCD_MISO, LCD_MOSI, -1);
  g_disp = lv_ili9341_create(kWidth, kHeight, LV_LCD_FLAG_BGR, send_cmd, send_color);
  if (!g_disp) {
    g_error = "LVGL display not created";
    return false;
  }
  // Landscape by default, as the BlackBerry: 320 × 240; Settings → rotation
  // turns it (saved in /rotation). The panel rotates in hardware (MADCTL), so
  // flushed areas (and the phone's copy) follow.
#ifndef LCD_ROTATION
#define LCD_ROTATION 90
#endif
  uint16_t deg = LCD_ROTATION;
  board::storage_read(kRotationFile, &deg, sizeof deg);
  lv_display_set_rotation(g_disp, rotation_of(deg));
  hwsim::resize(lv_display_get_horizontal_resolution(g_disp), lv_display_get_vertical_resolution(g_disp));
  size_t bytes = size_t(kHeight) * kBufLines * 2;  // rows are up to 320 pixels wide
  // Internal RAM: LVGL draws into these, and PSRAM is several times slower
  // for that. PSRAM only if internal RAM is short.
  auto alloc = [](size_t n) {
    void* p = heap_caps_malloc(n, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    return p ? p : heap_caps_malloc(n, MALLOC_CAP_SPIRAM);
  };
  void* a = alloc(bytes);
  if (!a) {
    g_error = "no memory for the draw buffer";
    return false;
  }
  lv_display_set_buffers(g_disp, a, nullptr, bytes, LV_DISPLAY_RENDER_MODE_PARTIAL);
#ifdef TOUCH_CS
  pinMode(TOUCH_CS, OUTPUT);
  cs(TOUCH_CS, false);
#ifdef TOUCH_IRQ
  pinMode(TOUCH_IRQ, INPUT_PULLUP);
#endif
  g_touch = lv_indev_create();
  lv_indev_set_type(g_touch, LV_INDEV_TYPE_POINTER);
  lv_indev_set_read_cb(g_touch, touch_cb);
  CalFile f;
  if (board::storage_read(kCalFile, &f, sizeof f) == sizeof f) {
    if (f.magic == kCalMagic) {
      g_cal = f.cal;
      g_have_cal = true;
    } else if (f.magic == kCalMagicV1) {
      // Landscape pixels at rotation 90 (lx, ly) → panel (ly, 319 − lx).
      const mesh::ui::TouchCal& o = f.cal;
      g_cal.a = o.d, g_cal.b = o.e, g_cal.c = o.f;
      g_cal.d = -o.a, g_cal.e = -o.b, g_cal.f = float(kHeight - 1) - o.c;
      g_have_cal = true;
    }
  }
#endif
#ifdef LCD_BL
  analogWrite(LCD_BL, 0);
#endif
  return true;
}

lv_indev_t* touch() { return g_touch; }
const char* error() { return g_error; }

void touch_cal_start() {
  lv_display_t* d = lv_display_get_default();
  mesh::ui::touch_cal_targets(lv_display_get_horizontal_resolution(d), lv_display_get_vertical_resolution(d),
                              g_cal_targets);
  for (int i = 0; i < 3; i++)
    to_panel(lv_display_get_rotation(d), g_cal_targets[i][0], g_cal_targets[i][1], &g_cal_phys[i][0], &g_cal_phys[i][1]);
  g_cal_sx = g_cal_sy = g_cal_n = 0;
  g_cal_step = 0;
  g_cal_on = g_touch != nullptr;
  if (!g_cal_on) g_cal_step = -1;
}

void touch_cal_cancel() {
  g_cal_on = false;
  g_cal_step = 3;
}

int touch_cal_step() { return g_cal_step; }

void touch_cal_target(int i, int* x, int* y) {
  *x = g_cal_targets[i < 0 ? 0 : i > 2 ? 2 : i][0];
  *y = g_cal_targets[i < 0 ? 0 : i > 2 ? 2 : i][1];
}

bool touch_calibrated() { return g_have_cal; }

int rotation() {
  if (!g_disp) return 0;
  switch (lv_display_get_rotation(g_disp)) {
    case LV_DISPLAY_ROTATION_0: return 0;
    case LV_DISPLAY_ROTATION_180: return 180;
    case LV_DISPLAY_ROTATION_270: return 270;
    default: return 90;
  }
}

void set_rotation(int deg) {
  if (!g_disp) return;
  lv_display_set_rotation(g_disp, rotation_of(deg));
  uint16_t d = uint16_t(rotation());
  board::storage_write(kRotationFile, &d, sizeof d);
  hwsim::resize(lv_display_get_horizontal_resolution(g_disp), lv_display_get_vertical_resolution(g_disp));
  lv_obj_invalidate(lv_screen_active());
}

void backlight(uint8_t level) {
#ifdef LCD_BL
  analogWrite(LCD_BL, level);
#else
  (void)level;
#endif
}

#else  // no screen in this build

bool begin() { return false; }
const char* error() { return "this build has no screen"; }
lv_indev_t* touch() { return nullptr; }
void backlight(uint8_t) {}
void touch_cal_start() {}
void touch_cal_cancel() {}
int touch_cal_step() { return -1; }
void touch_cal_target(int, int* x, int* y) { *x = *y = 0; }
bool touch_calibrated() { return false; }
int rotation() { return 0; }
void set_rotation(int) {}

#endif

}  // namespace display

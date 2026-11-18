#include "display.h"

#include <Arduino.h>
#include <SPI.h>
#include <esp_heap_caps.h>

#include "../hwsim/hwsim.h"
#include "src/drivers/display/ili9341/lv_ili9341.h"

namespace display {

#ifdef LCD_CS

namespace {

constexpr int kWidth = 240, kHeight = 320;
constexpr uint32_t kLcdHz = 40000000, kTouchHz = 2000000;
constexpr int kBufLines = 40;

// Raw XPT2046 readings at the screen edges (portrait x 0 and 239, y 0 and
// 319). On the owner's panel ("TFT SPI 240x320 V1.2") raw X falls as portrait
// x grows: with MIN < MAX, touches came out mirrored top to bottom in
// landscape (2026-11-18). A panel the other way round: swap the pair in
// build_flags (-DTOUCH_X_MIN=220 -DTOUCH_X_MAX=3880).
#ifndef TOUCH_X_MIN
#define TOUCH_X_MIN 3880
#define TOUCH_X_MAX 220
#endif
#ifndef TOUCH_Y_MIN
#define TOUCH_Y_MIN 260
#define TOUCH_Y_MAX 3860
#endif

SPIClass g_spi(HSPI);
lv_display_t* g_disp = nullptr;
const char* g_error = "";
lv_indev_t* g_touch = nullptr;

void cs(int pin, bool on) { digitalWrite(pin, on ? LOW : HIGH); }

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
#ifdef TOUCH_IRQ
  if (digitalRead(TOUCH_IRQ) == HIGH) return;  // not touched
#endif
  g_spi.beginTransaction(SPISettings(kTouchHz, MSBFIRST, SPI_MODE0));
  cs(TOUCH_CS, true);
  uint16_t z1 = touch_read(0xB1), z2 = touch_read(0xC1);
  int z = int(z1) + 4095 - int(z2);
  uint32_t sx = 0, sy = 0;
  for (int i = 0; i < 4; i++) {
    sx += touch_read(0xD1);
    sy += touch_read(0x91);
  }
  touch_read(0xD0);  // power down, keep PENIRQ on
  cs(TOUCH_CS, false);
  g_spi.endTransaction();
  if (z < 400) return;
  long x = map(long(sx / 4), TOUCH_X_MIN, TOUCH_X_MAX, 0, kWidth - 1);
  long y = map(long(sy / 4), TOUCH_Y_MIN, TOUCH_Y_MAX, 0, kHeight - 1);
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
  // Landscape, as the BlackBerry: 320 × 240. The panel rotates in hardware
  // (MADCTL), so flushed areas (and the phone's copy) are in landscape too.
  // LCD_ROTATION 270 if the panel is mounted the other way round.
#ifndef LCD_ROTATION
#define LCD_ROTATION 90
#endif
  lv_display_set_rotation(g_disp, LCD_ROTATION == 270 ? LV_DISPLAY_ROTATION_270
                                  : LCD_ROTATION == 0 ? LV_DISPLAY_ROTATION_0
                                                      : LV_DISPLAY_ROTATION_90);
  size_t bytes = size_t(kHeight) * kBufLines * 2;  // rows are up to 320 pixels wide
  // The panel is fed by the CPU (SPI writeBytes), so any memory works: PSRAM.
  // Internal RAM is what Wi-Fi, Bluetooth and TLS need: two 25 kB buffers
  // there left the relay's TLS handshake failing with "SSL - Memory
  // allocation failed" (owner, 2026-11-18).
  auto alloc = [](size_t n) {
    void* p = heap_caps_malloc(n, MALLOC_CAP_SPIRAM);
    return p ? p : heap_caps_malloc(n, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
  };
  void* a = alloc(bytes);
  void* b = alloc(bytes);
  if (!a || !b) {
    g_error = "no memory for the draw buffers";
    return false;
  }
  lv_display_set_buffers(g_disp, a, b, bytes, LV_DISPLAY_RENDER_MODE_PARTIAL);
#ifdef TOUCH_CS
  pinMode(TOUCH_CS, OUTPUT);
  cs(TOUCH_CS, false);
#ifdef TOUCH_IRQ
  pinMode(TOUCH_IRQ, INPUT_PULLUP);
#endif
  g_touch = lv_indev_create();
  lv_indev_set_type(g_touch, LV_INDEV_TYPE_POINTER);
  lv_indev_set_read_cb(g_touch, touch_cb);
#endif
#ifdef LCD_BL
  analogWrite(LCD_BL, 0);
#endif
  return true;
}

lv_indev_t* touch() { return g_touch; }
const char* error() { return g_error; }

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

#endif

}  // namespace display

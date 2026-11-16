#include "display.h"

#include <Arduino.h>
#include <SPI.h>
#include <esp_heap_caps.h>

#include "src/drivers/display/ili9341/lv_ili9341.h"

namespace display {

#ifdef LCD_CS

namespace {

constexpr int kWidth = 240, kHeight = 320;
constexpr uint32_t kLcdHz = 40000000, kTouchHz = 2000000;
constexpr int kBufLines = 40;

// Raw XPT2046 readings at the screen edges (portrait); change after a
// calibration on the real panel.
#ifndef TOUCH_X_MIN
#define TOUCH_X_MIN 220
#define TOUCH_X_MAX 3880
#define TOUCH_Y_MIN 260
#define TOUCH_Y_MAX 3860
#endif

SPIClass g_spi(HSPI);
lv_display_t* g_disp = nullptr;
lv_indev_t* g_touch = nullptr;

void cs(int pin, bool on) { digitalWrite(pin, on ? LOW : HIGH); }

void send_cmd(lv_display_t*, const uint8_t* cmd, size_t cmd_size, const uint8_t* param, size_t param_size) {
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
  if (!g_disp) return false;
  lv_display_set_rotation(g_disp, LV_DISPLAY_ROTATION_0);
  size_t bytes = size_t(kWidth) * kBufLines * 2;
  void* a = heap_caps_malloc(bytes, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
  void* b = heap_caps_malloc(bytes, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
  if (!a || !b) return false;
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

void backlight(uint8_t level) {
#ifdef LCD_BL
  analogWrite(LCD_BL, level);
#else
  (void)level;
#endif
}

#else  // no screen in this build

bool begin() { return false; }
lv_indev_t* touch() { return nullptr; }
void backlight(uint8_t) {}

#endif

}  // namespace display

#include "video.h"

#include <Arduino.h>
#include <JPEGDEC.h>  // bitbank2/JPEGDEC (platformio.ini lib_deps)
#include <esp_heap_caps.h>

#include <cstdio>
#include <cstring>
#include <new>

namespace video {
namespace {

constexpr int kMaxW = 320, kMaxH = 240;
JPEGDEC* g_jpeg = nullptr;  // ~20 kB of tables and buffers: PSRAM
uint16_t* g_px = nullptr;   // RGB565 (LVGL's byte order), kMaxW × kMaxH
lv_image_dsc_t g_dsc;
int g_w = 0, g_h = 0;
uint32_t g_ok = 0, g_bad = 0, g_last_us = 0;

int draw(JPEGDRAW* d) {
  for (int y = 0; y < d->iHeight; y++) {
    int row = d->y + y;
    if (row >= g_h) break;
    int n = d->iWidth;
    if (d->x + n > g_w) n = g_w - d->x;
    if (n <= 0) break;
    std::memcpy(g_px + size_t(row) * g_w + d->x, d->pPixels + size_t(y) * d->iWidth, size_t(n) * 2);
  }
  return 1;
}

}  // namespace

const lv_image_dsc_t* decode(const uint8_t* jpeg, size_t n, int max_w, int max_h) {
  if (!g_jpeg) {
    void* mem = heap_caps_malloc(sizeof(JPEGDEC), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    g_px = static_cast<uint16_t*>(heap_caps_malloc(size_t(kMaxW) * kMaxH * 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    if (!mem || !g_px) return nullptr;
    g_jpeg = new (mem) JPEGDEC();
  }
  if (max_w > kMaxW) max_w = kMaxW;
  if (max_h > kMaxH) max_h = kMaxH;
  uint32_t t0 = micros();
  if (n < 4 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 || !g_jpeg->openRAM(const_cast<uint8_t*>(jpeg), int(n), draw)) {
    g_bad++;
    return nullptr;
  }
  g_jpeg->setPixelType(RGB565_LITTLE_ENDIAN);
  int w = g_jpeg->getWidth(), h = g_jpeg->getHeight(), opt = 0;
  if (w > max_w * 2 || h > max_h * 2) {
    opt = JPEG_SCALE_QUARTER, w /= 4, h /= 4;
  } else if (w > max_w || h > max_h) {
    opt = JPEG_SCALE_HALF, w /= 2, h /= 2;
  }
  if (w > max_w) w = max_w;
  if (h > max_h) h = max_h;
  g_w = w, g_h = h;
  bool ok = w > 0 && h > 0 && g_jpeg->decode(0, 0, opt);
  g_jpeg->close();
  g_last_us = micros() - t0;
  if (!ok) {
    g_bad++;
    return nullptr;
  }
  g_ok++;
  std::memset(&g_dsc, 0, sizeof g_dsc);
  g_dsc.header.magic = LV_IMAGE_HEADER_MAGIC;
  g_dsc.header.cf = LV_COLOR_FORMAT_RGB565;
  g_dsc.header.w = uint32_t(w);
  g_dsc.header.h = uint32_t(h);
  g_dsc.header.stride = uint32_t(w) * 2;
  g_dsc.data = reinterpret_cast<const uint8_t*>(g_px);
  g_dsc.data_size = uint32_t(w) * uint32_t(h) * 2;
  return &g_dsc;
}

void status(char* out, size_t cap) {
  snprintf(out, cap, "decoded %lu, failed %lu, last %lu ms (%dx%d)", (unsigned long)g_ok, (unsigned long)g_bad,
           (unsigned long)(g_last_us / 1000), g_w, g_h);
}

}  // namespace video

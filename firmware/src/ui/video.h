// The far side's camera on the call screen: a JPEG frame (MJPEG, calls.md
// §7b) decoded with JPEGDEC into an RGB565 image in PSRAM that LVGL draws.
// Main loop (the UI's thread) only.
#pragma once

#include <cstddef>
#include <cstdint>

#include <lvgl.h>

namespace video {

// Decodes jpeg into the image (scaled down by 2 or 4 when it is larger than
// max_w × max_h). Null if it isn't a JPEG this decoder takes.
const lv_image_dsc_t* decode(const uint8_t* jpeg, size_t n, int max_w, int max_h);
// For the log: frames decoded, failed, the last decode time.
void status(char* out, size_t cap);

}  // namespace video

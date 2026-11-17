// The handheld's screen and touch panel (docs/handheld-proto.md §4) for LVGL:
// ILI9341 240×320, used in landscape (320×240), on its own SPI bus, backlight PWM, XPT2046 resistive touch
// on the same bus. Nothing here runs without LCD_CS (the plain Mava board).
#pragma once

#include <cstdint>

#include <lvgl.h>

namespace display {

// Starts SPI, the panel and LVGL's display; false if this build has no screen.
bool begin();
// Pointer input device for LVGL (null without a touch panel).
lv_indev_t* touch();
void backlight(uint8_t level);  // 0 = off
const char* error();            // why begin() failed ("" if it didn't)

}  // namespace display

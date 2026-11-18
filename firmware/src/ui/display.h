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

// Touch calibration (Settings): while it runs, touches don't reach LVGL;
// the user taps three crosses (touch_cal_target, screen pixels). step():
// 0–2 the cross to tap, 3 done (saved, in use from now on), -1 the taps made
// no usable map (start again).
void touch_cal_start();
void touch_cal_cancel();
int touch_cal_step();
void touch_cal_target(int i, int* x, int* y);
bool touch_calibrated();  // a saved calibration is in use

}  // namespace display

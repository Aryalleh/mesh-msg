// Touch calibration (Settings → Calibrate touch): three crosses tapped on the
// screen give an affine map from raw resistive-panel readings to screen
// pixels. It takes in any rotation, mirroring or swapped axes of the panel,
// so one firmware fits every display module. Pure C++: host-tested.
#pragma once

#include <cstdint>

namespace mesh {
namespace ui {

// screen x = a·rx + b·ry + c, screen y = d·rx + e·ry + f
struct TouchCal {
  float a = 0, b = 0, c = 0, d = 0, e = 0, f = 0;
};

// Where the three crosses go on a w × h screen (spread out, not in a line).
void touch_cal_targets(int w, int h, int out[3][2]);
// From the raw readings at the three targets; false when the taps don't make
// a usable map (taps in a line, a panel that didn't move, a far-off reading).
bool touch_cal_solve(const int raw[3][2], const int screen[3][2], TouchCal& out);
void touch_cal_apply(const TouchCal& c, int rx, int ry, int* x, int* y);

}  // namespace ui
}  // namespace mesh

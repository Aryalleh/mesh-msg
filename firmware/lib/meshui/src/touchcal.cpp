#include "touchcal.h"

#include <cmath>

namespace mesh {
namespace ui {

void touch_cal_targets(int w, int h, int out[3][2]) {
  out[0][0] = w / 10, out[0][1] = h / 10;          // top left
  out[1][0] = w - w / 10, out[1][1] = h / 2;       // right middle
  out[2][0] = w / 2, out[2][1] = h - h / 10;       // bottom middle
}

bool touch_cal_solve(const int raw[3][2], const int screen[3][2], TouchCal& out) {
  const double x1 = raw[0][0], y1 = raw[0][1], x2 = raw[1][0], y2 = raw[1][1], x3 = raw[2][0], y3 = raw[2][1];
  const double det = x1 * (y2 - y3) - x2 * (y1 - y3) + x3 * (y1 - y2);
  // Taps in a line or on one spot: the raw triangle must be a fair size
  // (12-bit readings: a touch panel spans thousands).
  if (std::fabs(det) < 100000.0) return false;
  auto solve = [&](double s1, double s2, double s3, float& a, float& b, float& c) {
    a = float((s1 * (y2 - y3) - s2 * (y1 - y3) + s3 * (y1 - y2)) / det);
    b = float((x1 * (s2 - s3) - x2 * (s1 - s3) + x3 * (s1 - s2)) / det);
    c = float((x1 * (y2 * s3 - y3 * s2) - x2 * (y1 * s3 - y3 * s1) + x3 * (y1 * s2 - y2 * s1)) / det);
  };
  TouchCal t;
  solve(screen[0][0], screen[1][0], screen[2][0], t.a, t.b, t.c);
  solve(screen[0][1], screen[1][1], screen[2][1], t.d, t.e, t.f);
  // A real panel: each screen axis follows mostly one raw axis, a few pixels
  // per hundred raw counts (a 12-bit span over 240–320 px is ~0.06–0.1 px).
  double sx = std::sqrt(double(t.a) * t.a + double(t.b) * t.b), sy = std::sqrt(double(t.d) * t.d + double(t.e) * t.e);
  if (sx < 0.02 || sx > 1.0 || sy < 0.02 || sy > 1.0) return false;
  out = t;
  return true;
}

void touch_cal_apply(const TouchCal& c, int rx, int ry, int* x, int* y) {
  *x = int(std::lround(c.a * rx + c.b * ry + c.c));
  *y = int(std::lround(c.d * rx + c.e * ry + c.f));
}

}  // namespace ui
}  // namespace mesh

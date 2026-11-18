#include <cstdlib>
// Touch keyboard layouts: rows add up, every Persian letter and a–z once,
// page changes.
#include <cstring>
#include <set>
#include <string>

#include "check.h"
#include "osk.h"
#include "touchcal.h"

namespace o = mesh::osk;

namespace {

// Calibration takes in any panel: flipped, swapped axes, a little rotated.
void touch_calibration() {
  using mesh::ui::TouchCal;
  int targets[3][2];
  mesh::ui::touch_cal_targets(320, 240, targets);
  auto check_panel = [&](int (*raw)(int, int, int)) {
    int r[3][2];
    for (int i = 0; i < 3; i++) r[i][0] = raw(targets[i][0], targets[i][1], 0), r[i][1] = raw(targets[i][0], targets[i][1], 1);
    TouchCal c;
    if (!mesh::ui::touch_cal_solve(r, targets, c)) return false;
    // Anywhere on the screen maps back within a pixel.
    for (int x = 0; x < 320; x += 37)
      for (int y = 0; y < 240; y += 29) {
        int gx, gy;
        mesh::ui::touch_cal_apply(c, raw(x, y, 0), raw(x, y, 1), &gx, &gy);
        if (std::abs(gx - x) > 1 || std::abs(gy - y) > 1) return false;
      }
    return true;
  };
  // Straight; both axes mirrored (the owner's panel); swapped axes; swapped + one mirrored.
  CHECK(check_panel([](int x, int y, int a) { return a == 0 ? 200 + x * 11 : 300 + y * 14; }));
  CHECK(check_panel([](int x, int y, int a) { return a == 0 ? 3900 - x * 11 : 3800 - y * 14; }));
  CHECK(check_panel([](int x, int y, int a) { return a == 0 ? 250 + y * 15 : 220 + x * 11; }));
  CHECK(check_panel([](int x, int y, int a) { return a == 0 ? 3850 - y * 15 : 220 + x * 11; }));
  // Bad taps: all on one spot, or in a line.
  int same[3][2] = {{2000, 2000}, {2010, 1990}, {1995, 2005}}, line[3][2] = {{500, 500}, {2000, 2000}, {3500, 3500}};
  TouchCal c;
  CHECK(!mesh::ui::touch_cal_solve(same, targets, c) && !mesh::ui::touch_cal_solve(line, targets, c));
}

}  // namespace

void run_osk() {
  touch_calibration();
  const o::Page pages[] = {o::Page::Persian, o::Page::Latin, o::Page::LatinUpper, o::Page::SymbolsPersian,
                           o::Page::SymbolsLatin};
  for (o::Page p : pages) {
    const o::Layout& l = o::layout(p);
    size_t sum = 0;
    for (size_t r = 0; r < l.row_count; r++) sum += l.rows[r];
    CHECKC(sum == l.count && l.count <= o::kMaxButtons, "rows add up");
    bool back = false, enter = false;
    for (size_t i = 0; i < l.count; i++) {
      back |= l.buttons[i].kind == o::Kind::Backspace;
      enter |= l.buttons[i].kind == o::Kind::Enter;
      CHECK(l.buttons[i].width >= 1 && l.buttons[i].width <= 4 && l.buttons[i].label && *l.buttons[i].label);
      if (l.buttons[i].kind == o::Kind::Text) CHECK(l.buttons[i].text != nullptr);
    }
    CHECK(back && enter);
  }
  const char* fa[] = {"ا", "ب", "پ", "ت", "ث", "ج", "چ", "ح", "خ", "د", "ذ", "ر", "ز", "ژ", "س", "ش",
                      "ص", "ض", "ط", "ظ", "ع", "غ", "ف", "ق", "ک", "گ", "ل", "م", "ن", "و", "ه", "ی"};
  std::multiset<std::string> got;
  const o::Layout& pl = o::layout(o::Page::Persian);
  for (size_t i = 0; i < pl.count; i++)
    if (pl.buttons[i].kind == o::Kind::Text) got.insert(pl.buttons[i].text);
  for (const char* f : fa) CHECKC(got.count(f) == 1, f);
  CHECK(got.count("‌") == 1);  // ZWNJ
  std::multiset<std::string> lat;
  const o::Layout& ll = o::layout(o::Page::Latin);
  for (size_t i = 0; i < ll.count; i++)
    if (ll.buttons[i].kind == o::Kind::Text) lat.insert(ll.buttons[i].text);
  for (char c = 'a'; c <= 'z'; c++) CHECK(lat.count(std::string(1, c)) == 1);

  auto find = [](o::Page p, o::Kind k) {
    const o::Layout& l = o::layout(p);
    for (size_t i = 0; i < l.count; i++)
      if (l.buttons[i].kind == k) return i;
    return l.count;
  };
  CHECK(o::press(o::Page::Persian, find(o::Page::Persian, o::Kind::Language)).next == o::Page::Latin);
  CHECK(o::press(o::Page::Latin, find(o::Page::Latin, o::Kind::Language)).next == o::Page::Persian);
  CHECK(o::press(o::Page::Latin, find(o::Page::Latin, o::Kind::Shift)).next == o::Page::LatinUpper);
  o::Press cap = o::press(o::Page::LatinUpper, 0);
  CHECK(cap.kind == o::Kind::Text && std::strcmp(cap.text, "Q") == 0 && cap.next == o::Page::Latin);
  CHECK(o::press(o::Page::Persian, find(o::Page::Persian, o::Kind::Symbols)).next == o::Page::SymbolsPersian);
  CHECK(o::press(o::Page::SymbolsPersian, find(o::Page::SymbolsPersian, o::Kind::Letters)).next ==
        o::Page::Persian);
  CHECK(o::press(o::Page::SymbolsLatin, find(o::Page::SymbolsLatin, o::Kind::Letters)).next == o::Page::Latin);
  o::Press first = o::press(o::Page::Persian, 0);
  CHECK(first.kind == o::Kind::Text && std::strcmp(first.text, "ض") == 0 && first.next == o::Page::Persian);
  CHECK(o::press(o::Page::Persian, 999).kind == o::Kind::Text);
}

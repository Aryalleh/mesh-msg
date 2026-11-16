// Handheld keyboard: the matrix carries every Persian letter and QWERTY
// exactly once, debouncing, Alt + Shift switches language, layers.
#include <cstring>
#include <set>
#include <vector>

#include "check.h"
#include "keys.h"

namespace k = mesh::keys;

namespace {

struct Rig {
  k::Matrix m;
  k::Layout l;
  uint8_t scan[k::kCols] = {};
  uint32_t now = 0;
  std::vector<k::Output> out;

  void step(uint32_t ms) {
    for (uint32_t t = 0; t < ms; t += 5) {
      now += 5;
      k::Event ev[8];
      size_t n = m.update(scan, now, ev, 8);
      for (size_t i = 0; i < n; i++) {
        k::Output o = l.on_event(ev[i]);
        if (o.action != k::Action::None) out.push_back(o);
      }
    }
  }
  void set(k::Pos p, bool down) {
    if (down)
      scan[p.col] = uint8_t(scan[p.col] | 1u << p.row);
    else
      scan[p.col] = uint8_t(scan[p.col] & ~(1u << p.row));
  }
  void tap(k::Pos p) {
    set(p, true);
    step(30);
    set(p, false);
    step(30);
  }
};

}  // namespace

void run_keys() {
  // 32 Persian letters and a–z, each exactly once; every position used.
  const char32_t fa[] = {0x0627, 0x0628, 0x067E, 0x062A, 0x062B, 0x062C, 0x0686, 0x062D, 0x062E, 0x062F, 0x0630,
                         0x0631, 0x0632, 0x0698, 0x0633, 0x0634, 0x0635, 0x0636, 0x0637, 0x0638, 0x0639, 0x063A,
                         0x0641, 0x0642, 0x06A9, 0x06AF, 0x0644, 0x0645, 0x0646, 0x0648, 0x0647, 0x06CC};
  std::multiset<char32_t> letters;
  std::multiset<char> latin;
  std::set<k::Key> specials;
  int none = 0;
  for (int r = 0; r < k::kRows; r++)
    for (int c = 0; c < k::kCols; c++) {
      const k::KeyDef& d = k::kMatrix[r][c];
      if (d.key == k::Key::Char) {
        letters.insert(d.persian);
        latin.insert(d.latin);
      } else if (d.key == k::Key::None) {
        none++;
      } else {
        CHECK(specials.insert(d.key).second);
      }
    }
  CHECK(none == 0 && letters.size() == 32 && specials.size() == 17);
  for (char32_t f : fa) CHECK(letters.count(f) == 1);
  for (char ch = 'a'; ch <= 'z'; ch++) CHECK(latin.count(ch) == 1);

  // Debounce: a 10 ms bounce is ignored, a held key gives one press.
  Rig r;
  k::Pos q = k::find_latin('q');
  r.set(q, true);
  r.step(5);
  r.set(q, false);
  r.step(40);
  CHECK(r.out.empty());
  r.tap(q);
  CHECK(r.out.size() == 1 && r.out[0].action == k::Action::Text && r.out[0].cp == 0x0636);  // ض

  // Persian layer: Shift extras, Shift + Space = ZWNJ, Alt digits are Persian.
  r.out.clear();
  k::Pos shift = k::find(k::Key::Shift), alt = k::find(k::Key::Alt);
  r.set(shift, true);
  r.step(30);
  r.tap(k::find_latin('h'));
  r.tap(k::find(k::Key::Space));
  r.set(shift, false);
  r.step(30);
  r.set(alt, true);
  r.step(30);
  r.tap(k::find_latin('w'));
  r.set(alt, false);
  r.step(30);
  CHECK(r.out.size() == 3 && r.out[0].cp == 0x0622 && r.out[1].cp == k::kZwnj && r.out[2].cp == 0x06F2);

  // Alt + Shift → Latin: QWERTY, capitals, ASCII digits; no stray symbol
  // while both are still held; back with Alt + Shift again.
  r.out.clear();
  r.set(alt, true);
  r.step(30);
  r.set(shift, true);
  r.step(30);
  r.tap(q);  // swallowed by the combo
  r.set(shift, false);
  r.set(alt, false);
  r.step(30);
  CHECK(r.out.size() == 1 && r.out[0].action == k::Action::LanguageChanged && !r.l.persian());
  r.out.clear();
  r.tap(q);
  r.set(shift, true);
  r.step(30);
  r.tap(q);
  r.set(shift, false);
  r.step(30);
  r.set(alt, true);
  r.step(30);
  r.tap(k::find_latin('p'));
  r.set(alt, false);
  r.step(30);
  CHECK(r.out.size() == 3 && r.out[0].cp == 'q' && r.out[1].cp == 'Q' && r.out[2].cp == '0');
  r.out.clear();
  r.set(shift, true);
  r.step(30);
  r.set(alt, true);
  r.step(30);
  r.set(alt, false);
  r.set(shift, false);
  r.step(30);
  CHECK(r.l.persian() && r.out.size() == 1);

  // Phone keys and the setup switch (held for as long as it is latched).
  r.out.clear();
  r.tap(k::find(k::Key::Call));
  r.tap(k::find(k::Key::End));
  r.tap(k::find(k::Key::Ok));
  CHECK(r.out.size() == 3 && r.out[0].action == k::Action::Call && r.out[1].action == k::Action::End &&
        r.out[2].action == k::Action::Ok);
  k::Pos setup = k::find(k::Key::Setup);
  r.set(setup, true);
  r.step(30);
  CHECK(r.m.down(k::Key::Setup));
  r.tap(q);  // other keys still work while the switch is on (diodes: no ghosting)
  CHECK(r.m.down(k::Key::Setup) && r.out.size() == 4);
}

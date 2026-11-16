// Handheld keyboard: the matrix carries every Persian letter and QWERTY
// exactly once, debouncing, Alt + Shift switches language, layers.
#include <cstring>
#include <set>
#include <vector>

#include "check.h"
#include "kbproto.h"
#include "keys.h"

namespace k = mesh::keys;

namespace {

struct Rig {
  const k::Keyboard& kb;
  k::Matrix m;
  k::Layout l;
  uint8_t scan[k::kMaxCols] = {};
  explicit Rig(k::Variant v = k::Variant::Matrix7x7) : kb(k::keyboard(v)), m(kb), l(kb) {}
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

// Every Persian letter and a–z exactly once (Persian letters may be direct
// or on Alt); every position wired; no special key twice.
void check_layout(const k::Keyboard& kb, size_t specials_want, int none_want = 0) {
  const char32_t fa[] = {0x0627, 0x0628, 0x067E, 0x062A, 0x062B, 0x062C, 0x0686, 0x062D, 0x062E, 0x062F, 0x0630,
                         0x0631, 0x0632, 0x0698, 0x0633, 0x0634, 0x0635, 0x0636, 0x0637, 0x0638, 0x0639, 0x063A,
                         0x0641, 0x0642, 0x06A9, 0x06AF, 0x0644, 0x0645, 0x0646, 0x0648, 0x0647, 0x06CC};
  std::multiset<char32_t> letters;
  std::multiset<char> latin;
  std::multiset<k::Key> specials;
  int none = 0;
  for (int r = 0; r < kb.rows; r++)
    for (int c = 0; c < kb.cols; c++) {
      const k::KeyDef& d = kb.at(k::Pos{int8_t(r), int8_t(c)});
      if (d.key == k::Key::Char) {
        letters.insert(d.persian);
        if (d.persian_alt >= 0x0600) letters.insert(d.persian_alt);
        latin.insert(d.latin);
      } else if (d.key == k::Key::None) {
        none++;
      } else {
        specials.insert(d.key);
      }
    }
  CHECK(none == none_want && specials.size() == specials_want);
  for (char32_t f : fa) CHECKC(letters.count(f) == 1, "Persian letter");
  for (char ch = 'a'; ch <= 'z'; ch++) CHECKC(latin.count(ch) == 1, "Latin letter");
  for (k::Key x : {k::Key::Space, k::Key::Backspace, k::Key::Enter, k::Key::Alt, k::Key::Call, k::Key::End,
                   k::Key::Menu, k::Key::Back, k::Key::VolUp, k::Key::VolDown})
    CHECKC(specials.count(x) == 1, "special key");
}

void blackberry() {
  const k::Keyboard& kb = k::keyboard(k::Variant::BB9900);
  CHECK(kb.rows == 7 && kb.cols == 6);
  check_layout(kb, 13, 1);  // 42 positions − 26 letters − $ − 0 − (0,0) free (two Shifts)
  const k::Keyboard& tp = k::keyboard(k::Variant::BB9900Trackpad);
  check_layout(tp, 14);
  CHECK(tp.at(k::Pos{0, 0}).key == k::Key::TrackClick && tp.at(k::Pos{6, 5}).key == k::Key::Enter);
  {
    Rig t(k::Variant::BB9900Trackpad);
    t.tap(tp.find(k::Key::TrackClick));
    CHECK(t.out.size() == 1 && t.out[0].action == k::Action::Ok);
  }
  Rig r(k::Variant::BB9900);
  // Direct letters by position; و and ک on $ and 0; Alt letters.
  r.tap(kb.find_latin('q'));
  r.tap(kb.find_latin('$'));
  r.tap(kb.find_latin('0'));
  k::Pos alt = kb.find(k::Key::Alt);
  r.tap(alt);  // one-shot, as on a BlackBerry
  r.tap(kb.find_latin('p'));
  r.tap(kb.find_latin('p'));  // the one-shot is used up
  CHECK(r.out.size() == 5 && r.out[0].cp == 0x0636 && r.out[1].cp == 0x0648 && r.out[2].cp == 0x06A9 &&
        r.out[3].cp == 0x062C && r.out[4].cp == 0x062D);
  // Tapped Alt then Shift: Latin. QWERTZ legends: the key right of T types z.
  r.out.clear();
  r.tap(alt);
  r.tap(kb.find(k::Key::Shift));
  CHECK(r.out.size() == 1 && r.out[0].action == k::Action::LanguageChanged && !r.l.persian());
  r.out.clear();
  r.tap(kb.find_latin('y'));
  r.tap(alt);
  r.tap(kb.find_latin('w'));
  CHECK(r.out.size() == 2 && r.out[0].cp == 'z' && r.out[1].cp == '1');
  // Sym held: E S D F are arrows, Enter is OK; Sym tapped: symbol picker.
  r.out.clear();
  k::Pos sym = kb.find(k::Key::Sym);
  r.set(sym, true);
  r.step(30);
  r.tap(kb.find_latin('e'));
  r.tap(kb.find_latin('f'));
  r.tap(kb.find(k::Key::Enter));
  r.set(sym, false);
  r.step(30);
  r.tap(sym);
  CHECK(r.out.size() == 4 && r.out[0].action == k::Action::Up && r.out[1].action == k::Action::Right &&
        r.out[2].action == k::Action::Ok && r.out[3].action == k::Action::Symbols);
}

void run_keys() {
  const k::Keyboard& kb7 = k::keyboard(k::Variant::Matrix7x7);
  check_layout(kb7, 17);
  blackberry();

  // Debounce: a 10 ms bounce is ignored, a held key gives one press.
  Rig r;
  k::Pos q = r.kb.find_latin('q');
  r.set(q, true);
  r.step(5);
  r.set(q, false);
  r.step(40);
  CHECK(r.out.empty());
  r.tap(q);
  CHECK(r.out.size() == 1 && r.out[0].action == k::Action::Text && r.out[0].cp == 0x0636);  // ض

  // Persian layer: Shift extras, Shift + Space = ZWNJ, Alt digits are Persian.
  r.out.clear();
  k::Pos shift = r.kb.find(k::Key::Shift), alt = r.kb.find(k::Key::Alt);
  r.set(shift, true);
  r.step(30);
  r.tap(r.kb.find_latin('h'));
  r.tap(r.kb.find(k::Key::Space));
  r.set(shift, false);
  r.step(30);
  r.set(alt, true);
  r.step(30);
  r.tap(r.kb.find_latin('w'));
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
  r.tap(r.kb.find_latin('p'));
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

  // Phone keys; a held key does not block others (diodes: no ghosting).
  r.out.clear();
  r.tap(r.kb.find(k::Key::Call));
  r.tap(r.kb.find(k::Key::End));
  r.tap(r.kb.find(k::Key::Ok));
  CHECK(r.out.size() == 3 && r.out[0].action == k::Action::Call && r.out[1].action == k::Action::End &&
        r.out[2].action == k::Action::Ok);
  k::Pos vol = r.kb.find(k::Key::VolUp);
  r.set(vol, true);
  r.step(30);
  CHECK(r.m.down(k::Key::VolUp));
  r.tap(q);
  CHECK(r.m.down(k::Key::VolUp) && r.out.size() == 5);
  r.tap(r.kb.find(k::Key::Sym));
  CHECK(r.out.size() == 6 && r.out[5].action == k::Action::Symbols);
}

// Keyboard module protocol: ID, event FIFO (order, overflow, popped by
// reads), saturated motion, backlight writes.
void run_kbproto() {
  namespace kk = mesh::keys;
  kk::ModuleState m(kk::Variant::BB9900Trackpad);
  uint8_t b[32];
  kk::Variant v;
  CHECK(m.read(kk::kRegId, b, sizeof b) == 4 && kk::parse_id(b, 4, &v) && v == kk::Variant::BB9900Trackpad);
  b[1] = 'X';
  CHECK(!kk::parse_id(b, 4, &v));
  CHECK(!m.pending());

  for (int i = 0; i < 20; i++) m.push(kk::Event{kk::Pos{int8_t(i % 7), int8_t(i % 6)}, i % 2 == 0});
  CHECK(m.pending() && m.queued() == 20);
  kk::Event ev[16];
  size_t n = m.read(kk::kRegEvents, b, sizeof b);
  size_t k = kk::parse_events(b, n, ev, 16);
  CHECK(n == 16 && k == 15 && ev[0].pos.row == 0 && ev[0].pressed && ev[3].pos.row == 3 && ev[3].pos.col == 3 &&
        !ev[3].pressed);
  n = m.read(kk::kRegEvents, b, sizeof b);
  k = kk::parse_events(b, n, ev, 16);
  CHECK(k == 5 && ev[0].pos.row == 15 % 7 && ev[0].pos.col == 15 % 6 && m.queued() == 0);
  n = m.read(kk::kRegEvents, b, sizeof b);
  CHECK(n == 1 && b[0] == 0);

  // Overflow keeps the newest kFifo events.
  for (int i = 0; i < int(kk::kFifo) + 10; i++) m.push(kk::Event{kk::Pos{int8_t(i % 7), 0}, true});
  CHECK(m.queued() == kk::kFifo);
  m.read(kk::kRegEvents, b, sizeof b);
  CHECK(kk::decode_event(b[1]).pos.row == 10 % 7);
  while (m.queued()) m.read(kk::kRegEvents, b, sizeof b);

  m.motion(100, -5);
  m.motion(100, -3);
  CHECK(m.pending() && m.read(kk::kRegTrack, b, sizeof b) == 2 && int8_t(b[0]) == 127 && int8_t(b[1]) == -8);
  CHECK(!m.pending());
  uint8_t w[2] = {kk::kRegBacklight, 200};
  CHECK(m.write(w, 2) == 200 && m.write(w, 1) == -1);
}

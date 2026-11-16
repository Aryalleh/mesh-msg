// Handheld keyboard (docs/handheld-proto.md §4): a 7 × 7 diode matrix on 14
// GPIOs, 49 positions — 32 Persian letters on the ISIRI 9147 positions (so
// the Latin layer is QWERTY), space, backspace, enter, shift, alt, a
// navigation cluster, call / end, menu / back, volume, and the latching
// setup switch.
//
//   Matrix   debounces raw scans (one byte of pressed rows per column) and
//            reports presses and releases.
//   Layout   turns presses into text or actions: Persian or Latin layer,
//            Alt + Shift switches between them, Shift for capitals and Persian
//            extras (آ ئ ؤ ء, Shift + Space = ZWNJ), Alt for digits and symbols.
//
// Pure C++17, no Arduino: the board scans the GPIOs, the host tests drive it.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace keys {

constexpr int kRows = 7;
constexpr int kCols = 7;
constexpr uint32_t kDebounceMs = 15;

enum class Key : uint8_t {
  None,
  Char,  // letter / punctuation key: see KeyDef
  Space,
  Backspace,
  Enter,
  Shift,
  Alt,
  Up,
  Down,
  Left,
  Right,
  Ok,
  Call,  // answer / dial (green)
  End,   // hang up / reject (red)
  Menu,  // left soft key
  Back,  // right soft key
  VolUp,
  VolDown,
  Setup,  // latching setup switch (hotspot + panel), not a key
};

struct KeyDef {
  Key key = Key::None;
  char latin = 0;            // QWERTY, lower case
  char32_t persian = 0;      // Persian layer
  char32_t persian_shift = 0;  // Shift in the Persian layer (0 = same as persian)
  char latin_alt = 0;        // Alt layer (digits / symbols); 0 = none
  char32_t persian_alt = 0;  // Alt in the Persian layer (Persian digits / punctuation)
};

// The wiring: kMatrix[row][col] is the key between row line R<row> and
// column line C<col> (diode cathode towards the column).
extern const KeyDef kMatrix[kRows][kCols];

struct Pos {
  int8_t row = -1, col = -1;
  bool valid() const { return row >= 0; }
};
Pos find(Key k);
Pos find_latin(char c);

struct Event {
  Pos pos;
  bool pressed = false;
};

class Matrix {
 public:
  // scan[c]: bit r set = the key at (r, c) reads pressed. Returns the number
  // of events written to out (debounced changes).
  size_t update(const uint8_t scan[kCols], uint32_t now_ms, Event* out, size_t cap);
  bool down(Pos p) const { return p.valid() && (state_[p.col] >> p.row & 1); }
  bool down(Key k) const { return down(find(k)); }

 private:
  uint8_t state_[kCols] = {};
  uint8_t last_raw_[kCols] = {};
  uint32_t since_[kCols] = {};
};

enum class Action : uint8_t {
  None,
  Text,  // codepoint in Output::cp
  Backspace,
  Enter,
  Up,
  Down,
  Left,
  Right,
  Ok,
  Call,
  End,
  Menu,
  Back,
  VolUp,
  VolDown,
  LanguageChanged,  // Alt + Shift
};

struct Output {
  Action action = Action::None;
  char32_t cp = 0;
};

class Layout {
 public:
  // Feed every Matrix event in order.
  Output on_event(const Event& e);
  bool persian() const { return persian_; }
  void set_persian(bool p) { persian_ = p; }

 private:
  bool persian_ = true;
  bool shift_ = false, alt_ = false;
  bool combo_ = false;  // Alt + Shift used as a combo: no symbols until both released
};

constexpr char32_t kZwnj = 0x200C;

}  // namespace keys
}  // namespace mesh

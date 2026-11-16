// Handheld keyboards (docs/handheld-proto.md §4, §7):
//
//   Matrix7x7  version A: our own 7 × 7 diode matrix on 14 GPIOs — 32 Persian
//              letters on their ISIRI 9147 positions (QWERTY Latin layer),
//              arrows + OK, call / end, menu / back, volume, setup switch.
//   BB9900     versions B and C: an original BlackBerry Bold 9900 keyboard,
//              7 rows × 6 columns on its flex (39 keys), plus three positions
//              we add on our board: setup switch (B) or trackpad click (C),
//              Vol+ and Vol−. 26 letter keys carry the ISIRI letters; و and ک
//              sit on the $ and 0 keys; ج چ گ ژ are Alt + ح ع م ذ.
//
//   Matrix   debounces raw scans (one byte of pressed rows per column) and
//            reports presses and releases.
//   Layout   turns presses into text or actions: Persian or Latin layer,
//            Alt + Shift switches between them, Shift for capitals and Persian
//            extras (آ ئ ؤ ة ء, Shift + Space = ZWNJ), Alt for digits and
//            symbols. Alt and Shift work held or tapped (one-shot, as on a
//            BlackBerry). On the BB9900, holding Sym turns E S D F into arrows
//            and Enter into OK; tapping it asks for the symbol picker.
//
// Pure C++17, no Arduino: the board scans the GPIOs, the host tests drive it.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace keys {

constexpr int kMaxRows = 8;
constexpr int kMaxCols = 8;
constexpr uint32_t kDebounceMs = 15;

enum class Key : uint8_t {
  None,
  Char,  // letter / punctuation key: see KeyDef
  Space,
  Backspace,
  Enter,
  Shift,
  Alt,
  Sym,  // BB9900: tap = symbol picker, hold = navigation layer
  Up,
  Down,
  Left,
  Right,
  Ok,
  Call,  // answer / dial (green)
  End,   // hang up / reject (red)
  Menu,  // left soft key (BB: the BlackBerry key)
  Back,  // right soft key
  VolUp,
  VolDown,
  TrackClick,  // trackpad press = OK
  Setup,       // latching setup switch (hotspot + panel), not a key
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
  Symbols,          // open the symbol / digit picker
  LanguageChanged,  // Alt + Shift
};

struct KeyDef {
  Key key = Key::None;
  char latin = 0;              // QWERTY, lower case
  char32_t persian = 0;        // Persian layer
  char32_t persian_shift = 0;  // Shift in the Persian layer (0 = same as persian)
  char latin_alt = 0;          // Alt layer (digits / symbols); 0 = none
  char32_t persian_alt = 0;    // Alt in the Persian layer (letters, Persian digits, punctuation)
  Action nav = Action::None;   // BB9900: action while Sym is held
};

struct Pos {
  int8_t row = -1, col = -1;
  bool valid() const { return row >= 0; }
};

// BB9900Trackpad: version C — the trackpad click takes (0,0), the setup
// switch moves to its own GPIO.
enum class Variant : uint8_t { Matrix7x7, BB9900, BB9900Trackpad };

struct Keyboard {
  Variant variant;
  int rows, cols;
  const KeyDef* defs;  // rows × cols, row-major
  // The Latin legends are QWERTZ (German 9900 keypads): Y and Z swap.
  bool qwertz;

  const KeyDef& at(Pos p) const { return defs[p.row * cols + p.col]; }
  Pos find(Key k) const;
  Pos find_latin(char c) const;
};

// The wiring: keyboard(v).at({row, col}) is the key between row line R<row>
// and column line C<col>.
const Keyboard& keyboard(Variant v);

struct Event {
  Pos pos;
  bool pressed = false;
};

class Matrix {
 public:
  explicit Matrix(const Keyboard& kb) : kb_(kb) {}
  // scan[c] for c < kb.cols: bit r set = the key at (r, c) reads pressed.
  // Returns the number of events written to out (debounced changes).
  size_t update(const uint8_t* scan, uint32_t now_ms, Event* out, size_t cap);
  bool down(Pos p) const { return p.valid() && (state_[p.col] >> p.row & 1); }
  bool down(Key k) const { return down(kb_.find(k)); }

 private:
  const Keyboard& kb_;
  uint8_t state_[kMaxCols] = {};
  uint8_t last_raw_[kMaxCols] = {};
  uint32_t since_[kMaxCols] = {};
};

struct Output {
  Action action = Action::None;
  char32_t cp = 0;
};

class Layout {
 public:
  explicit Layout(const Keyboard& kb) : kb_(kb) {}
  // Feed every Matrix event in order.
  Output on_event(const Event& e);
  bool persian() const { return persian_; }
  void set_persian(bool p) { persian_ = p; }

 private:
  Output modifier(Key k, bool pressed);
  Output press(const KeyDef& d);

  const Keyboard& kb_;
  bool persian_ = true;
  bool shift_ = false, alt_ = false, sym_ = false;  // held
  bool shift_once_ = false, alt_once_ = false;      // tapped: applies to the next key
  bool used_ = false;    // a key was pressed while a modifier was held
  bool combo_ = false;   // Alt + Shift switched language: swallow typing until both are up
  bool sym_used_ = false;
};

constexpr char32_t kZwnj = 0x200C;

}  // namespace keys
}  // namespace mesh

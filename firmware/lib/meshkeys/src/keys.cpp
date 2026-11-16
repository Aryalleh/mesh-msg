#include "keys.h"

namespace mesh {
namespace keys {

namespace {

constexpr KeyDef C(char latin, char32_t fa, char la = 0, char32_t fa_alt = 0, char32_t fa_shift = 0) {
  KeyDef d;
  d.key = Key::Char;
  d.latin = latin;
  d.persian = fa;
  d.persian_shift = fa_shift;
  d.latin_alt = la;
  d.persian_alt = fa_alt ? fa_alt : char32_t(la);
  return d;
}

constexpr KeyDef K(Key k) {
  KeyDef d;
  d.key = k;
  return d;
}

// Persian digits ۰–۹ and punctuation.
constexpr char32_t fa_digit(int d) { return char32_t(0x06F0 + d); }
constexpr char32_t kFaComma = 0x060C, kFaSemicolon = 0x061B, kFaQuestion = 0x061F;

}  // namespace

// Letters on their ISIRI 9147 positions: ض on Q, ش on A, ظ on Z ... ;
// ژ has its own key (Latin '.') so every one of the 32 letters is one press.
// Alt on the top row gives digits (Persian digits in the Persian layer).
const KeyDef kMatrix[kRows][kCols] = {
    // R0
    {C('q', 0x0636, '1', fa_digit(1)), C('w', 0x0635, '2', fa_digit(2)), C('e', 0x062B, '3', fa_digit(3)),
     C('r', 0x0642, '4', fa_digit(4)), C('t', 0x0641, '5', fa_digit(5)), C('y', 0x063A, '6', fa_digit(6)),
     C('u', 0x0639, '7', fa_digit(7))},
    // R1
    {C('i', 0x0647, '8', fa_digit(8), 0x0629), C('o', 0x062E, '9', fa_digit(9)), C('p', 0x062D, '0', fa_digit(0)),
     C('[', 0x062C, '('), C(']', 0x0686, ')'), C('a', 0x0634, '!'), C('s', 0x0633, '?', kFaQuestion)},
    // R2
    {C('d', 0x06CC, '-', 0, 0x0626), C('f', 0x0628, '+'), C('g', 0x0644, '='), C('h', 0x0627, '/', 0, 0x0622),
     C('j', 0x062A, ':'), C('k', 0x0646, '"'), C('l', 0x0645, '\'')},
    // R3
    {C(';', 0x06A9, '*'), C('\'', 0x06AF, '#'), C('z', 0x0638, '@'), C('x', 0x0637, '%'), C('c', 0x0632, '&'),
     C('v', 0x0631, '_'), C('b', 0x0630, '<')},
    // R4
    {C('n', 0x062F, '>'), C('m', 0x067E, ';', kFaSemicolon), C(',', 0x0648, ',', kFaComma, 0x0624),
     C('.', 0x0698, '.', '.', 0x0621), K(Key::Space), K(Key::Backspace), K(Key::Enter)},
    // R5
    {K(Key::Shift), K(Key::Alt), K(Key::Up), K(Key::Down), K(Key::Left), K(Key::Right), K(Key::Ok)},
    // R6
    {K(Key::Call), K(Key::End), K(Key::Menu), K(Key::Back), K(Key::VolUp), K(Key::VolDown), K(Key::Setup)},
};

Pos find(Key k) {
  for (int r = 0; r < kRows; r++)
    for (int c = 0; c < kCols; c++)
      if (kMatrix[r][c].key == k) return Pos{int8_t(r), int8_t(c)};
  return Pos{};
}

Pos find_latin(char ch) {
  for (int r = 0; r < kRows; r++)
    for (int c = 0; c < kCols; c++)
      if (kMatrix[r][c].key == Key::Char && kMatrix[r][c].latin == ch) return Pos{int8_t(r), int8_t(c)};
  return Pos{};
}

size_t Matrix::update(const uint8_t scan[kCols], uint32_t now_ms, Event* out, size_t cap) {
  size_t n = 0;
  for (int c = 0; c < kCols; c++) {
    uint8_t raw = uint8_t(scan[c] & ((1u << kRows) - 1));
    if (raw != last_raw_[c]) {
      last_raw_[c] = raw;
      since_[c] = now_ms;
      continue;
    }
    if (raw == state_[c] || now_ms - since_[c] < kDebounceMs) continue;
    uint8_t changed = uint8_t(raw ^ state_[c]);
    for (int r = 0; r < kRows; r++) {
      if (!(changed >> r & 1)) continue;
      bool pressed = raw >> r & 1;
      if (n < cap) {
        out[n++] = Event{Pos{int8_t(r), int8_t(c)}, pressed};
        state_[c] = uint8_t(pressed ? state_[c] | 1u << r : state_[c] & ~(1u << r));
      }
    }
  }
  return n;
}

Output Layout::on_event(const Event& e) {
  Output o;
  if (!e.pos.valid()) return o;
  const KeyDef& d = kMatrix[e.pos.row][e.pos.col];
  if (d.key == Key::Shift || d.key == Key::Alt) {
    bool& mine = d.key == Key::Shift ? shift_ : alt_;
    bool other = d.key == Key::Shift ? alt_ : shift_;
    mine = e.pressed;
    if (e.pressed && other) {
      persian_ = !persian_;
      combo_ = true;
      o.action = Action::LanguageChanged;
    }
    if (!shift_ && !alt_) combo_ = false;
    return o;
  }
  if (!e.pressed) return o;
  // Typing while Alt + Shift are still held after a language switch is a slip.
  if (combo_ && (d.key == Key::Char || d.key == Key::Space)) return o;
  bool shift = shift_, alt = alt_;
  switch (d.key) {
    case Key::Char: {
      char32_t cp = 0;
      if (alt) {
        cp = persian_ ? d.persian_alt : char32_t(d.latin_alt);
      } else if (persian_) {
        cp = shift && d.persian_shift ? d.persian_shift : d.persian;
      } else {
        cp = char32_t(d.latin);
        if (shift && cp >= 'a' && cp <= 'z') cp = cp - 'a' + 'A';
      }
      if (cp) {
        o.action = Action::Text;
        o.cp = cp;
      }
      return o;
    }
    case Key::Space:
      o.action = Action::Text;
      o.cp = persian_ && shift ? kZwnj : U' ';
      return o;
    case Key::Backspace: o.action = Action::Backspace; return o;
    case Key::Enter: o.action = Action::Enter; return o;
    case Key::Up: o.action = Action::Up; return o;
    case Key::Down: o.action = Action::Down; return o;
    case Key::Left: o.action = Action::Left; return o;
    case Key::Right: o.action = Action::Right; return o;
    case Key::Ok: o.action = Action::Ok; return o;
    case Key::Call: o.action = Action::Call; return o;
    case Key::End: o.action = Action::End; return o;
    case Key::Menu: o.action = Action::Menu; return o;
    case Key::Back: o.action = Action::Back; return o;
    case Key::VolUp: o.action = Action::VolUp; return o;
    case Key::VolDown: o.action = Action::VolDown; return o;
    default: return o;  // Setup is a switch, read by the board
  }
}

}  // namespace keys
}  // namespace mesh

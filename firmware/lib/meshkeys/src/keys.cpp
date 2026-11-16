#include "keys.h"

namespace mesh {
namespace keys {

namespace {

constexpr KeyDef C(char latin, char32_t fa, char la = 0, char32_t fa_alt = 0, char32_t fa_shift = 0,
                   Action nav = Action::None) {
  KeyDef d;
  d.key = Key::Char;
  d.latin = latin;
  d.persian = fa;
  d.persian_shift = fa_shift;
  d.latin_alt = la;
  d.persian_alt = fa_alt ? fa_alt : char32_t(la);
  d.nav = nav;
  return d;
}

constexpr KeyDef K(Key k, Action nav = Action::None) {
  KeyDef d;
  d.key = k;
  d.nav = nav;
  return d;
}

// Persian digits ۰–۹ and punctuation.
constexpr char32_t fa_digit(int d) { return char32_t(0x06F0 + d); }
constexpr char32_t kFaComma = 0x060C, kFaSemicolon = 0x061B, kFaQuestion = 0x061F;

// Persian letters.
constexpr char32_t ALEF = 0x0627, BE = 0x0628, PE = 0x067E, TE = 0x062A, SE = 0x062B, JIM = 0x062C,
                   CHE = 0x0686, HE_JIMI = 0x062D, KHE = 0x062E, DAL = 0x062F, ZAL = 0x0630, RE = 0x0631,
                   ZE = 0x0632, ZHE = 0x0698, SIN = 0x0633, SHIN = 0x0634, SAD = 0x0635, ZAD = 0x0636,
                   TA = 0x0637, ZA = 0x0638, EYN = 0x0639, GHEYN = 0x063A, FE = 0x0641, GHAF = 0x0642,
                   KAF = 0x06A9, GAF = 0x06AF, LAM = 0x0644, MIM = 0x0645, NUN = 0x0646, VAV = 0x0648,
                   HE = 0x0647, YE = 0x06CC;
constexpr char32_t ALEF_MADDA = 0x0622, YE_HAMZA = 0x0626, VAV_HAMZA = 0x0624, TE_MARBUTA = 0x0629,
                   HAMZA = 0x0621;

// ---- version A: our own 7 × 7 matrix ----
// Letters on their ISIRI 9147 positions: ض on Q, ش on A, ظ on Z ... ;
// ژ has its own key (Latin '.') so every one of the 32 letters is one press.
// Alt on the top row gives digits (Persian digits in the Persian layer).
const KeyDef k7x7[7 * 7] = {
    // R0
    C('q', ZAD, '1', fa_digit(1)), C('w', SAD, '2', fa_digit(2)), C('e', SE, '3', fa_digit(3)),
    C('r', GHAF, '4', fa_digit(4)), C('t', FE, '5', fa_digit(5)), C('y', GHEYN, '6', fa_digit(6)),
    C('u', EYN, '7', fa_digit(7)),
    // R1
    C('i', HE, '8', fa_digit(8), TE_MARBUTA), C('o', KHE, '9', fa_digit(9)), C('p', HE_JIMI, '0', fa_digit(0)),
    C('[', JIM, '('), C(']', CHE, ')'), C('a', SHIN, '!'), C('s', SIN, '?', kFaQuestion),
    // R2
    C('d', YE, '-', 0, YE_HAMZA), C('f', BE, '+'), C('g', LAM, '='), C('h', ALEF, '/', 0, ALEF_MADDA),
    C('j', TE, ':'), C('k', NUN, '"'), C('l', MIM, '\''),
    // R3
    C(';', KAF, '*'), C('\'', GAF, '#'), C('z', ZA, '@'), C('x', TA, '%'), C('c', ZE, '&'), C('v', RE, '_'),
    C('b', ZAL, '<'),
    // R4
    C('n', DAL, '>'), C('m', PE, ';', kFaSemicolon), C(',', VAV, ',', kFaComma, VAV_HAMZA),
    C('.', ZHE, '.', '.', HAMZA), K(Key::Space), K(Key::Backspace), K(Key::Enter),
    // R5
    K(Key::Shift), K(Key::Alt), K(Key::Up), K(Key::Down), K(Key::Left), K(Key::Right), K(Key::Ok),
    // R6
    K(Key::Call), K(Key::End), K(Key::Menu), K(Key::Back), K(Key::VolUp), K(Key::VolDown), K(Key::Sym),
};

// ---- versions B / C: BlackBerry Bold 9900 keyboard ----
// Matrix positions from the 9900 keyboard flex as mapped by ZitaoTech's
// BB9900-USB-keyboard (QMK info.json): rows R0–R6, columns C0–C5. Latin
// letters are named by their QWERTY position; `qwertz` swaps Y and Z.
// Alt symbols are the ones printed on the keycaps. Persian: ISIRI letters on
// the 26 letter keys, و on $, ک on 0; Alt + ح = ج, Alt + ع = چ, Alt + م = گ,
// Alt + ذ = ژ (their printed symbols @ _ " ! are in the symbol picker).
// (0,0): free (B) or trackpad click (C); (6,0) / (6,1): Vol+ / Vol− side
// buttons on the keyboard module — each with a diode.
constexpr KeyDef kBB9900[7 * 6] = {
    // R0
    K(Key::None), C('w', SAD, '1', fa_digit(1)), C('g', LAM, '/'), C('s', SIN, '4', fa_digit(4), 0, Action::Left),
    C('l', MIM, '"', GAF), C('h', ALEF, ':', 0, ALEF_MADDA),
    // R1
    C('a', SHIN, '*'), C('q', ZAD, '#'), C('r', GHAF, '3', fa_digit(3)), C('e', SE, '2', fa_digit(2), 0, Action::Up),
    C('o', KHE, '+'), C('u', EYN, '_', CHE),
    // R2
    K(Key::Call), C('0', KAF, '0', fa_digit(0)), C('f', BE, '6', fa_digit(6), 0, Action::Right), K(Key::Shift),
    C('k', NUN, '\''), C('j', TE, ';', kFaSemicolon),
    // R3
    K(Key::End), K(Key::Space), C('c', ZE, '9', fa_digit(9), HAMZA), C('z', ZA, '7', fa_digit(7)),
    C('m', PE, '.'), C('n', DAL, ',', kFaComma),
    // R4
    K(Key::Menu), K(Key::Sym), C('t', FE, '('), C('d', YE, '5', fa_digit(5), YE_HAMZA, Action::Down),
    C('i', HE, '-', 0, TE_MARBUTA), C('y', GHEYN, ')'),
    // R5
    K(Key::Back), K(Key::Alt), C('v', RE, '?', kFaQuestion), C('x', TA, '8', fa_digit(8)),
    C('$', VAV, '$', 0, VAV_HAMZA), C('b', ZAL, '!', ZHE),
    // R6
    K(Key::VolUp), K(Key::VolDown), K(Key::Shift), C('p', HE_JIMI, '@', JIM), K(Key::Backspace),
    K(Key::Enter, Action::Ok),
};

struct WithTrackpad {
  KeyDef defs[7 * 6];
  constexpr WithTrackpad() : defs() {
    for (int i = 0; i < 7 * 6; i++) defs[i] = kBB9900[i];
    defs[0] = K(Key::TrackClick);
  }
};
constexpr WithTrackpad kBB9900Tp;

const Keyboard kKeyboards[] = {
    {Variant::Matrix7x7, 7, 7, k7x7, false},
    {Variant::BB9900, 7, 6, kBB9900, true},
    {Variant::BB9900Trackpad, 7, 6, kBB9900Tp.defs, true},
};

}  // namespace

const Keyboard& keyboard(Variant v) { return kKeyboards[int(v)]; }

Pos Keyboard::find(Key k) const {
  for (int r = 0; r < rows; r++)
    for (int c = 0; c < cols; c++)
      if (defs[r * cols + c].key == k) return Pos{int8_t(r), int8_t(c)};
  return Pos{};
}

Pos Keyboard::find_latin(char ch) const {
  for (int r = 0; r < rows; r++)
    for (int c = 0; c < cols; c++)
      if (defs[r * cols + c].key == Key::Char && defs[r * cols + c].latin == ch) return Pos{int8_t(r), int8_t(c)};
  return Pos{};
}

size_t Matrix::update(const uint8_t* scan, uint32_t now_ms, Event* out, size_t cap) {
  size_t n = 0;
  for (int c = 0; c < kb_.cols; c++) {
    uint8_t raw = uint8_t(scan[c] & ((1u << kb_.rows) - 1));
    if (raw != last_raw_[c]) {
      last_raw_[c] = raw;
      since_[c] = now_ms;
      continue;
    }
    if (raw == state_[c] || now_ms - since_[c] < kDebounceMs) continue;
    uint8_t changed = uint8_t(raw ^ state_[c]);
    for (int r = 0; r < kb_.rows; r++) {
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

Output Layout::modifier(Key k, bool pressed) {
  Output o;
  bool& held = k == Key::Shift ? shift_ : alt_;
  bool& once = k == Key::Shift ? shift_once_ : alt_once_;
  bool other = k == Key::Shift ? (alt_ || alt_once_) : (shift_ || shift_once_);
  if (pressed) {
    held = true;
    if (!shift_ || !alt_) used_ = false;
    if (other) {
      // Alt + Shift, held together or one tapped first: switch language.
      persian_ = !persian_;
      combo_ = true;
      shift_once_ = alt_once_ = false;
      o.action = Action::LanguageChanged;
    }
    return o;
  }
  held = false;
  if (!combo_ && !used_) once = !once;  // a tap on its own: one-shot (tap again to cancel)
  if (!shift_ && !alt_) combo_ = false;
  return o;
}

Output Layout::press(const KeyDef& d) {
  Output o;
  // Typing while Alt + Shift are still held after a language switch is a slip.
  if (combo_ && (d.key == Key::Char || d.key == Key::Space)) return o;
  if (shift_ || alt_) used_ = true;
  bool shift = shift_ || shift_once_, alt = alt_ || alt_once_;
  if (sym_ && d.nav != Action::None) {
    sym_used_ = true;
    o.action = d.nav;
    return o;
  }
  if (d.key == Key::Char || d.key == Key::Space) shift_once_ = alt_once_ = false;
  switch (d.key) {
    case Key::Char: {
      char32_t cp = 0;
      if (alt) {
        cp = persian_ ? d.persian_alt : char32_t(d.latin_alt);
      } else if (persian_) {
        cp = shift && d.persian_shift ? d.persian_shift : d.persian;
      } else {
        cp = char32_t(d.latin);
        if (kb_.qwertz && (cp == 'y' || cp == 'z')) cp = cp == 'y' ? 'z' : 'y';
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
    case Key::Ok:
    case Key::TrackClick: o.action = Action::Ok; return o;
    case Key::Call: o.action = Action::Call; return o;
    case Key::End: o.action = Action::End; return o;
    case Key::Menu: o.action = Action::Menu; return o;
    case Key::Back: o.action = Action::Back; return o;
    case Key::VolUp: o.action = Action::VolUp; return o;
    case Key::VolDown: o.action = Action::VolDown; return o;
    default: return o;
  }
}

Output Layout::on_event(const Event& e) {
  Output o;
  if (!e.pos.valid()) return o;
  const KeyDef& d = kb_.at(e.pos);
  if (d.key == Key::Shift || d.key == Key::Alt) return modifier(d.key, e.pressed);
  if (d.key == Key::Sym) {
    if (e.pressed) {
      sym_ = true;
      sym_used_ = false;
    } else {
      sym_ = false;
      if (!sym_used_) o.action = Action::Symbols;
    }
    return o;
  }
  if (!e.pressed) return o;
  return press(d);
}

}  // namespace keys
}  // namespace mesh

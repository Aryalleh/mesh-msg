#include "osk.h"

namespace mesh {
namespace osk {

namespace {

constexpr Button T(const char* s, uint8_t w = 1) { return Button{s, Kind::Text, s, w}; }
constexpr Button K(const char* label, Kind k, uint8_t w = 1) { return Button{label, k, nullptr, w}; }
constexpr Button TL(const char* label, const char* text, uint8_t w) { return Button{label, Kind::Text, text, w}; }

// Labels use plain characters so the Persian font renders them; the UI maps
// these few control labels to LVGL symbols.
constexpr const char* kBksp = "<-";
constexpr const char* kEnter = "Enter";
constexpr const char* kShift = "Shift";
constexpr const char* kHide = "v";

// Persian: ISIRI 9147 order — ض … ج on the top row, ش … گ, then ظ … ژ.
constexpr Button kPersian[] = {
    T("ض"), T("ص"), T("ث"), T("ق"), T("ف"), T("غ"), T("ع"), T("ه"), T("خ"), T("ح"), T("ج"),
    T("ش"), T("س"), T("ی"), T("ب"), T("ل"), T("ا"), T("ت"), T("ن"), T("م"), T("ک"), T("گ"),
    T("ظ"), T("ط"), T("ز"), T("ر"), T("ذ"), T("د"), T("پ"), T("و"), T("چ"), T("ژ"), K(kBksp, Kind::Backspace),
    K("۱۲۳", Kind::Symbols, 2), K("EN", Kind::Language, 2), TL("نیم", "‌", 2), TL(" ", " ", 4), T("،"),
    K(kEnter, Kind::Enter, 2), K(kHide, Kind::Hide),
};
constexpr uint8_t kPersianRows[] = {11, 11, 11, 7};

constexpr Button kLatin[] = {
    T("q"), T("w"), T("e"), T("r"), T("t"), T("y"), T("u"), T("i"), T("o"), T("p"),
    T("a"), T("s"), T("d"), T("f"), T("g"), T("h"), T("j"), T("k"), T("l"),
    K(kShift, Kind::Shift, 2), T("z"), T("x"), T("c"), T("v"), T("b"), T("n"), T("m"), K(kBksp, Kind::Backspace, 2),
    K("123", Kind::Symbols, 2), K("فا", Kind::Language, 2), T(","), TL(" ", " ", 4), T("."), K(kEnter, Kind::Enter, 2),
    K(kHide, Kind::Hide),
};
constexpr uint8_t kLatinRows[] = {10, 9, 9, 7};

constexpr Button kLatinUpper[] = {
    T("Q"), T("W"), T("E"), T("R"), T("T"), T("Y"), T("U"), T("I"), T("O"), T("P"),
    T("A"), T("S"), T("D"), T("F"), T("G"), T("H"), T("J"), T("K"), T("L"),
    K(kShift, Kind::Shift, 2), T("Z"), T("X"), T("C"), T("V"), T("B"), T("N"), T("M"), K(kBksp, Kind::Backspace, 2),
    K("123", Kind::Symbols, 2), K("فا", Kind::Language, 2), T(","), TL(" ", " ", 4), T("."), K(kEnter, Kind::Enter, 2),
    K(kHide, Kind::Hide),
};

// Symbols: digits, punctuation, and the Persian extras آ ئ ؤ ة ء « ».
constexpr Button kSymbolsPersian[] = {
    T("۱"), T("۲"), T("۳"), T("۴"), T("۵"), T("۶"), T("۷"), T("۸"), T("۹"), T("۰"),
    T("آ"), T("ئ"), T("ؤ"), T("ة"), T("ء"), T("«"), T("»"), T("؟"), T("!"), T("؛"),
    T("@"), T("#"), T("%"), T("+"), T("-"), T("="), T("/"), T(":"), T("("), T(")"), K(kBksp, Kind::Backspace, 2),
    K("الف", Kind::Letters, 2), T("."), TL(" ", " ", 4), T("،"), K(kEnter, Kind::Enter, 2), K(kHide, Kind::Hide),
};
constexpr uint8_t kSymbolsRows[] = {10, 10, 11, 6};

constexpr Button kSymbolsLatin[] = {
    T("1"), T("2"), T("3"), T("4"), T("5"), T("6"), T("7"), T("8"), T("9"), T("0"),
    T("@"), T("#"), T("$"), T("%"), T("&"), T("*"), T("-"), T("+"), T("("), T(")"),
    T("!"), T("?"), T("\""), T("'"), T(":"), T(";"), T("/"), T("_"), T("="), T("~"), K(kBksp, Kind::Backspace, 2),
    K("ABC", Kind::Letters, 2), T(","), TL(" ", " ", 4), T("."), K(kEnter, Kind::Enter, 2), K(kHide, Kind::Hide),
};

template <size_t N, size_t R>
constexpr Layout L(const Button (&b)[N], const uint8_t (&r)[R]) {
  return Layout{b, N, r, R};
}

const Layout kLayouts[] = {
    L(kPersian, kPersianRows),        L(kLatin, kLatinRows),           L(kLatinUpper, kLatinRows),
    L(kSymbolsPersian, kSymbolsRows), L(kSymbolsLatin, kSymbolsRows),
};

}  // namespace

const Layout& layout(Page p) { return kLayouts[int(p)]; }

Press press(Page p, size_t index) {
  Press r;
  r.next = p;
  const Layout& l = layout(p);
  if (index >= l.count) {
    r.kind = Kind::Text;
    r.text = "";
    return r;
  }
  const Button& b = l.buttons[index];
  r.kind = b.kind;
  r.text = b.text;
  bool persian = p == Page::Persian || p == Page::SymbolsPersian;
  switch (b.kind) {
    case Kind::Text:
      if (p == Page::LatinUpper) r.next = Page::Latin;  // one capital, then lower case
      break;
    case Kind::Shift:
      r.next = p == Page::Latin ? Page::LatinUpper : Page::Latin;
      break;
    case Kind::Language:
      r.next = persian ? Page::Latin : Page::Persian;
      break;
    case Kind::Symbols:
      r.next = persian ? Page::SymbolsPersian : Page::SymbolsLatin;
      break;
    case Kind::Letters:
      r.next = persian ? Page::Persian : Page::Latin;
      break;
    default:
      break;
  }
  return r;
}

}  // namespace osk
}  // namespace mesh

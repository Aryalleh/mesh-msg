// On-screen (touch) keyboard layouts for the handheld (docs/handheld-proto.md §8):
// Persian (ISIRI 9147 order, 32 letters on three rows), Latin QWERTY lower /
// upper, and symbols (Persian or Latin digits). The UI shows a page as an
// LVGL button matrix; press() says what a button does. Pure C++17, host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace osk {

enum class Page : uint8_t { Persian, Latin, LatinUpper, SymbolsPersian, SymbolsLatin };

enum class Kind : uint8_t {
  Text,       // insert `text` (UTF-8)
  Backspace,
  Enter,
  Shift,      // Latin ↔ Latin upper
  Language,   // Persian ↔ Latin
  Symbols,    // letters → symbols of the current language
  Letters,    // symbols → letters of the current language
  Hide,       // close the keyboard
};

struct Button {
  const char* label;  // UTF-8, shown on the key
  Kind kind;
  const char* text;   // for Kind::Text
  uint8_t width;      // relative width in its row (1–4)
};

constexpr size_t kMaxButtons = 48;

struct Layout {
  const Button* buttons;
  size_t count;
  // Buttons per row, rows top to bottom.
  const uint8_t* rows;
  size_t row_count;
};

const Layout& layout(Page p);

struct Press {
  Kind kind = Kind::Text;
  const char* text = nullptr;  // Kind::Text
  Page next;                   // the page to show after this press
};

// What pressing button `index` on page p does. Shift on the Latin page is one
// shot: after one capital the keyboard returns to lower case.
Press press(Page p, size_t index);

}  // namespace osk
}  // namespace mesh

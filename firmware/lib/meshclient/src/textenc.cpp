#include "textenc.h"

#include <vector>

namespace mesh {
namespace textenc {
namespace {

const std::vector<char32_t>& table() {
  static const std::vector<char32_t> t = [] {
    std::vector<char32_t> v;
    auto span = [&](char32_t a, char32_t b) {
      for (char32_t r = a; r <= b; r++) v.push_back(r);
    };
    span(0x0621, 0x063A);
    span(0x0641, 0x064A);
    span(0x064B, 0x0652);
    for (char32_t r : {0x0654, 0x0670}) v.push_back(r);
    for (char32_t r : {0x067E, 0x0686, 0x0698, 0x06A9, 0x06AF, 0x06CC, 0x06C0}) v.push_back(r);
    span(0x0660, 0x0669);
    span(0x06F0, 0x06F9);
    for (char32_t r : {0x060C, 0x061B, 0x061F, 0x0640, 0x066A, 0x066B, 0x066C}) v.push_back(r);
    for (char32_t r : {0x00AB, 0x00BB, 0x200C, 0x200D, 0x200E, 0x200F}) v.push_back(r);
    for (char32_t r : {0x2026, 0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00A0}) v.push_back(r);
    return v;
  }();
  return t;
}

int index_of(char32_t r) {
  const auto& t = table();
  for (size_t i = 0; i < t.size(); i++)
    if (t[i] == r) return int(i);
  return -1;
}

// Decodes one UTF-8 rune at s[i]; returns its length (0 = invalid).
size_t rune(const uint8_t* s, size_t n, size_t i, char32_t& r) {
  uint8_t c = s[i];
  size_t len = c < 0x80 ? 1 : (c >> 5) == 6 ? 2 : (c >> 4) == 14 ? 3 : (c >> 3) == 30 ? 4 : 0;
  if (!len || i + len > n) return 0;
  r = len == 1 ? c : len == 2 ? c & 0x1F : len == 3 ? c & 0x0F : c & 0x07;
  for (size_t k = 1; k < len; k++) {
    if ((s[i + k] & 0xC0) != 0x80) return 0;
    r = r << 6 | (s[i + k] & 0x3F);
  }
  static const char32_t kMin[] = {0, 0, 0x80, 0x800, 0x10000};
  if (r < kMin[len] || r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF)) return 0;
  return len;
}

void put(std::string& out, char32_t r) {
  if (r < 0x80) {
    out += char(r);
  } else if (r < 0x800) {
    out += char(0xC0 | r >> 6);
    out += char(0x80 | (r & 0x3F));
  } else if (r < 0x10000) {
    out += char(0xE0 | r >> 12);
    out += char(0x80 | (r >> 6 & 0x3F));
    out += char(0x80 | (r & 0x3F));
  } else {
    out += char(0xF0 | r >> 18);
    out += char(0x80 | (r >> 12 & 0x3F));
    out += char(0x80 | (r >> 6 & 0x3F));
    out += char(0x80 | (r & 0x3F));
  }
}

}  // namespace

uint8_t encode(const std::string& s, std::string& out) {
  std::string c;
  const uint8_t* b = reinterpret_cast<const uint8_t*>(s.data());
  for (size_t i = 0; i < s.size();) {
    char32_t r = 0xFFFD;
    size_t len = rune(b, s.size(), i, r);
    if (!len) {  // as Go's range over a string: one byte becomes U+FFFD
      len = 1;
      r = 0xFFFD;
    }
    int k = index_of(r);
    if (k >= 0) {
      c += char(0x80 + k);
    } else if (r > 0 && r < 0x80) {
      c += char(r);
    } else {
      c += char(0);
      put(c, r);
    }
    i += len;
  }
  if (c.size() < s.size()) {
    out = c;
    return 1;
  }
  out = s;
  return 0;
}

bool decode(uint8_t ct, const uint8_t* b, size_t n, std::string& out) {
  out.clear();
  if (ct == 0) {
    for (size_t i = 0; i < n;) {
      char32_t r;
      size_t len = rune(b, n, i, r);
      if (!len) return false;
      i += len;
    }
    out.assign(reinterpret_cast<const char*>(b), n);
    return true;
  }
  if (ct != 1) return false;
  const auto& t = table();
  for (size_t i = 0; i < n;) {
    uint8_t c = b[i];
    if (c == 0) {
      char32_t r;
      size_t len = i + 1 < n ? rune(b, n, i + 1, r) : 0;
      if (!len) return false;
      put(out, r);
      i += 1 + len;
    } else if (c < 0x80) {
      out += char(c);
      i++;
    } else {
      size_t k = c - 0x80;
      if (k >= t.size()) return false;
      put(out, t[k]);
      i++;
    }
  }
  return true;
}

}  // namespace textenc
}  // namespace mesh

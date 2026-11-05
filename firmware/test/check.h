// Tiny test harness for the host build.
#pragma once

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace check {

inline int& failures() {
  static int n = 0;
  return n;
}

inline void report(bool ok, const char* expr, const char* file, int line, const std::string& ctx) {
  if (ok) return;
  failures()++;
  if (ctx.empty())
    std::fprintf(stderr, "FAIL %s:%d: %s\n", file, line, expr);
  else
    std::fprintf(stderr, "FAIL %s:%d: %s  [%s]\n", file, line, expr, ctx.c_str());
}

using Bytes = std::vector<uint8_t>;

inline Bytes unhex(const std::string& s) {
  Bytes out;
  for (size_t i = 0; i + 1 < s.size(); i += 2) out.push_back(uint8_t(std::stoul(s.substr(i, 2), nullptr, 16)));
  return out;
}

inline std::string hex(const uint8_t* b, size_t n) {
  static const char* d = "0123456789abcdef";
  std::string s;
  for (size_t i = 0; i < n; i++) {
    s += d[b[i] >> 4];
    s += d[b[i] & 15];
  }
  return s;
}
inline std::string hex(const Bytes& b) { return hex(b.data(), b.size()); }

}  // namespace check

#define CHECK(expr) ::check::report((expr), #expr, __FILE__, __LINE__, "")
#define CHECKC(expr, ctx) ::check::report((expr), #expr, __FILE__, __LINE__, (ctx))
#define CHECK_HEX(got, want, ctx)                                                                     \
  ::check::report((got) == (want), #got " == " #want, __FILE__, __LINE__,                             \
                  std::string(ctx) + " got=" + (got) + " want=" + (want))

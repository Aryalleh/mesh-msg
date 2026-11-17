#include "mirror.h"

#include <cstring>

namespace mesh {
namespace mirror {

namespace {
void put16(uint8_t* p, int v) {
  p[0] = uint8_t(v >> 8);
  p[1] = uint8_t(v);
}
int get16(const uint8_t* p) { return int(p[0]) << 8 | p[1]; }
}  // namespace

Mirror::Mirror(uint16_t* fb) : fb_(fb) { std::memset(fb_, 0, sizeof(uint16_t) * kWidth * kHeight); }

void Mirror::set(int t) {
  uint8_t bit = uint8_t(1u << (t & 7));
  if (dirty_[t >> 3] & bit) return;
  dirty_[t >> 3] |= bit;
  dirty_count_++;
}

void Mirror::blit(int x1, int y1, int x2, int y2, const uint16_t* px) {
  int stride = x2 - x1 + 1;
  if (stride <= 0 || y2 < y1) return;
  int cx1 = x1 < 0 ? 0 : x1, cy1 = y1 < 0 ? 0 : y1;
  int cx2 = x2 >= kWidth ? kWidth - 1 : x2, cy2 = y2 >= kHeight ? kHeight - 1 : y2;
  if (cx1 > cx2 || cy1 > cy2) return;
  for (int y = cy1; y <= cy2; y++) {
    const uint16_t* src = px + size_t(y - y1) * stride + (cx1 - x1);
    std::memcpy(fb_ + size_t(y) * kWidth + cx1, src, sizeof(uint16_t) * (cx2 - cx1 + 1));
  }
  for (int ty = cy1 / kTileH; ty <= cy2 / kTileH; ty++)
    for (int tx = cx1 / kTileW; tx <= cx2 / kTileW; tx++) set(ty * kCols + tx);
}

void Mirror::mark_all() {
  for (int t = 0; t < kTiles; t++) set(t);
  cursor_ = 0;
}

size_t Mirror::info(uint8_t* out, size_t cap) {
  if (cap < kInfoSize) return 0;
  out[0] = kInfo;
  put16(out + 1, kWidth);
  put16(out + 3, kHeight);
  out[5] = kTileW;
  out[6] = kTileH;
  return kInfoSize;
}

size_t Mirror::next(uint8_t* out, size_t cap) {
  if (!dirty_count_ || cap < kMaxTile) return 0;
  int t = cursor_;
  while (!(dirty_[t >> 3] & (1u << (t & 7)))) t = (t + 1) % kTiles;
  dirty_[t >> 3] &= uint8_t(~(1u << (t & 7)));
  dirty_count_--;
  cursor_ = (t + 1) % kTiles;

  int x = (t % kCols) * kTileW, y = (t / kCols) * kTileH;
  out[0] = kTile;
  put16(out + 1, x);
  put16(out + 3, y);
  out[5] = kTileW;
  out[6] = kTileH;
  // Run-length first; raw if that is not smaller.
  uint8_t* p = out + kTileHeader;
  size_t n = 0, raw = kTileW * kTileH * 2;
  bool rle = true;
  uint16_t run_c = 0;
  int run_n = 0;
  auto flush = [&]() {
    if (!run_n) return;
    if (n + 3 > raw) {
      rle = false;
      return;
    }
    p[n++] = uint8_t(run_n);
    p[n++] = uint8_t(run_c);
    p[n++] = uint8_t(run_c >> 8);
  };
  for (int r = 0; r < kTileH && rle; r++) {
    const uint16_t* row = fb_ + size_t(y + r) * kWidth + x;
    for (int c = 0; c < kTileW && rle; c++) {
      if (run_n && (row[c] != run_c || run_n == 255)) {
        flush();
        run_n = 0;
      }
      run_c = row[c];
      run_n++;
    }
  }
  if (rle) flush();
  if (rle && n < raw) {
    out[7] = kFormatRle;
    return kTileHeader + n;
  }
  out[7] = kFormatRaw;
  for (int r = 0; r < kTileH; r++) {
    const uint16_t* row = fb_ + size_t(y + r) * kWidth + x;
    for (int c = 0; c < kTileW; c++) {
      *p++ = uint8_t(row[c]);
      *p++ = uint8_t(row[c] >> 8);
    }
  }
  return kTileHeader + raw;
}

bool Mirror::apply(const uint8_t* d, size_t n, uint16_t* fb) {
  if (n < kTileHeader || d[0] != kTile) return false;
  int x = get16(d + 1), y = get16(d + 3), w = d[5], h = d[6];
  if (w == 0 || h == 0 || x + w > kWidth || y + h > kHeight) return false;
  const uint8_t* p = d + kTileHeader;
  size_t m = n - kTileHeader, want = size_t(w) * h;
  if (d[7] == kFormatRaw) {
    if (m != want * 2) return false;
    for (int r = 0; r < h; r++)
      for (int c = 0; c < w; c++, p += 2) fb[size_t(y + r) * kWidth + x + c] = uint16_t(p[0] | p[1] << 8);
    return true;
  }
  if (d[7] != kFormatRle || m % 3) return false;
  size_t i = 0;
  for (size_t k = 0; k < m; k += 3) {
    size_t cnt = p[k];
    uint16_t col = uint16_t(p[k + 1] | p[k + 2] << 8);
    if (cnt == 0 || i + cnt > want) return false;
    for (; cnt; cnt--, i++) fb[size_t(y + int(i) / w) * kWidth + x + int(i) % w] = col;
  }
  return i == want;
}

}  // namespace mirror
}  // namespace mesh

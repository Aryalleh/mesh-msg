// Screen mirror (docs/screen.md): the handheld's screen on a phone over the
// host link. The board keeps a copy of the 240×320 RGB565 frame; LVGL's
// flushes are blitted into it and mark 16×8 tiles dirty; the main loop sends
// dirty tiles as SCREEN events while the host's output buffer has room, so
// the screen never waits for USB and only what changed is sent.
//
// Event data (host::kEvScreen):
//   info: 0 | width u16 BE | height u16 BE | tile w u8 | tile h u8
//   tile: 1 | x u16 BE | y u16 BE | w u8 | h u8 | format u8 | pixels
//     format 0: raw RGB565 little-endian, row by row
//     format 1: runs of count u8 (1–255) | RGB565 u16 little-endian
// INPUT command arguments (host::kOpInput):
//   touch: 0 | x u16 BE | y u16 BE | pressed u8
//   key:   1 | action u8 (keys::Action) | code point u32 BE (Text)
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace mirror {

constexpr int kWidth = 240;
constexpr int kHeight = 320;
constexpr int kTileW = 16;
constexpr int kTileH = 8;
constexpr int kCols = kWidth / kTileW;   // 15
constexpr int kRows = kHeight / kTileH;  // 40
constexpr int kTiles = kCols * kRows;

constexpr uint8_t kInfo = 0;
constexpr uint8_t kTile = 1;
constexpr uint8_t kFormatRaw = 0;
constexpr uint8_t kFormatRle = 1;
constexpr size_t kInfoSize = 7;
constexpr size_t kTileHeader = 8;
constexpr size_t kMaxTile = kTileHeader + kTileW * kTileH * 2;  // raw is never exceeded

constexpr uint8_t kInputTouch = 0;
constexpr uint8_t kInputKey = 1;
constexpr size_t kInputSize = 6;

class Mirror {
 public:
  // fb: kWidth × kHeight pixels the mirror owns the contents of.
  explicit Mirror(uint16_t* fb);

  // A flushed area, inclusive corners, px row-major (x2 − x1 + 1 per row).
  // Parts outside the screen are clipped.
  void blit(int x1, int y1, int x2, int y2, const uint16_t* px);
  // Everything is sent again (a host just subscribed).
  void mark_all();
  bool pending() const { return dirty_count_ > 0; }
  size_t dirty() const { return dirty_count_; }

  // Encodes the next dirty tile (event data, starting with kTile) into out;
  // returns its size, 0 if nothing is dirty or cap < kMaxTile.
  size_t next(uint8_t* out, size_t cap);
  static size_t info(uint8_t* out, size_t cap);

  // Applies one tile event to a frame buffer (the phone side, and tests).
  static bool apply(const uint8_t* d, size_t n, uint16_t* fb);

 private:
  void set(int t);
  uint16_t* fb_;
  uint8_t dirty_[(kTiles + 7) / 8] = {};
  size_t dirty_count_ = 0;
  int cursor_ = 0;
};

}  // namespace mirror
}  // namespace mesh

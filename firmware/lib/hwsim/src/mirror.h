// Screen mirror (docs/screen.md): the handheld's screen on a phone over the
// host link. The board keeps a copy of the 320×240 RGB565 frame (240×320
// when the board's Settings turn the screen to portrait: set_size); LVGL's
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
//   option:2 | id u8 | value u8 | 0 0 0  (kOptTouchKeyboard: 0 = hide it, the phone types)
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace mirror {

// Landscape by default, as the BlackBerry: 320 × 240 (the panel is rotated in
// hardware); portrait 240 × 320 holds the same pixels and tiles.
constexpr int kWidth = 320;
constexpr int kHeight = 240;
constexpr int kTileW = 16;
constexpr int kTileH = 8;
constexpr int kTiles = (kWidth / kTileW) * (kHeight / kTileH);  // 600 either way

constexpr uint8_t kInfo = 0;
constexpr uint8_t kTile = 1;
constexpr uint8_t kFormatRaw = 0;
constexpr uint8_t kFormatRle = 1;
constexpr size_t kInfoSize = 7;
constexpr size_t kTileHeader = 8;
constexpr size_t kMaxTile = kTileHeader + kTileW * kTileH * 2;  // raw is never exceeded

constexpr uint8_t kInputTouch = 0;
constexpr uint8_t kInputKey = 1;
constexpr uint8_t kInputOption = 2;
constexpr uint8_t kOptTouchKeyboard = 1;
constexpr size_t kInputSize = 6;

class Mirror {
 public:
  // fb: kWidth × kHeight pixels the mirror owns the contents of.
  explicit Mirror(uint16_t* fb);
  // The screen turned: w × h (multiples of the tile size, w·h = kWidth·kHeight).
  // Clears the copy; the host needs a new info event and every tile again.
  bool set_size(int w, int h);
  int width() const { return w_; }
  int height() const { return h_; }

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
  size_t info(uint8_t* out, size_t cap) const;

  // Applies one tile event to a w × h frame buffer (the phone side, and tests).
  static bool apply(const uint8_t* d, size_t n, uint16_t* fb, int w = kWidth, int h = kHeight);

 private:
  void set(int t);
  int cols() const { return w_ / kTileW; }
  uint16_t* fb_;
  int w_ = kWidth, h_ = kHeight;
  uint8_t dirty_[(kTiles + 7) / 8] = {};
  size_t dirty_count_ = 0;
  int cursor_ = 0;
};

}  // namespace mirror
}  // namespace mesh

// Screen mirror: what the phone rebuilds from the tiles equals the board's frame.
#include <cstdlib>
#include <vector>

#include "check.h"
#include "mirror.h"

namespace mm = mesh::mirror;
using check::Bytes;

namespace {

std::vector<uint16_t> frame() { return std::vector<uint16_t>(size_t(mm::kWidth) * mm::kHeight); }

// Sends everything dirty into a phone-side frame; returns how many tiles.
int drain(mm::Mirror& m, std::vector<uint16_t>& phone, size_t* bytes = nullptr) {
  uint8_t buf[mm::kMaxTile];
  int tiles = 0;
  while (size_t n = m.next(buf, sizeof buf)) {
    CHECK(n <= mm::kMaxTile);
    CHECK(mm::Mirror::apply(buf, n, phone.data()));
    if (bytes) *bytes += n;
    tiles++;
  }
  return tiles;
}

void round_trip() {
  auto fb = frame(), phone = frame();
  mm::Mirror m(fb.data());
  // A flat background: every tile goes as one run.
  std::vector<uint16_t> bg(size_t(mm::kWidth) * mm::kHeight, 0x0841);
  m.blit(0, 0, mm::kWidth - 1, mm::kHeight - 1, bg.data());
  size_t bytes = 0;
  CHECK(drain(m, phone, &bytes) == mm::kTiles);
  CHECK(bytes == size_t(mm::kTiles) * (mm::kTileHeader + 3));
  CHECK(phone == fb);

  // Noise in an odd, partly off-screen area: raw tiles, clipped, only those tiles.
  srand(7);
  std::vector<uint16_t> noise(size_t(50) * 30);
  for (auto& v : noise) v = uint16_t(rand());
  m.blit(220, 300, 269, 329, noise.data());
  int tiles = drain(m, phone);
  CHECK(tiles == 2 * 3);  // x 220..239 → 2 tile columns, y 300..319 → 3 tile rows
  CHECK(phone == fb);
  CHECK(fb[size_t(300) * mm::kWidth + 220] == noise[0]);
  CHECK(fb[size_t(319) * mm::kWidth + 239] == noise[size_t(19) * 50 + 19]);

  // A new subscriber gets the whole frame.
  auto fresh = frame();
  m.mark_all();
  CHECK(drain(m, fresh) == mm::kTiles && fresh == fb);
}

void wire_format() {
  // Shared with android/screen's test: a tile of one colour.
  auto fb = frame();
  mm::Mirror m(fb.data());
  std::vector<uint16_t> px(size_t(mm::kTileW) * mm::kTileH, 0x1234);
  m.blit(16, 8, 31, 15, px.data());
  uint8_t buf[mm::kMaxTile];
  size_t n = m.next(buf, sizeof buf);
  CHECK(Bytes(buf, buf + n) == check::unhex("010010000810080180" "3412"));
  CHECK(m.next(buf, sizeof buf) == 0);
  uint8_t info[mm::kInfoSize];
  CHECK(mm::Mirror::info(info, sizeof info) == mm::kInfoSize);
  CHECK(Bytes(info, info + sizeof info) == check::unhex("0000f001401008"));
  // Malformed tiles are refused.
  auto phone = frame();
  Bytes bad = check::unhex("0100f00008100801803412");  // x 240: off screen
  CHECK(!mm::Mirror::apply(bad.data(), bad.size(), phone.data()));
  bad = check::unhex("010000000810080140" "3412");  // 64 of 128 pixels
  CHECK(!mm::Mirror::apply(bad.data(), bad.size(), phone.data()));
}

}  // namespace

void run_mirror() {
  round_trip();
  wire_format();
}

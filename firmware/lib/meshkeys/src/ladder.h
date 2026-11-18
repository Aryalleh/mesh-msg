// Navigation keys on one ADC pin (docs/handheld-proto.md §2, "Navigation
// keys"): a resistor ladder for boards without the keyboard module. The pin
// has 10 kΩ to 3.3 V; each key pulls it to GND through its own resistor, so
// every key gives its own voltage (one key at a time):
//
//   key     Up   Down  Left  Right  OK    Back  Menu  Call  End
//   to GND  0 Ω  1 k   2.2 k 4.7 k  6.8 k 10 k  15 k  22 k  39 k
//   × 3.3 V 0    0.091 0.180 0.320  0.405 0.500 0.600 0.688 0.796
//
// Debounced (30 ms), arrows repeat while held. Pure C++: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

#include "keys.h"

namespace mesh {
namespace keys {

constexpr size_t kLadderKeys = 9;
extern const Action kLadderActions[kLadderKeys];

// The key whose voltage mv is (of a vcc_mv supply), or -1 for none.
int ladder_key(uint32_t mv, uint32_t vcc_mv = 3300);

class Ladder {
 public:
  // One reading every few ms; returns an action to perform, or None.
  Action feed(uint32_t mv, uint32_t now_ms);
  int held() const { return stable_; }

 private:
  static constexpr uint32_t kSettleMs = 30, kRepeatAfter = 450, kRepeatEvery = 120;
  int last_ = -1, stable_ = -1;
  uint32_t since_ = 0, pressed_at_ = 0, repeated_at_ = 0;
};

}  // namespace keys
}  // namespace mesh

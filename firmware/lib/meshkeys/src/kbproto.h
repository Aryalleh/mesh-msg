// Keyboard module protocol (docs/handheld-proto.md §3): an RP2040 scans the
// keyboard (and the BlackBerry trackpad in version C) and is an I2C target on
// the handheld's shared I2C bus. The ESP32-S3 reads it when INT goes low.
//
//   address 0x1F, 100–400 kHz. Write one register byte, then read:
//
//   0x00 ID        read 4: 'M' 'K' version variant (mesh::keys::Variant)
//   0x01 EVENTS    read 1 + kEventsPerRead: count, then that many events
//                  (oldest first; popped by the read). Event byte:
//                  pressed << 7 | row << 3 | col
//   0x02 TRACK     read 2: dx, dy (int8, motion since the last read, saturated)
//   0x03 BACKLIGHT write 1 more byte: keyboard / trackpad backlight 0–255
//
//   INT (open drain, active low) is low while events or motion are pending.
//
// The module debounces (mesh::keys::Matrix); the ESP32 turns positions into
// text with mesh::keys::Layout, so layouts change without reflashing the
// module. Pure C++17, used by both firmwares and the host tests.
#pragma once

#include <cstddef>
#include <cstdint>

#include "keys.h"

namespace mesh {
namespace keys {

constexpr uint8_t kModuleAddr = 0x1F;
constexpr uint8_t kRegId = 0x00, kRegEvents = 0x01, kRegTrack = 0x02, kRegBacklight = 0x03;
constexpr uint8_t kProtoVersion = 1;
constexpr size_t kEventsPerRead = 15;
constexpr size_t kFifo = 64;

uint8_t encode_event(const Event& e);
Event decode_event(uint8_t b);

// Module side: queued events and motion, and the bytes a register read returns.
class ModuleState {
 public:
  explicit ModuleState(Variant v) : variant_(v) {}
  void push(const Event& e);  // drops the oldest when full
  void motion(int dx, int dy);
  bool pending() const { return count_ > 0 || dx_ != 0 || dy_ != 0; }
  // Fills out (cap ≥ 4 / 1 + kEventsPerRead / 2) for a read of reg; returns
  // the length. EVENTS and TRACK reads consume what they return.
  size_t read(uint8_t reg, uint8_t* out, size_t cap);
  // A host write: reg then data. Returns the new backlight level, or -1.
  int write(const uint8_t* b, size_t n);
  size_t queued() const { return count_; }

 private:
  Variant variant_;
  uint8_t q_[kFifo] = {};
  size_t head_ = 0, count_ = 0;
  int dx_ = 0, dy_ = 0;
};

// Host side: parse an EVENTS read.
size_t parse_events(const uint8_t* b, size_t n, Event* out, size_t cap);
// Host side: parse an ID read; false if it is not a keyboard module.
bool parse_id(const uint8_t* b, size_t n, Variant* v);

}  // namespace keys
}  // namespace mesh

// BlackBerry 9900 optical trackpad (version C, docs/handheld-proto.md §7):
// Avago ADBS-A320 optical finger navigation sensor on the shared I2C bus at
// 0x3B (product ID 0x83). NRST is on EN, SHTDWN on GND, MOTION_N is not wired:
// the motion register is polled. The click is a dome switch in the key matrix.
#pragma once

#include <cstdint>

namespace trackpad {

bool begin();  // false: no sensor (versions A and B, or not answering)
// Movement since the last call, in sensor counts; false = none.
bool read(int8_t* dx, int8_t* dy);

}  // namespace trackpad

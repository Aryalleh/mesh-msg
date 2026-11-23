// Hamyar presence on the board (docs/presence.md): when switched on in the setup panel (/presence) with the node
// id, key and report URL from the university's Hamyar admin (حضور و نودها → مکان‌ها و نودها), the board
//  - adds a beacon to its Bluetooth advert (alternating with the mesh advert every second) that the Hamyar app hears,
//  - collects the tokens phones advertise and, once per 120 s window, posts them signed over Wi-Fi.
// Needs a trusted clock (NTP / relay / RTC): no beacon and no report before that.
// Kept in its own file (/presence.bin), not in net::Settings, so older boards keep their settings.
#pragma once

#include <cstddef>
#include <cstdint>

namespace presence_node {

constexpr uint32_t kMagic = 0x4d505231;  // "MPR1"

struct Config {
  uint32_t magic = kMagic;
  uint8_t on = 0;
  uint8_t pad[3] = {};
  uint32_t node_id = 0;
  uint8_t key[32] = {};
  char url[128] = {};  // https://<slug>.hamyared.ir/presence/node
};

// After board::storage_begin().
void begin();
// Main loop (any lock): beacon per window, phones heard -> collector, report at the end of each window.
void loop();
const Config& config();
// From the setup panel; takes effect at once and is saved.
bool set(const Config& c);
// One line for the board log.
void status(char* out, size_t cap);

}  // namespace presence_node

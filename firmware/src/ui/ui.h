// The handheld's user interface (docs/handheld-proto.md §8), on LVGL:
//
//   status bar   battery, LoRa neighbours, Wi-Fi / relay, BLE / USB host, language
//   home         Chats · Network · Web access (iPhone) · Settings
//   chats, chat  conversations from the Messenger; typing with the hardware
//                keyboard (keyboard module) or the touch keyboard (Persian,
//                Latin, symbols; mesh::osk)
//   network      node status (LoRa, Meshtastic channel, airtime, relay)
//   web access   the address (and QR code) an iPhone on the same Wi-Fi opens
//   settings     device password, BLE passkey, LoRa module on/off, brightness, typing language
//
// Phone keys: 📞 call, ⛔ hang up / back to home, Menu = home, Back = back,
// Vol ± = brightness (volume once audio exists). Persian text is shaped and
// laid out right-to-left by LVGL (LV_USE_ARABIC_PERSIAN_CHARS, LV_USE_BIDI).
#pragma once

#include <cstdint>
#include <string>

#include "messenger.h"

namespace ui {

struct Status {
  std::string name, id, meshtastic_id, region, channel, relay, web_url, password;
  uint32_t freq_hz = 0, passkey = 0;
  int tx_dbm = 0, neighbours = 0, stored = 0, battery_pct = -1;
  uint32_t air_used_s = 0, air_budget_s = 0;
  bool usb = false, ble = false, wifi = false, setup_mode = false;
  uint32_t unix_time = 0;
  int radio = 0;  // LoRa: 1 working, 0 not found, -1 off (the "no LoRa module" setting)
};

// Settings → LoRa module: fitted = false skips the radio (saved in the node
// config). Called from the UI without the node lock; turning it back on
// probes for the chip (up to 20 s without one).
using RadioFn = void (*)(bool fitted);
void on_radio(RadioFn fn);

// Called by the UI about once a second (from ui::loop, outside the node
// lock: the function takes the lock itself).
using StatusFn = Status (*)();

// False if this build has no screen (the plain Mava board).
bool begin(StatusFn status, Messenger& messenger);
// Keyboard module, touch, LVGL timers. Call often from the main loop,
// without holding the node lock.
void loop(uint32_t now_ms);
bool ready();

// Input from a phone or PC showing the screen (remote.h): a touch at screen
// coordinates, or a key (keys::Action, code point for Text). False: unknown action.
void remote_touch(int x, int y, bool pressed);
bool remote_key(uint8_t action, uint32_t cp);
// An option from the phone: id 1 = the touch keyboard (value 0 hides it).
void remote_option(uint8_t id, uint8_t value);

}  // namespace ui

// The handheld's user interface (docs/handheld-proto.md §8), on LVGL:
//
//   status bar   battery, LoRa neighbours, Wi-Fi / relay, BLE / USB host, language
//   home         Chats · Network · Web access (iPhone) · Settings
//   chats, chat  conversations from the Messenger; typing with the hardware
//                keyboard (keyboard module) or the touch keyboard (Persian,
//                Latin, symbols; mesh::osk)
//   network      node status (LoRa, Meshtastic channel, airtime, relay)
//   web access   the address (and QR code) an iPhone on the same Wi-Fi opens
//   settings     device password, BLE passkey, LoRa module on/off, sound test, echo test,
//                brightness, typing language
//
//   call         incoming (answer / decline), calling, talking (mute, hang up):
//                comes up by itself while a call is live
//
// Phone keys: 📞 call (in a chat) / answer, ⛔ hang up or decline / back to home, Menu = home, Back = back,
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
  int audio_outputs = 0;  // speakers taking sound (the phone in pre-releases, later the codec)
  int mic_level = 0;      // % of full scale
  bool echo = false;      // the echo test is on
};

// Sounds (board_audio.h): kSoundKey on every key, kSoundTest from Settings.
constexpr uint8_t kSoundMessage = 1, kSoundKey = 2, kSoundTest = 3, kSoundRing = 4, kSoundBusy = 5;
using SoundFn = void (*)(uint8_t sound);
void on_sound(SoundFn fn);
// Settings → echo test: the microphone back to the speaker.
using EchoFn = void (*)(bool on);
void on_echo(EchoFn fn);

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
// The navigation keys on NAV_KEYS_PIN were found at boot (lib/meshkeys ladder.h).
bool nav_keys();
uint32_t nav_keys_mv();  // the pin's lowest reading at boot, for the log

// Input from a phone or PC showing the screen (hwsim.h): a touch at screen
// coordinates, or a key (keys::Action, code point for Text). False: unknown action.
void remote_touch(int x, int y, bool pressed);
bool remote_key(uint8_t action, uint32_t cp);
// An option from the phone: id 1 = the touch keyboard (value 0 hides it).
void remote_option(uint8_t id, uint8_t value);

}  // namespace ui

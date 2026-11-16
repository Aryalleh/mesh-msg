// Wi-Fi side of the board (docs/board.md §2, §5):
//
//   setup switch ON  → hotspot "Mava-XXXX" (WPA2, device password) with a
//                      captive portal: the setup panel opens by itself on the
//                      phone or laptop that joins. Station mode is off.
//   setup switch OFF → hotspot off; the board joins the saved Wi-Fi and the
//                      relay worker runs the gateway session with the relay:
//                      uploads what the relay does not have (with the relay's
//                      proof of work), long-polls /v1/gateway for receipts and
//                      for the identities announced near the board, and hands
//                      those packets to the node (LoRa + USB + BLE).
//
// The switch is read live: flipping it changes mode without a reboot.
#pragma once

#include <cstddef>
#include <cstdint>

#include "node.h"

namespace net {

constexpr uint32_t kSettingsMagic = 0x4d564e31;  // "MVN1"

struct Settings {
  uint32_t magic = kSettingsMagic;
  char wifi_ssid[33] = {};
  char wifi_pass[65] = {};
  char relay_url[96] = "https://eu-1.mfadem.ir";
  // Device password: hotspot (WPA2) and setup panel. Generated at first boot,
  // printed on the USB log (and on the board's label at assembly).
  char password[17] = {};
  uint32_t ble_passkey = 0;  // 6 digits, BLE pairing
  // Meshtastic channel (meshtastic::Channel): name + PSK (1 byte = short index).
  char channel[12] = "LongFast";
  uint8_t psk[32] = {1};
  uint8_t psk_len = 1;
  uint8_t relay_on = 1;  // run the gateway session when Wi-Fi is up
  uint8_t carrier = 0;   // also carry undelivered traffic for the relay's region
  uint8_t pad = 0;
  uint16_t quota_kb_h = 512;  // carrier budget
};

// What the main loop provides to the panel.
struct Hooks {
  // A new Meshtastic channel was saved (called with the node lock held).
  void (*channel_changed)(const Settings& s) = nullptr;
};

bool load(Settings& s);
bool save(const Settings& s);
// First boot: device password and BLE passkey.
void init_secrets(Settings& s);

// node_lock: a FreeRTOS mutex; the main loop holds it while it runs the
// node, the relay worker takes it for each node call.
bool begin(Settings& s, mesh::node::Node& node, void* node_lock, const Hooks& hooks);
// Switch, hotspot, portal and web panel. Main loop, node lock held.
void loop(mesh::route::Millis now);

enum class Mode : uint8_t { Off, Setup, Station };
Mode mode();
bool wifi_up();
const char* relay_state();  // short text for logs and the panel

}  // namespace net

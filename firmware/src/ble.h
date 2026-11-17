// Bluetooth (docs/board.md §4), no pairing (owner's choice), up to 3 phones:
//
//   Phones' mesh  6d657368-6d73-6700-…  (RX 6701 write, TX 6702 notify)
//     The board is a peer in the phones' own Bluetooth mesh (BleProto.kt,
//     lib/meshnode blemesh.h): phones find and use it like another phone.
//     Host ports 3–5, packets only.
//   Host link     6d657368-6c72-0000-…  (RX 0001 write, TX 0002 notify)
//     The apps' "LoRa node" stream, as over USB, host port 1. Untrusted over
//     Bluetooth (no pairing): packets and status; settings, Wi-Fi, reboot and
//     wipe only over USB or the setup panel.
#pragma once

#include <cstddef>
#include <cstdint>

#include "node.h"

namespace ble {

// id: 6 bytes of the node identity; the board's Bluetooth address is made
// from them (random static), not the chip's MAC.
bool begin(const char* name, const uint8_t id[6]);
// Moves bytes between the BLE link and node host port 1. Call from the main
// loop with the node lock held.
void loop(mesh::node::Node& node, mesh::route::Millis now);
bool connected();
int mesh_peers();  // phones in the Bluetooth mesh right now
// One line for the board log: address, advertising, links, connects, hellos,
// drops and the last drop's reason.
void status(char* out, size_t cap);

constexpr size_t kPort = 1;

}  // namespace ble

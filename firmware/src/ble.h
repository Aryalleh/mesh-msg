// Phone link over BLE (docs/firmware.md §6, docs/board.md §4): the host-link
// byte stream on a GATT service, host port 1 of node::Node. The same stream
// as USB, so the phone apps speak to the board exactly as meshd does over USB.
//
//   Service  6d657368-6c72-0000-8000-00805f9b34fb
//     RX     6d657368-6c72-0001-8000-00805f9b34fb  write (encrypted, MITM)   phone → node
//     TX     6d657368-6c72-0002-8000-00805f9b34fb  notify                    node → phone
//
// Writing needs a bonded link (6-digit passkey, shown in the setup panel and
// on the USB log), so nobody in radio range can send commands such as WIPE.
#pragma once

#include <cstddef>
#include <cstdint>

#include "node.h"

namespace ble {

bool begin(const char* name, uint32_t passkey);
// Moves bytes between the BLE link and node host port 1. Call from the main
// loop with the node lock held.
void loop(mesh::node::Node& node, mesh::route::Millis now);
bool connected();

constexpr size_t kPort = 1;

}  // namespace ble

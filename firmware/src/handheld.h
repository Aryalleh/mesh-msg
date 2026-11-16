#pragma once

#include <cstddef>
#include <cstdint>

#include "node.h"
#include "meshtastic_backend.h"

namespace mesh::handheld {

// Stand-alone Mava handheld runtime.
//
// BLE is the phone/UI link. LoRa is Meshtastic-compatible transport.
// Wi-Fi/Internet is optional and intentionally kept separate from the
// LoRa packet path.
class Handheld {
 public:
  static constexpr size_t kBlePacketMax = 512;

  bool begin(node::Node& node);
  void loop(node::Millis now);

  bool ble_ready() const { return ble_ready_; }
  bool meshtastic_ready() const { return meshtastic_ready_; }

 private:
  node::Node* node_ = nullptr;
  meshtastic::Backend meshtastic_;
  bool ble_ready_ = false;
  bool meshtastic_ready_ = false;

  bool push_ble_to_node(const uint8_t* data, size_t len, node::Millis now);
  void notify_phone(const uint8_t* data, size_t len);
};

}  // namespace mesh::handheld

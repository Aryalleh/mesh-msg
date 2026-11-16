#pragma once
#include <cstddef>
#include <cstdint>

#include "node.h"

namespace mesh::gateway {

// ESP32-S3 gateway: BLE GATT RX -> Mava router/LoRa + optional HTTP relay.
// BLE callbacks only enqueue fixed-size packets; all routing happens from loop().
class Gateway {
 public:
  static constexpr size_t kMaxPacket = mesh::kLoRaMTU;
  static constexpr size_t kQueueDepth = 8;

  Gateway() = default;

  bool begin(node::Node& node);
  void loop(node::Millis now);
  // Called by the BLE GATT callback; only copies into the fixed RX queue.
  bool push_ble(const uint8_t* data, size_t len);

  bool ble_ready() const { return ble_ready_; }
  bool wifi_ready() const { return wifi_ready_; }
  uint32_t ble_rx_dropped() const { return ble_rx_dropped_; }
  uint32_t lora_forwarded() const { return lora_forwarded_; }
  uint32_t relay_queued() const { return relay_queued_; }
  uint32_t relay_failed() const { return relay_failed_; }

 private:
  struct Item {
    uint16_t len = 0;
    uint8_t data[kMaxPacket] = {};
  };

  bool forward_lora(const Item& item, route::Millis now);
  void relay_task();

  node::Node* node_ = nullptr;
  Item rx_[kQueueDepth] = {};
  volatile uint8_t rx_head_ = 0;
  volatile uint8_t rx_tail_ = 0;

  bool ble_ready_ = false;
  bool wifi_ready_ = false;
  uint32_t ble_rx_dropped_ = 0;
  uint32_t lora_forwarded_ = 0;
  uint32_t relay_queued_ = 0;
  uint32_t relay_failed_ = 0;

  void* relay_queue_ = nullptr;
  void* relay_task_handle_ = nullptr;
};

}  // namespace mesh::gateway

// SX1262 through RadioLib, in the shape node::Node expects (docs/firmware.md
// §4, §5): a frame is a raw LoRa payload (sender short ID || packet), modulation
// comes from the node's preset — 8-symbol preamble, explicit header, CRC on, low
// data rate optimisation chosen by RadioLib exactly as mac::airtime_us assumes
// — private sync word, frequency / power from the node's config.
#pragma once

#include <cstddef>
#include <cstdint>

#include "hostlink.h"

namespace radio {

bool begin(const mesh::host::NodeConfig& cfg);      // false: no radio found
bool configure(const mesh::host::NodeConfig& cfg);  // retune live

// Non-blocking transmit; tx_done() becomes true when it is over.
bool transmit(const uint8_t* frame, size_t n);
bool transmitting();
// True once per finished transmission (and puts the radio back in RX).
bool tx_done();

// A received frame, if one is waiting: copies into buf, returns its length
// (0 = none). snr_db is the packet SNR.
size_t receive(uint8_t* buf, size_t cap, int* snr_db);

// Channel activity detection: blocks for a few symbols. True = busy.
bool cad_busy();

const char* chip_name();

}  // namespace radio

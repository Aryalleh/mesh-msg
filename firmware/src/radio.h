// SX1262 through RadioLib on the Meshtastic backbone (docs/board.md §3): a
// frame is a Meshtastic radio packet (meshtastic::Backend wraps the Mava
// packet); modulation is LongFast — SF11, 250 kHz, 4/5, 16-symbol preamble,
// sync word 0x2B, explicit header, CRC on, as mac::kMeshtasticLongFast
// assumes. Frequency and power come from the node's config.
#pragma once

#include <cstddef>
#include <cstdint>

#include "hostlink.h"

namespace radio {

bool begin(const mesh::host::NodeConfig& cfg);      // false: no radio found
bool configure(const mesh::host::NodeConfig& cfg);  // retune live
bool ready();                                       // begin() found the chip
void off();  // the "no LoRa module" setting: chip (if any) to sleep, nothing more sent

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

# Mava DIY ESP32-S3 handheld

This target is the stand-alone Mava handheld, not the old phone gateway.

## Hardware

Target MCU/module:

- ESP32-S3-N16R8
- 16 MB flash
- 8 MB octal PSRAM
- external SX1262 LoRa radio
- USB-C for flashing/debugging
- BLE for the phone/UI link
- Wi-Fi is available for a future Internet-relay transport

The repository includes a portable PlatformIO board definition at `firmware/boards/esp32-s3-n16r8.json`.

## Default SX1262 wiring

The first prototype target uses:

| SX1262 | ESP32-S3 |
|---|---:|
| SCK | GPIO12 |
| MISO | GPIO13 |
| MOSI | GPIO11 |
| NSS/CS | GPIO10 |
| DIO1 | GPIO1 |
| RESET | GPIO5 |
| BUSY | GPIO4 |
| 3V3 | 3V3 |
| GND | GND |

These are build defaults, not a claim about the pinout of every SX1262 breakout. Override the `RADIO_*` defines in `platformio.ini` for the actual module.

## Software architecture

`MavaPacket` remains the application packet.

```text
phone
  |
  | BLE / Mava hostlink
  v
ESP32-S3 handheld
  |
  +-- Mava identity / router / store / transfer
  |
  +-- Meshtastic backend
         |
         +-- Meshtastic 16-byte radio header
         +-- Data protobuf, private port 256
         +-- Meshtastic channel AES-CTR
         +-- SX1262
```

Meshtastic is therefore the LoRa transport/relay network. Mava does not become a Meshtastic text message.

## Meshtastic compatibility

The backend implements the Meshtastic on-air header and the `Data` protobuf wrapper for private application port `256`. Meshtastic documents ports 256-511 as the private application range.

The current default is the public LongFast channel convention:

- channel name: `LongFast`
- short PSK index: `1`
- expanded PSK: Meshtastic's published 16-byte default key
- channel hash: calculated from channel name + expanded PSK
- hop limit: 3
- LongFast modulation: SF11, 250 kHz, CR 4/5
- EU_868 default frequency: 869.525 MHz

Mava's encrypted packet is the payload of the private application. The Mava packet is not re-encrypted as a Meshtastic text message.

## Current scope

Implemented in this branch:

1. Stand-alone ESP32-S3-N16R8 PlatformIO target.
2. Bidirectional BLE host link.
3. Existing Mava identity/router/store/transfer stack runs locally.
4. Meshtastic-compatible 16-byte radio header.
5. Private Meshtastic application port 256.
6. Meshtastic channel AES-CTR for the default public channel.
7. Conversion between Mava packets and Meshtastic radio frames.
8. Inbound Meshtastic private-app frames are injected into the Mava router.
9. Outbound Mava packets are wrapped as Meshtastic private-app frames.

Not yet implemented:

- Meshtastic NodeInfo/NodeDB synchronization.
- Meshtastic PKI direct-message encryption.
- Meshtastic AEAD channels.
- Automatic discovery/configuration of a non-default channel.
- Meshtastic frequency-slot calculation for every region/preset.
- Display/keyboard/GPS/audio hardware.

Those are intentionally separate milestones; the handheld can first prove Mava <-> Meshtastic private-app interoperability on the default channel.

## Build

From `firmware/`:

    pio run -e mava_handheld_s3

Upload:

    pio run -e mava_handheld_s3 -t upload

Monitor:

    pio device monitor -b 115200

For the first hardware test, use the same Meshtastic region/preset/channel settings on the nearby Meshtastic node. The radio must be on the same frequency/preset and channel key to receive encrypted channel traffic.
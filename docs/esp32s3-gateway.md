# ESP32-S3 Mava gateway

This framework turns an ESP32-S3 + SX1262 board into a Mava fan-out gateway:

```text
phone/app
   │ BLE GATT write
   ▼
ESP32-S3 Gateway
   ├──► Mava router ──► SX1262 ──► LoRa mesh
   └──► Wi-Fi worker ──► POST /v1/packets ──► Internet relay
```

The BLE payload is a raw Mava packet. The gateway does not convert it to a Meshtastic text message and does not replace Mava packet IDs. The same encrypted Mava packet is handed to both transports.

## BLE

The gateway uses NimBLE on ESP32-S3 and exposes:

- service: `7b2f0001-5a56-4d61-7661-4d6176610001`
- RX/write: `7b2f0002-5a56-4d61-7661-4d6176610001`
- TX/notify: `7b2f0003-5a56-4d61-7661-4d6176610001`

A phone writes one complete Mava packet to RX. The callback only copies it into a fixed queue; routing is performed from the normal Arduino loop.

ESP32-S3 supports BLE and Espressif recommends NimBLE for BLE-only applications because it has lower memory/code overhead than Bluedroid.

## LoRa

`node::Node::send_raw()` validates the packet and submits it to the existing Mava router. The gateway therefore uses exactly the same MAC, CAD, duty-cycle, hop and retransmission path as packets received from the existing host link.

The BLE packet must not contain the one-byte LoRa link header.

## Internet

The Wi-Fi side runs in a separate FreeRTOS worker so a slow HTTPS request does not block LoRa processing.

The current framework sends the relay's normal binary packet stream:

`uint32_be(length) || raw_mava_packet`

to:

`POST /v1/packets`

with `Content-Type: application/octet-stream`.

### Important production limitation

The current Mava relay protects anonymous `/v1/packets` uploads with proof of work. This ESP32 framework deliberately does not fake or bypass that check.

For bench testing, use a relay configuration with PoW disabled/allowlisted. For production, the next step is an authenticated gateway session or an asynchronous ESP32 PoW worker.

HTTPS currently uses `setInsecure()` as a development placeholder. Production firmware must pin the relay CA/certificate.

## Build configuration

Do not commit Wi-Fi credentials. Supply them as PlatformIO build flags:

```text
-DMAVA_WIFI_SSID="YOUR_WIFI"
-DMAVA_WIFI_PASSWORD="YOUR_PASSWORD"
-DMAVA_RELAY_URL="https://relay.example"
```

The defaults in `gateway_config.h` are empty, so the firmware still works as a normal LoRa node when no gateway network is configured.

## Meshtastic relationship

This gateway does not turn the ESP32 into a Meshtastic node.

The intended stack is:

```text
Mava packet
   │
   ├── native Mava LoRa transport
   │
   └── future MeshtasticTransport
          │
          └── Meshtastic application packet / port 256
```

So the BLE gateway can coexist with the Meshtastic transport work already in this branch without changing Mava's end-to-end packet semantics.
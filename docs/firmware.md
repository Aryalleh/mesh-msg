# LoRa Node Firmware ("hardware framework")

## 1. Hardware targets

| Board | MCU | Radio | Use |
|---|---|---|---|
| Heltec WiFi LoRa 32 V3 | ESP32-S3 | SX1262 | client / gateway (cheap, has Wi-Fi, OLED) |
| LilyGO T-Beam Supreme | ESP32-S3 | SX1262 | client with GPS, 18650 holder |
| RAKwireless RAK4631 (WisBlock) | nRF52840 | SX1262 | solar repeater (very low power) |
| LilyGO T-Echo | nRF52840 | SX1262 | pocket client, e-ink |

All share the SX1262, so one radio driver. MCU differences are hidden behind a small HAL.

**Repeater kit (rooftop):** RAK4631 + 5–8 dBi omni (or Yagi for fixed point-to-point
backbone links) + 6 W solar panel + LiFePO4 cell + IP65 box. Antenna height matters more than
power: 10 km between cities needs line of sight from elevated sites.

## 2. Node roles

| Role | Phone link | Forwards LoRa | Wi-Fi uplink | Power |
|---|---|---|---|---|
| CLIENT | BLE / USB | yes (low priority) | no | battery |
| REPEATER | none (config only) | yes | no | solar, deep sleep between RX |
| GATEWAY | optional | yes | yes → relay server | mains |

Role is a runtime config value, not a different build.

## 3. Software architecture

```
firmware/
  platformio.ini            envs: heltec_v3, tbeam_s3, rak4631, techo
  src/
    main.cpp                setup + cooperative loop / FreeRTOS tasks
    hal/                    Board pins, power, LED, display, battery ADC
    radio/RadioIface        RadioLib SX1262 wrapper: TX, RX, CAD, presets
    proto/PacketCodec       outer header encode/decode (spec/ test vectors)
    proto/Announce          sign/verify announces, receipts (Monocypher Ed25519)
    route/Router            route table, dedup cache, forwarding decision
    route/Summary           Bloom filter build/merge
    store/PacketStore       LittleFS ring buffer, expiry, quotas
    mac/Airtime             listen-before-talk (CAD), random backoff, duty-cycle budget
    link/PhoneLinkBle       GATT service (RX/TX characteristics) + framing
    link/PhoneLinkSerial    USB-CDC framing (same protocol as BLE)
    link/WifiUplink         HTTPS client to relay (GATEWAY role)
    config/Config           NVS/flash config, BLE/serial config commands
  test/                     native unit tests (PlatformIO `native` env)
```

The `proto/` and `route/` modules are pure C++ with no Arduino dependencies so they run in
the `native` test env and can be fuzzed.

## 4. Radio presets

| Preset | SF / BW / CR | ≈ bitrate | ≈ airtime 200 B | Use |
|---|---|---|---|---|
| FAST | SF7 / 250 kHz / 4/5 | 11 kbps | 0.2 s | dense local, short range |
| BALANCED | SF9 / 125 kHz / 4/5 | 1.8 kbps | 1 s | default backbone |
| LONG | SF11 / 125 kHz / 4/8 | 0.4 kbps | 5 s | hard links, last resort |

Frequency plan is configurable (433 / 868 / 915 bands). **Check local regulations before
transmitting** — band, power and duty-cycle limits vary by country; the default build ships
with EU-868-style limits (1% duty cycle in most sub-bands) enforced by `mac/Airtime`.

## 5. MAC behaviour

- Listen-before-talk using SX1262 Channel Activity Detection, random backoff.
- Hop-level ACK (LINK type) for unicast forwards, up to 3 retries with jittered backoff.
- Announce rate limit: own announce ≤ 1 / 30 min; forwarded announces ≤ 10 % airtime.
- SUMMARY broadcast every 15 min or on significant change.
- Priority queue: RECEIPT > DATA(PRIORITY) > DATA > SUMMARY > ANNOUNCE.

## 6. Phone link (BLE GATT)

```
Service  6e6d0001-...           (UUID to be generated)
  RX  (write w/o response)  phone → node: framed packets / commands
  TX  (notify)              node → phone: framed packets / events
```

Frame: `kind(1) | len(2) | payload`. `kind`: 0 = mesh packet, 1 = command, 2 = event.
Commands: get status, set role/preset/frequency, list stored packets, reboot, wipe.
Pairing: BLE passkey (shown on display, or printed on device label for headless boards).

The node never sees plaintext: it stores and forwards ciphertext only. `wipe` erases store,
config and node identity.

## 7. Milestones

1. Two boards exchange raw packets; serial console shows decoded headers.
2. Router + store + hop ACKs across 3 boards (A → repeater → B).
3. BLE phone link; Android app sends text through 2 nodes.
4. Gateway Wi-Fi uplink to relay server.
5. Low-power repeater build on RAK4631 with solar; field range test.

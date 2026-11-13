# LilyGO T-Deck Plus: a stand-alone handheld

Status: **board support built (step T0); everything else here is a plan, not code.**

## 1. The device

| Part | Chip | Pins |
|---|---|---|
| MCU | ESP32-S3, 16 MB flash, 8 MB PSRAM, Wi-Fi + Bluetooth LE | native USB (CDC) |
| LoRa | SX1262, 868 MHz | CS 9, DIO1 45, RST 17, BUSY 13; shared SPI SCK 40, MISO 38, MOSI 41 |
| Display | ST7789, 320×240 | CS 12, DC 11, backlight 42 (same SPI) |
| SD card | | CS 39 (same SPI) |
| Keyboard | ESP32-C3 behind I2C 0x55 | SDA 18, SCL 8, INT 46 |
| Trackball | 4 hall switches + click | 3, 15, 1, 2, click 0 |
| Microphone | ES7210 (I2S + I2C) | MCLK 48, LRCK 21, SCK 47, DIN 14 |
| Speaker | MAX98357A (I2S) | BCK 7, WS 5, DOUT 6 |
| GPS (Plus) | UART | TX 43, RX 44 |
| Power | peripheral rail enable | GPIO 10 (must be high); battery ADC GPIO 4 |

It has every link the network uses (LoRa, Bluetooth, Wi-Fi) plus a screen, a keyboard, a
microphone and a speaker, so it can be more than a LoRa node: a messenger that needs no
phone.

## 2. What that takes

A LoRa node (firmware.md) only moves ciphertext; it has no account, no contacts and no
plaintext. A stand-alone device is a **client**: it needs what today exists only in the Go
core, written again in C++ for the microcontroller (Go does not run on the ESP32):

| Piece | Go today | On the T-Deck |
|---|---|---|
| Packet codec, announces, receipts, router, store | `core/packet`, `router`, `store` | exists in `firmware/lib` (meshproto, meshroute, meshnode) |
| End-to-end encryption (Noise K/X: X25519, ChaCha20-Poly1305, SHA-256) | `core/e2e` | new `lib/meshe2e` on Monocypher (X25519, ChaCha20-Poly1305 IETF) + the SHA-256 already there + HMAC/HKDF |
| Text encoding (compact Persian) | `core/textenc` | new, checked by the existing text vectors |
| Contacts, messages, groups | `core/node` (SQLite) | new, small files on LittleFS / SD card |
| Relay client | `core/relayclient` (HTTP) | new, over Wi-Fi: packets, mailbox, receipts, proof-of-work |
| Bluetooth mesh with phones | Android `BleMesh` | new, NimBLE: same GATT service and framing |
| Direct links + calls | `core/direct`, `core/call` | new, a subset (§5) |
| User interface | Android / web | new, LVGL on the ST7789 with keyboard and trackball |

Each piece is kept identical to the Go core by `spec/test-vectors` (new vector sections for
e2e, groups, call signaling), as the firmware's packet code is today, and is tested on the
host with `make -C firmware test` before it runs on a board.

## 3. Steps

| Step | Result | Needs the device to verify |
|---|---|---|
| **T0** (done) | `pio run -e tdeck_plus`: runs as a LoRa node over USB with `meshd` or a phone, like the other boards | radio pins |
| **T1** | screen, keyboard, trackball, battery; status screens (node, radio, neighbours); settings on the device; Bluetooth host link so a phone uses it as its LoRa node | yes |
| **T2** | identity on the device, imported remotely (§4) | yes |
| **T3** | stand-alone messaging: contacts, chats, send and receive text over LoRa, Bluetooth and a relay over Wi-Fi; receipts; private groups | yes |
| **T4** | voice calls over Wi-Fi (§5) | yes |
| later | voice messages (record / play), files on the SD card, GPS position sharing | |

T1–T4 can't be finished blind: display, keyboard, audio and radio all need the device on
the desk. Protocol code is written and tested on the host first.

## 4. Getting an identity onto the device (T2)

Nobody can type a 64-byte secret on that keyboard, and the passphrase backup
(`meshkey1:`, Argon2id with 64 MiB) can't be opened on a device with 8 MB of RAM. So the
identity is **sent to the device** from a phone or PC that already has it:

1. On the device: *Settings → Receive identity*. It makes a one-time X25519 key and shows a
   6-digit code derived from it on its screen.
2. On the sender — `mesh identity push` on a PC (USB cable), or *Send my identity to a
   device* in the Android app (Bluetooth or USB OTG) — the user types the 6 digits.
3. The sender checks the code against the device's key, seals the 64-byte seed to that key
   (Noise N) and sends it over the host link (new commands `IDENTITY_OFFER`,
   `IDENTITY_PUT`). The code stops another device, or a tampered cable, from getting it.
4. The device stores the seed in encrypted flash (ESP32 flash encryption + NVS encryption),
   behind a device PIN; `WIPE` and too many wrong PINs erase it.

Contacts go the same way (the sender pushes the contact list), or one by one as
`meshmsg:` links over the cable, or by QR code shown on the phone when a camera is not
available: typed on the PC, not on the device.

Open: one account is meant to run on one device at a time (desktop README). Either the
T-Deck gets **its own identity** made on the device (simplest; contacts add it like any
person), or the account **moves** to it. Using one account on phone and T-Deck at once
needs multi-device support that does not exist yet.

## 5. Calls on the device (T4)

- Audio: ES7210 microphone in, MAX98357A speaker out, both I2S, 16 kHz mono, 20 ms frames.
- Codec: Opus (codec id 1, calls.md), fixed-point build, low complexity; an ESP32-S3
  encodes and decodes one 16 kHz voice stream in real time. AMR-WB is not built.
- Path: Wi-Fi only, and only a direct path, as for phones (calls.md §1). A subset of
  `core/direct`: host and reflexive candidates, authenticated probes, hole punching; no
  router port mapping or symmetric-NAT methods at first. Never over LoRa or Bluetooth.
- Signaling: content type 7 over any path (LoRa included: an OFFER is a few fragments).
- No echo canceller on the device: the speaker is next to the microphone, so calls start
  as push-to-talk (or with a headset on the audio jack), full duplex later.

## 6. Security

A stand-alone T-Deck holds plaintext, contacts and the account key, unlike a LoRa node. The
rule "no plaintext or contact data on LoRa nodes" still holds for nodes; a T-Deck in
stand-alone mode is a client and gets a client's protection: encrypted flash, device PIN,
auto-lock, wipe on wrong PINs, no debug console with secrets, and the same threat model as
a seized phone.

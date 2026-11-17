# Mava custom board

The one LoRa device Mava supports (decision D22, 2026-11-16): a custom board with an
ESP32-S3-N16R8 and an SX1262. All earlier boards (Heltec V3, T-Beam S3, T-Deck Plus,
RAK4631, T-Echo) are dropped. Firmware: `firmware/`, PlatformIO env `mava_handheld_s3`.

The board carries Mava traffic in **both directions on all three links at once**:

```
                    ┌──────────────── Mava board ─────────────────┐
 phone (app) ◄─BLE──┤ host port 1                                 │
 PC (meshd) ◄─USB───┤ host port 0      node::Node                 ├──LoRa──► Meshtastic backbone
 phone (OTG)        │                  router · MAC · store       │          (any Meshtastic node repeats)
                    │ relay worker ◄──── uplink sink              │
                    └───────┬─────────────────────────────────────┘
                            │ Wi-Fi (switch off)   │ hotspot + panel (switch on)
                            ▼                      ▼
                     internet relay          setup from a phone or laptop
```

Every packet that enters on one side is stored once and handed to every other side: the
store keeps one "already has it" bit per USB host, BLE host and uplink, so a side that drops
out gets exactly what it missed when it comes back. Mava packets are end-to-end encrypted
before they reach the board; the board only ever sees ciphertext.

Phones and laptops are full nodes of the same mesh: each runs relay + Bluetooth itself, and
uses LoRa through a board on USB (meshd, Android OTG) or Bluetooth (iPhone, Android).

## 1. Hardware

The board is built as the **handheld** (2.4" ILI9341 touch screen, WM8960 audio, 49-key
matrix): the full wiring is in [handheld-proto.md](handheld-proto.md). The parts board.md
relies on:

| Part | Pin(s) |
|---|---|
| ESP32-S3-N16R8 (16 MB flash, 8 MB octal PSRAM) | native USB-C (CDC) for flashing, log and the USB host link |
| SX1262 | SCK 12, MISO 13, MOSI 11 (the radio's own bus), CS 10, DIO1 1, BUSY 4, NRESET 8; TCXO 1.8 V; DIO2 drives the RF switch |
| Setup switch (physical, latching) | GPIO 14 to GND |

The pins are build flags in `firmware/platformio.ini`. The board definition is
`firmware/boards/esp32-s3-n16r8.json`.

## 2. Setup: hotspot, captive portal, switch

1. **First boot** generates the node identity and a 6-digit **BLE passkey**. The **device
   password** (hotspot and panel) is **`12345678`** on every board until you set your own in
   the panel (kept from then on; a full erase or *Install everything* brings back the
   default). Both are printed on the USB log (`pio device monitor`). Change it: anyone who
   knows the default can open the panel while the hotspot is on. The log repeats the
   hotspot name, device password and BLE passkey every 10 s until a host attaches, and
   each boot step is logged (`boot: Wi-Fi`, `boot: radio`, …) so a step that hangs shows.
2. **Setup mode: switch ON, no Wi-Fi saved yet, or a host asked for it.** A new board (or
   one without the switch fitted) opens the hotspot by itself. The board stops using Wi-Fi as a client and opens the hotspot
   `Mava-XXXX` (WPA2, device password). A phone or laptop that joins gets the panel by
   itself (captive portal: every DNS name answers 192.168.4.1, and the Android, iOS/macOS and
   Windows connectivity checks are redirected to the panel). The panel asks for the device
   password once per session.
3. In the panel: Wi-Fi network (a list from a scan, or typed) and password; relay address
   (default `https://eu-1.mfadem.ir`), relay on/off, carrier mode and its budget; LoRa region,
   frequency and power; Meshtastic channel name and key; node name; a new device password.
   It also shows the node's status (identity, Meshtastic node number, neighbours, stored
   packets, airtime, USB/BLE host, relay state) and the BLE passkey.
   After *Save* with the switch off, the hotspot closes a few seconds later and the board
   joins the network.
4. **Switch OFF and Wi-Fi saved → normal mode.** The hotspot goes off, the board joins the
   saved Wi-Fi, sets its clock by NTP (so it announces itself without a host) and starts the
   relay worker. Flip the switch ON again at any time to change settings; no reboot needed.

**Without the hotspot**: a host on USB or BLE (trusted ports only, never the web port) sets
the Wi-Fi with the host-link command `SET_WIFI` (network, password, relay) and opens or
closes the hotspot with `HOTSPOT 1/0`:

```
mesh lora wifi ssid="Home" password="…"     # meshd on a PC, board on USB
mesh lora hotspot on                         # open the setup hotspot now (off: back to normal)
```

The Android flasher has the same as **Board Wi-Fi** (docs/flashing.md). Without saved
Wi-Fi the board still works fully on LoRa, BLE and USB, with its hotspot open.

## 3. LoRa: the Meshtastic backbone

The radio speaks Meshtastic on the air, so the board joins existing Meshtastic meshes and
every Meshtastic node in range repeats Mava traffic:

- Modulation: LongFast (SF11, 250 kHz, CR 4/5, 16-symbol preamble, sync word 0x2B). The
  frequency is the region's (EU_868: 869.525 MHz, US_915: 906.875 MHz — LongFast's defaults);
  set it in the panel if the local mesh uses another slot.
- Frame: Meshtastic's 16-byte header, then a `Data` protobuf with `portnum = 256`
  (PRIVATE_APP) and the Mava packet as payload, encrypted with the channel key (AES-CTR,
  nonce = packet id ‖ sender). Default channel: `LongFast` with the public default key, so
  Meshtastic nodes relay it; a private channel works too, but then only nodes with that
  channel relay it. Mava's own end-to-end encryption is unchanged either way.
- **Packet size: Meshtastic payloads are at most 233 bytes**, so Mava's LoRa MTU is 233
  (`core/packet.LoRaMTU`, `firmware/lib/meshproto/src/packet.h`); one frame carries about
  150 Persian characters for a known contact.
- **No double flooding.** When the Mava router rebroadcasts a packet it first heard from
  Meshtastic, the adapter sends it as Meshtastic would: original sender and packet id,
  hop limit − 1, itself as relay node. Meshtastic nodes that already heard it drop it. Our
  own retries reuse the first id. A Meshtastic node relaying our own packet is the Mava
  router's implicit ACK.
- The MAC times frames with LongFast airtime (about 2.16 s for a full frame) against the
  region's duty-cycle budget.

Code: `firmware/lib/meshtastic` (pure C++, host-tested in `firmware/test/meshtastic_test.cpp`).

## 4. Bluetooth

No pairing (owner's choice, 0.1.1-beta14): phones connect directly. Up to 3 phones at once
(NimBLE's connection limit). Two GATT services:

- **The phones' own Bluetooth mesh** (`6d657368-6d73-6700-…`, android `BleProto.kt`): the board
  advertises it exactly as a phone does (service UUID + manufacturer data 0xFFFF with a
  session ID). Its session ID is 0, the lowest, so every phone connects to it as GATT client
  and nothing has to be tapped: Mava phones nearby use the board like another phone. After
  the phone's hello (0x4D + 4 bytes) both directions carry `len(3) || packet`
  (`lib/meshnode/src/blemesh.*`, host-tested). Each phone is a host port of the node
  (ports 3–5, packets only): what it sends goes out on LoRa and to the other phones, USB,
  the browser and the relay, and what the node holds streams back. Packets larger than
  LoRa's 233 bytes (photos, voice) are skipped by the board.
- **Host link** (`6d657368-6c72-0000-…`): the apps' "LoRa node" stream, exactly as over USB,
  host port 1 (Android: Network → *Connect board over Bluetooth*, `BleLoRa.kt`; iPhone:
  `ios/Mava/LoRaBle.swift`). Without pairing, this port is **untrusted**: packets and status
  only; settings, Wi-Fi, reboot and wipe are refused and stay on USB and the setup panel.

The advert holds the mesh service and session ID (29 of 31 bytes); the host-link service and
the name `Mava-XXXX` are in the scan response.

## 5. Internet: the relay worker

With Wi-Fi up, a FreeRTOS task runs the gateway session (`POST /v1/gateway`, docs/server.md
§4) in a loop:

- **Up:** up to 16 packets the relay does not have yet (heard on LoRa, sent by a USB or BLE
  host), with the relay's proof of work (`X-PoW`, 16 bits on eu-1: well under a second on the
  ESP32-S3). If the upload fails, the packets are handed back and sent next time.
- **Down:** long-poll (10 s) for receipts and for DATA to the identities announced near the
  board (on LoRa or by its hosts; those identities must allow gateways to fetch for them).
  With carrier mode on, also undelivered traffic of the relay's region, within the budget.
  What arrives is broadcast on LoRa and handed to the USB and BLE hosts.
- The cursor is saved to flash (every 5 minutes) so a reboot does not refetch everything.
- `https://` relays are checked against Let's Encrypt's roots (`firmware/src/roots.h`);
  `http://` works for a relay on a LAN.

## 5a. Browser access

On the home Wi-Fi (or its own hotspot) the board serves the Mava web client at `/app/`
and links browsers into the mesh as host port 2: [web-access.md](web-access.md).

## 6. Not done yet

- Not run on hardware: no board in the build container. Verify pins, radio, BLE pairing with
  iOS and Android, captive portal on Android/iOS/Windows/macOS.
- Android app: LoRa over Bluetooth (iOS has it; Android only has USB).
- meshd: Bluetooth to phones (laptops reach phones through relays and LAN today).
- Meshtastic frequency slots other than the region default are set by hand.
- Display, battery measurement and deep sleep: the prototype has none.

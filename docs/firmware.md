# LoRa Node Firmware ("hardware framework")

## 1. Hardware target

One board: the **Mava custom board** (ESP32-S3-N16R8 + SX1262), described in
[board.md](board.md) — its pins, the setup switch and hotspot, the Meshtastic backbone,
BLE and the Wi-Fi relay worker. Earlier off-the-shelf boards (Heltec V3, T-Beam S3, T-Deck
Plus, RAK4631, T-Echo) are no longer supported (decisions.md D22).

On air the board speaks Meshtastic (LongFast, Mava packets as PRIVATE_APP payloads), so the
sections below on the native LoRa link header, presets and region plans describe the Mava
router and MAC, which run unchanged on top: the router's frames go through the Meshtastic
adapter (`lib/meshtastic`) instead of straight to the radio.

## 2. Node roles

| Role | Phone link | Forwards LoRa | Wi-Fi uplink | Power |
|---|---|---|---|---|
| CLIENT | BLE / USB | yes (low priority) | no | battery |
| REPEATER | none (config only) | yes | no | solar, deep sleep between RX |
| GATEWAY | optional | yes | yes → relay server (own Wi-Fi, or through its host: `meshd` over USB, phone over BLE) | mains |

Role is a runtime config value, not a different build.

## 2a. Broadcast model

**LoRa is a broadcast medium and we use it that way.** Every transmission is heard by every
node in range; there is no unicast on the air.

```
 relay A ──► GATEWAY node A ))) ((( repeater ))) ((( node B ──► phone(s) via BLE   (offline users)
 (has the packet)   broadcast        rebroadcast       │
                                                       └──► GATEWAY node B ──► relay B
                                                            (relay B can't reach relay A)
```

What a GATEWAY node does:

1. **Server → air:** holds a gateway session with its relay (server.md §4, mode `lora`) and
   receives packets selected for broadcast; transmits them.
2. **Air → server:** everything it hears on air that its relay doesn't have yet is uploaded.
   So **LoRa links relays that cannot reach each other** — e.g. two cities' regional relays
   isolated from each other, or an isolated relay and one that still has internet.
3. **Air → local:** CLIENT nodes hand received packets to their paired phone, which delivers
   them or spreads them in the Tier 0 phone mesh.

Every node (repeater, client, gateway) **rebroadcasts each new packet once** (managed flood):

- Dedup by `packet_id(+fragment)` in a seen-cache (~24 h) — this also stops loops between
  air and relays: a gateway never re-transmits a packet it has already heard on air.
- `hop_count` incremented; dropped at `max_hops` (default 3 on LoRa).
- **SNR-ordered delay:** nodes that heard the packet weakly (likely farthest away) wait less
  before rebroadcasting; a node that hears enough neighbours rebroadcast it first cancels its
  own copy. Hearing a rebroadcast is the implicit ACK.
- RECEIPTs are broadcast the same way and purge copies queued in every node that hears them.
  A node only purges on a receipt it can verify, using public keys it learned from announces
  it heard (a bounded cache); unverifiable receipts are still forwarded. Announces and
  receipts that fail verification are dropped before they enter the seen-cache, so a forged
  copy cannot shadow the real one.

### Airtime budget — the real limit

At BALANCED (SF9/125 kHz) a full 255 B frame takes ≈ 1 s on air. With a 1 % duty cycle that is
**≈ 36 full frames per hour per node**; ≈ 360/hour in a 10 % sub-band where allowed. The
whole LoRa layer therefore carries **text only, selected by priority**, never bulk sync:

1. RECEIPTs (small, free up everyone's queues)
2. DATA whose destination has no internet path (home region unreachable / not online) and
   is announced on this side of the air (cluster SUMMARY match)
3. PRIORITY-flagged DATA
4. Other DATA, oldest-first, until the budget is spent
5. ANNOUNCE / SUMMARY (rate-limited)

Packets are never broadcast if the relay already has a RECEIPT for them, if they are
`NO_LORA`, or if they were heard on air already.

## 2b. Gateway uplink — direct or through a phone, automatically

A GATEWAY is **two-way** (relay → air and air → relay) and can reach its relay over **any**
of these uplinks. It uses whatever is available, several at once, with no manual switching:

| Uplink | How | Good for |
|---|---|---|
| **Direct Wi-Fi** | ESP32 built-in Wi-Fi → relay over HTTPS/WebSocket | rooftop node near a building with internet |
| **Direct Ethernet** | W5500 SPI module | fixed installs with a cable |
| **Direct cellular** | external LTE modem over UART (SIM card; domestic data reaches regional relays during blackouts) | remote sites, no Wi-Fi |
| **Phone over BLE** | paired phone with any relay access (domestic data, VPN, Starlink…) carries the session | handheld / portable gateway, cheap boards |
| **PC over USB** | `meshd` on a Linux PC carries the session | PC with VPN / Starlink / LAN |

Design that keeps it seamless:

1. **The node owns the gateway identity.** Each node has an Ed25519 key. Every gateway
   session — direct or through a phone/PC — is authenticated by the node signing the relay's
   challenge (the phone/PC only forwards it over BLE/USB). The relay sees one gateway, not
   "a phone" or "a Wi-Fi client".
2. **Multiple uplinks at once are fine.** The relay groups connections by node key and
   pushes each packet once on whichever connection is alive; uploads are deduplicated by
   `packet_id`. If the phone walks away, the Wi-Fi/LTE link keeps going, and vice versa.
3. **The node owns the airtime budget.** All packets for broadcast land in the node's own
   priority queue, whatever uplink delivered them, so two uplinks can never double the
   on-air traffic.
4. **No uplink → still useful.** The node keeps working as a client/repeater and stores
   what it hears; when an uplink returns it uploads the backlog.
5. **Phone side is automatic:** when the app is paired with a gateway-capable node and the
   phone can reach a relay, it opens the tunnelled session in the background (user setting:
   *Share my internet with my LoRa node* — on/off + data quota).

Uplink priority for *outbound* work (uploading what was heard): wired > Wi-Fi > phone/PC >
cellular (cost). Inbound pushes arrive on all live sessions and are deduplicated by the node.

### Host link framing (BLE / USB)

Phone (BLE) and PC (USB serial) speak the same byte stream to the node, implemented in
`core/hostlink` (Go) and `firmware/lib/meshnode/src/hostlink` (C++), kept identical by the
`hostlink` section of `spec/test-vectors/vectors.json`:

```
COBS( kind(1) | len(2, BE) | payload | crc16(2, BE) ) 0x00        CRC-16/CCITT-FALSE
kind: 0 mesh packet · 1 command (host → node) · 2 event (node → host) · 3 gateway tunnel
command / event payload: op(1) | req(1) | data        (req echoed in the reply)
```

COBS removes zero bytes, so `0x00` delimits frames and a receiver resynchronises after
noise or a mid-stream start. Commands: `HELLO [unix_time]` (status reply, then the stored
backlog streams), `GET_STATUS`, `GET_CONFIG`, `SET_CONFIG` (TLVs: role, region, preset,
frequency, power, name, announce interval, 8 LoRa module: 1 fitted (default, not encoded) /
0 none, the board then skips the radio — validated against the region plan, persisted,
radio retuned live), `GET_IDENTITY`, `RESYNC` (resend everything stored), `REBOOT`,
`WIPE "WIPE"`, and on boards with Wi-Fi `SET_WIFI` (TLVs: 1 SSID 0–32 bytes, 2 password
empty or 8–63, 3 relay URL; empty SSID forgets the network) and `HOTSPOT 1|0` (open the setup
hotspot now / back to normal), and on boards with a screen UI `SCREEN 1|0` and `INPUT` (the
screen mirrored to the host, docs/screen.md; events `SCREEN`); other nodes answer
`ACK unknown op`. Events: `STATUS` (36-byte block), `CONFIG`, `IDENTITY`, `ACK`, `LOG`.

Mesh packets from the host are transmitted as is (the host's router has already handled the
hop count). Packets the node hears are stored and streamed to the host; a packet counts as
delivered only once its bytes left the node's output buffer, so a host that disconnects
mid-stream gets it again on the next `HELLO`. A node announces itself only after a host has
told it the time (boards have no RTC; relays keep the newest announce per identity).

Region plans (`core/hostlink.Regions`, firmware table checked against it): EU_868
(869.525 MHz, 10 %), EU_868_1PCT (868.1 MHz, 1 %), EU_433, US_915, AU_915, IN_865, AS_923.
They are starting points, not legal advice: **check local rules before transmitting.**

## 3. Software architecture

```
firmware/
  platformio.ini            envs: heltec_v3, tbeam_s3, rak4631, techo (pins as build flags)
  src/                      the board layer (Arduino), moves bytes only
    main.cpp                boot from LittleFS, radio / host / timer loop, bench logging
    radio                   RadioLib SX1262: TX, RX, CAD, presets, retune
    board                   RNG, LittleFS files, battery, LED, PMU rails, reboot
  lib/meshproto/            pure C++17, no heap, no Arduino: packet codec, SHA-256,
                            identity, announce/receipt (spec/test-vectors conformance)
  lib/meshroute/            managed-flood router, MAC (airtime, duty cycle, CAD), store
  lib/meshnode/             host link (COBS stream shared with core/hostlink) + node::Node
  lib/monocypher/           X25519 / Ed25519
  sim/                      meshnode-sim: real nodes on a simulated radio, ptys for meshd
  test/                     host tests (`make test`, ASan/UBSan)
```

Everything that decides anything is in `lib/` and runs on the host: `node::Node` takes
received frames, host bytes and the time, and says what to transmit, when to do CAD and what
to send to the host. `src/` is the only Arduino code: radio, USB, the BLE phone link (§6),
Wi-Fi (setup hotspot + relay worker, board.md §2, §5). Not built: Ethernet and cellular
uplinks, display, GPS, deep sleep.

## 4. Radio presets

| Preset | SF / BW / CR | ≈ bitrate | ≈ airtime 200 B | Use |
|---|---|---|---|---|
| FAST | SF7 / 250 kHz / 4/5 | 11 kbps | 0.2 s | dense local, short range |
| BALANCED | SF9 / 125 kHz / 4/5 | 1.8 kbps | 1 s | default backbone |
| LONG | SF11 / 125 kHz / 4/8 | 0.4 kbps | 5 s | hard links, last resort |

Frequency plan is configurable (433 / 868 / 915 bands). **Check local regulations before
transmitting** — band, power and duty-cycle limits vary by country; the default build ships
with the EU_868 plan (869.525 MHz, the 869.4–869.65 MHz sub-band with 10 % duty cycle)
enforced by `mac/Airtime`; EU_868_1PCT and other regions are a config setting.

## 5. MAC behaviour

- Listen-before-talk using SX1262 Channel Activity Detection, random backoff.
- All forwarding is broadcast + managed flood (§2a). Implicit ACK = hearing a neighbour
  rebroadcast; if none is heard, the originating node retransmits up to 2 times with backoff.
- Announce rate limit: own announce ≤ 1 / 30 min; forwarded announces ≤ 10 % airtime.
- SUMMARY broadcast every 15 min or on significant change.
- Priority queue: RECEIPT > DATA(PRIORITY) > DATA > SUMMARY > ANNOUNCE.

## 6. Phone link (BLE GATT)

```
Service  6d657368-6c72-0000-8000-00805f9b34fb     ("meshlr", then the Bluetooth base tail)
  RX     6d657368-6c72-0001-8000-00805f9b34fb     write w/o response   phone → node
  TX     6d657368-6c72-0002-8000-00805f9b34fb     notify               node → phone
```

The bytes are the host link stream of §2b, unchanged (COBS frames, any split across
writes/notifications). The node advertises the service and its name. The iPhone app speaks this
(`ios/Mava/LoRaBle.swift`, docs/ios.md §4: iPhones can't use a USB node); the board
implements it as host port 1 (`firmware/src/ble.cpp`), next to USB on port 0. Writing needs
a bonded link (6-digit passkey, board.md §4).

Frame: `kind(1) | len(2) | payload`. `kind`: 0 = mesh packet, 1 = command, 2 = event,
3 = uplink tunnel (gateway session bytes, §2b).
Commands: get status, set role/preset/frequency, list stored packets, reboot, wipe.
Pairing: BLE passkey (shown on display, or printed on device label for headless boards).

The node never sees plaintext: it stores and forwards ciphertext only. `wipe` erases store,
config and node identity.

## 7. Milestones

1. Two boards exchange raw packets; serial console shows decoded headers.
2. Managed flood + dedup + airtime budget across 3 boards (A → repeater → B).
3. Gateway role: relay A → air → relay B (via Wi-Fi or USB host `meshd`).
4. BLE phone link; Android app sends/receives text through nodes.
5. Low-power repeater build on RAK4631 with solar; field range test.

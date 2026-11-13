# Roadmap

| Milestone | Deliverable | Done when |
|---|---|---|
| **M0 Spec** | protocol.md v0 frozen, `spec/test-vectors/*.json`, Persian text compression benchmark | vectors pass in Go core and C++ firmware codec |
| **M1 Core + sim** | `core/` packet, identity, e2e, router, store; `sim/` drives it: phones, LoRa, regional + global relays, bridges, blackout scenario | delivery ratio / latency / airtime reported for Tehran ↔ Berlin and City A ↔ City B |
| **M2 Relay** | `server/` relay: packets, mailbox, receipts, federation `/v1/sync`, gateway sessions | two relays federate; packet uploaded to one is fetched from the other |
| **M3 Linux desktop** | `meshd` + `mesh` CLI: client, gateway, bridge roles, LAN interface | two Linux PCs chat via IR-style relay ↔ bridge ↔ EU-style relay |
| **M4 LoRa link** | firmware radio, codec, router, store, hop ACK; USB serial to meshd | text crosses PC → node → repeater → node → PC |
| **M5 Android** | gomobile core, BLE mesh, LoRa-over-BLE, gateway mode, chat UI | message: phone (BLE) → phone gateway → relay → desktop |
| **M6 Hardening** | panic wipe, APK sharing, Tor path, reproducible builds, security review | external review findings addressed |
| **M7 Field test** | 10 km LoRa test + simulated domestic-only network test | measured delivery report |
| **M8 Windows / macOS** | desktop builds + installers | |
| **M9 Direct links + calls** | direct IP link across any NAT ([connectivity.md](connectivity.md)), then 1:1 voice calls on Android ([calls.md](calls.md), stream-based so video can be added later) | call between two phones behind different NATs and on one hotspot; relay counters show zero audio bytes |
| **M10 iPhone app** | Go core as an iOS framework (`gomobile bind -target=ios`, built on a macOS CI runner); native SwiftUI app: chat over relays, Bluetooth mesh (Core Bluetooth, foreground first), push from relays through APNs for messages and calls (PushKit + CallKit), LoRa through a node's Bluetooth host link (firmware.md §6; iPhones can't use USB LoRa nodes), TestFlight distribution | iPhone ↔ Android message over Bluetooth and over a relay; iPhone ↔ Android call; incoming message and call arrive while the app is in the background |

iOS limits that shape M10: no always-running background service, so an iPhone is an endpoint,
not a relay (android.md §1); background Bluetooth advertising is hard for Android phones to
see; delivery while closed depends on Apple's push service, which learns that a message arrived
(not its content). The Go core already cross-compiles for `GOOS=ios` without changes.
Distribution needs an Apple Developer account, which is not sold in some countries.

Order rationale: the Go core and relay come first because desktop, Android and the
simulator all reuse them, and Linux desktop + relay is the fastest way to a working
end-to-end system with no special hardware.

## Code changes required by the 2026-11-12 design decisions (steps 1–4 built)

The app is **alpha, tested in the lab only**; no beta or original release exists yet. Until
the first beta there is **no backward compatibility with alpha builds**: formats, APIs and
relay rules may change at once, old alpha apps and relays are simply replaced, and no
transition period for them is built. (The forward-compatibility rules of calls.md — ignore
unknown ops, streams, codecs — are still built in, for versions after the beta.)

Existing code that must change:

| # | Where | Today | Must become |
|---|---|---|---|
| 1 | `core/node/media.go` `SendMedia` (+ Android, web UI, CLI) | every file is uploaded to relays | normal media: no upload (`n = 0` URLs), sender keeps and serves it over direct paths; important media: user marks it, uploaded to relays (protocol.md §6b) |
| 2 | `server/api/media.go`, `server/cmd/relay`, `server/admin` | files kept ≤ 8 h (`media_max_hours`, panel cap 72 h) | important files kept 7 days, never evicted once accepted (server.md §6a) |
| 3 | same | account signature only with `media_accounts`; fixed PoW bits | signature always required; daily allowance per account set in the panel; PoW by account age + uploads today; first-announce time per account; `GET /v1/media/quota`; usage ledger `POST/GET /v1/usage`, also in `/v1/sync` (server.md §6a) |
| 4 | `core/node/lan.go` | UDP multicast discovery + TCP, one subnet | stays, and becomes one candidate source of the new direct-link module (connectivity.md) |
| 5 | new `core/direct`, relay UDP reflector, TURN in `meshmsg-relay` | none | connectivity.md §3–§7, server.md §6c |
| 6 | new `core/call`, Android audio | none (content type 7 reserved) | calls.md |
| 7 | `README.fa.md`, app strings | describe the old media rule | updated (README.fa.md done with these docs) |
| 8 | relay `eu-1` server | TCP only | a UDP port for the reflector; TURN ports if its admins turn TURN on |

Done: (1), (2), (3) (signatures, allowance, quota, account age, proof-of-work, usage
ledger), (7) — v0.1.0-alpha10. (5) direct links (core/direct, relay reflector and TURN for
admins, node `direct` link, normal media over it) and (6) voice calls (core/call, node and
mobile API, Android audio, call screen and service) are built; tested in an in-memory NAT
simulator and on loopback, not yet between real phones on real networks. (4) stays as it
is (LAN discovery still runs beside core/direct). (8) waits for the next eu-1 update.
Relay ports fixed for all relays (server.md §6d).

Order: (1)+(2)+per-relay allowance → (3) age, PoW, ledger → (5) direct links with netns NAT
tests, then TURN for admins → (6) calls (core first; audio needs phones). Each step ships as a
new tag.

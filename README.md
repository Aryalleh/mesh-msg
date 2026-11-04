# mesh-msg

An off-grid, censorship-resistant messenger built as a **mesh of meshes**.

Messages travel over whatever path exists:

| Tier | Transport | Range | Payload |
|---|---|---|---|
| 0 — Local | BLE, Wi-Fi Direct, Wi-Fi Aware (phone ↔ phone) | 10–100 m per hop | Text + small media |
| 1 — Backbone | LoRa nodes (ESP32 + SX1262), paired to phones via BLE | 2–15 km per hop | Text only (~200 B packets) |
| 2 — WAN | Any phone with internet (VPN/Tor) → relay servers | Global | Anything |

```
   City A                          ~10 km                         City B
 ┌──────────────────┐                                    ┌──────────────────┐
 │ 📱──BLE──📱──📱   │      LoRa backbone (rooftops)      │   📱──📱──BLE──📱 │
 │      📱──BLE──📡──┼────────────────────────────────────┼──📡──BLE──📱      │
 │  (has VPN) 📱     │                                    │     📱 (online)   │
 └───────┼──────────┘                                    └───────┼──────────┘
         └──────────── Internet relay servers (Tier 2) ──────────┘
```

## Core principles

- **Delay-tolerant / store-and-forward** — nodes hold encrypted messages until a better route appears.
- **Identity = public key** — no phone numbers, no accounts.
- **End-to-end encryption above every transport** — relays, LoRa nodes and servers carry only ciphertext. No custom crypto.
- **Transport-agnostic routing** — each tier is just an interface; pick the cheapest one that works.
- **Survives server loss** — Tier 2 is optional; Tiers 0 and 1 work without any internet.

## Threat model highlights

Designed with internet shutdowns (e.g. Iran) in mind: radio direction-finding, device seizure,
blocked servers, no app-store access. See [docs/threat-model.md](docs/threat-model.md).

## Design docs

- [Architecture](docs/architecture.md) — tiers, components, message path
- [Protocol](docs/protocol.md) — identity, packet format, encryption, routing
- [Firmware](docs/firmware.md) — LoRa node hardware framework
- [Android](docs/android.md) — app, phone-to-phone mesh
- [Server](docs/server.md) — relay / gateway
- [Threat model](docs/threat-model.md)
- [Decisions](docs/decisions.md) · [Roadmap](docs/roadmap.md)

## Status

Design phase (M0).

## Layout

```
docs/        design docs
spec/        protocol test vectors
sim/         Python simulator of the three-tier network
firmware/    LoRa node firmware (C++ / PlatformIO)
android/     Android app (Kotlin)
server/      relay server (Go)
```

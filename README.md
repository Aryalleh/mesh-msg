# mesh-msg

English | [فارسی](README.fa.md)

An off-grid, censorship-resistant messenger built as a **mesh of meshes**.

Messages travel over whatever path exists:

| Tier | Transport | Range | Payload |
|---|---|---|---|
| 0 — Local | BLE, Wi-Fi Direct, Wi-Fi Aware (phone ↔ phone) | 10–100 m per hop | Text + small media |
| 1 — LoRa | LoRa nodes **broadcast**: gateway nodes take packets from a relay and broadcast them to every node in range — offline users, or other relays with no connection to the origin | 2–15 km per hop | Text only (~200 B packets) |
| 2a — Regional | Phones/PCs on a country's domestic network → in-region relay | One country, even during an international blackout | Anything |
| 2b — Global | Federated relays (EU, US, Asia, AU); regions joined by volunteer **bridges** (VPN, Starlink, unfiltered SIM) | Global | Anything |

Any phone or PC that can reach a relay can register as a **gateway** ("I'm online — send me
traffic for the mesh around me"). Clients: Android, and desktop (Linux first, then Windows
and macOS).

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
- [Core](docs/core.md) — shared Go core (protocol, crypto, router)
- [Server](docs/server.md) — regional & global relays, gateways, bridges
- [Desktop](docs/desktop.md) — `meshd` client / gateway / bridge (Linux first)
- [Android](docs/android.md) — app, phone-to-phone mesh
- [Firmware](docs/firmware.md) — LoRa node hardware framework
- [Threat model](docs/threat-model.md)
- [Decisions](docs/decisions.md) · [Roadmap](docs/roadmap.md)

## Status

M0–M3 implemented: Go core, simulator, relay and Linux desktop (`meshd` + `mesh`),
with language-neutral test vectors. M4 (LoRa firmware): the whole node
(protocol, router, MAC, store, host link) is board-independent and host-tested against the same
vectors (`make -C firmware test`); `scripts/e2e-lora.sh` sends a text PC → node → repeater →
node → PC with real firmware nodes on a simulated radio; PlatformIO builds for Heltec V3,
T-Beam S3, RAK4631 and T-Echo exist but are not yet verified on hardware. M5 (Android): the core is bound with gomobile
(`mobile/`), and `android/` has the app — Compose UI in Persian and English, BLE mesh
transport, USB LoRa link, foreground service — built and tested on the JVM, not yet on phones.
See [roadmap](docs/roadmap.md).

## Layout

```
docs/        design docs
spec/        protocol test vectors
core/        shared Go core: protocol, crypto, router, store, relay client
server/      relay server (Go)
desktop/     meshd daemon + CLI + web UI (Go; Linux first)
android/     Android app (Kotlin + Go core via gomobile)
firmware/    LoRa node firmware (C++ / PlatformIO)
sim/         network simulator (Go, uses core/)
```

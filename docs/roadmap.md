# Roadmap

| Milestone | Deliverable | Done when |
|---|---|---|
| **M0 Spec** | protocol.md v0 frozen, `spec/test-vectors/*.json`, Persian text compression benchmark | vectors pass in Go core and C++ firmware codec |
| **M1 Core + sim** | `core/` packet, identity, e2e, router, store; `sim/` drives it: phones, LoRa, regional + global relays, bridges, blackout scenario | delivery ratio / latency / airtime reported for Tehran ↔ Berlin and City A ↔ City B |
| **M2 Relay** | `server/` relay: packets, mailbox, receipts, federation `/v1/sync`, gateway sessions | two relays federate; packet uploaded to one is fetched from the other |
| **M3 Linux desktop** | `meshd` + `mesh` CLI: client, gateway, bridge roles, LAN interface | two Linux PCs chat via IR-style relay ↔ bridge ↔ EU-style relay |
| **M4 LoRa link** | firmware radio, codec, router, store, hop ACK; USB serial to meshd | text crosses PC → node → repeater → node → PC |
| **M5 Android** | gomobile core, BLE mesh, LoRa-over-BLE, gateway mode, chat UI | message: phone (BLE) → phone gateway → relay → desktop |
| **M6 Hardening** | panic wipe, stealth, APK sharing, Tor path, reproducible builds, security review | external review findings addressed |
| **M7 Field test** | 10 km LoRa test + simulated domestic-only network test | measured delivery report |
| **M8 Windows / macOS** | desktop builds + installers | |

Order rationale: the Go core and relay come first because desktop, Android and the
simulator all reuse them, and Linux desktop + relay is the fastest way to a working
end-to-end system with no special hardware.

# System Architecture

## 1. Goal

Deliver short, end-to-end-encrypted messages between people **with no working internet**,
using whatever links exist: phones near each other, long-range LoRa radios, a country's
domestic network during an international blackout, and — when anyone anywhere has a way
out (VPN, Starlink, unfiltered SIM) — the global internet. Each layer works alone; together
they form one network: a mesh of meshes.

## 2. Layers

```
                       Tier 2b: Global relay federation
       ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
       │ EU relay │◄──►│ US relay │◄──►│Asia relay│◄──►│ AU relay │
       └────▲─────┘    └──────────┘    └──────────┘    └────▲─────┘
            │ international internet (cut during blackout)   │
   ┌────────┴──────────────────┐                     📱💻 users abroad
   │ BRIDGES                   │  devices with a way out: VPN, Starlink,
   │ desktop daemon / phone in │  unfiltered SIM. They tunnel relay sync
   │ bridge mode               │  across the cut, in both directions.
   └────────▲──────────────────┘
            │ domestic network (usually still up during blackout)
   ┌────────┴──────────────────────────┐
   │ Tier 2a: Regional relays (e.g. IR) │  several, small, disposable
   └───▲──────────────▲────────────▲───┘
       │ GATEWAYS: any phone / PC that can reach a relay registers
       │ "I'm online, send me traffic for the mesh around me"
      📱gw            💻gw          📱gw
       │ BLE           │ USB         │ BLE
  ┌────┴──────┐   ┌────┴───┐   ┌─────┴─────┐
  │ Tier 0    │   │ 📡 LoRa │~~~│ 📡 LoRa    │  Tier 1 backbone
  │ phone mesh│   │  node   │   │  node     │  (city ↔ city)
  └───────────┘   └────────┘   └───────────┘
```

| Layer | Links | Reach | Who runs it |
|---|---|---|---|
| Tier 0 local | BLE, Wi-Fi Direct, LAN | 10–100 m / one LAN | every phone & PC with the app |
| Tier 1 backbone | LoRa (SX1262) | 1–15 km per hop | client, repeater, gateway nodes |
| Tier 2a regional | HTTPS/WebSocket to an in-region relay | one country's domestic network | volunteers inside the region |
| Tier 2b global | HTTPS/WebSocket, CDN-fronted, Tor | worldwide | volunteers anywhere |

## 3. Roles (any device can hold several)

| Role | What it does | Runs on |
|---|---|---|
| **Client** | owns an identity, sends/receives messages | Android, Linux/Windows/macOS |
| **Gateway** | has relay access; registers with a relay and moves packets between relay and its local mesh (Tier 0 / Tier 1) | Android, desktop, LoRa gateway node |
| **Bridge** | has access to *two* relays that can't reach each other (e.g. IR regional + EU global); syncs them | desktop daemon (primary), Android (bridge mode) |
| **Relay** | server: stores and forwards ciphertext, federates with peer relays | Linux server |
| **LoRa repeater** | forwards LoRa only | LoRa node |

## 4. Components

| Component | Dir | Tech | Doc |
|---|---|---|---|
| Wire protocol + test vectors | `spec/` | language-neutral spec, JSON vectors | [protocol.md](protocol.md) |
| Shared core | `core/` | Go: codec, Noise crypto, router, store, relay client | [core.md](core.md) |
| Relay server | `server/` | Go, single static binary | [server.md](server.md) |
| Desktop client / gateway / bridge | `desktop/` | Go daemon `meshd` + CLI + local web UI; **Linux first** | [desktop.md](desktop.md) |
| Android app | `android/` | Kotlin UI + transports, `core/` via gomobile | [android.md](android.md) |
| LoRa firmware | `firmware/` | C++17, PlatformIO, RadioLib | [firmware.md](firmware.md) |
| Simulator | `sim/` | Go, drives the real `core/` router | [roadmap.md](roadmap.md) |

One Go core is shared by server, desktop, Android and the simulator. Firmware is the only
second implementation (outer header, announces, receipts — no E2E crypto), kept compatible
by `spec/test-vectors/`.

## 5. Core design decisions

1. **One packet format everywhere.** A packet crosses BLE → LoRa → regional relay → bridge →
   global relay → phone unchanged. Every link is just an *interface* to the router.
2. **Identity = keypair.** Address = 16-byte hash of public keys. No accounts, no phone numbers.
3. **End-to-end Noise encryption.** Gateways, bridges and relays see only destination hash,
   packet ID, hop count, size.
4. **Sender hidden** inside the ciphertext.
5. **Store-and-forward everywhere**, purged by signed receipts or expiry.
6. **LoRa-first sizing** for text; `NO_LORA` flag for anything bigger.
7. **Every server and bridge is untrusted and replaceable.** Losing all relays leaves Tiers 0
   and 1; losing all bridges leaves each region working internally.

## 5a. LoRa = broadcast layer

LoRa modules don't route point-to-point; they **broadcast**. A LoRa gateway node takes
packets from its relay and broadcasts them; every node in range hears them and rebroadcasts
once (hop-limited). On the far side:

- **client nodes** hand them to paired phones of offline users, and
- **gateway nodes** upload them to *their* relay — which may be a relay with no connection to
  the origin relay. So LoRa also joins otherwise isolated relays and regions.

Airtime is tiny (≈ tens of text packets per hour per node), so relays only put on the air what
has no other path. Details: firmware.md §2a.

## 5b. Direct IP links across any topology

Devices may sit behind CGNAT, mobile networks, hotspots and nested NATs. A direct link is
chosen only by tested reachability, never by whether an address looks private or public;
the most direct working path wins (traffic stays on a shared local network), relays only
pass encrypted candidates and never carry a direct session, and there is no TURN fallback.
Requirement and design: [connectivity.md](connectivity.md).

## 6. Message paths (example)

Ali is in Tehran during an international blackout, offline except BLE.
Sara is in Berlin with normal internet.

1. Ali's app encrypts for Sara, packet `dest = H(Sara)`, spreads it in the BLE mesh.
2. Reza's phone nearby has domestic mobile data → it is a **gateway** registered with the IR
   regional relay; it uploads the packet.
3. A **bridge** (a PC in Tehran with Starlink, running `meshd --bridge`) syncs the IR relay
   with the EU relay; the packet crosses.
4. The EU relay federates it to all global relays; Sara's client, connected to the US relay,
   receives it via live push.
5. Sara's signed receipt travels back the same way; every holder purges its copy.

Reverse direction: Sara's reply reaches the IR relay through the bridge. The relay knows
Ali's **home region** (his announces arrive via IR gateways) and pushes the packet to IR
gateways that registered "Ali is near me" — or, if none, to **carrier** gateways that
accept a quota of undelivered traffic to spray into their local mesh.

## 7. Relationship to existing projects

Ideas borrowed from Reticulum/LXMF (hash addresses, announces, reverse-path proofs,
propagation nodes), Meshtastic (cheap LoRa hardware, BLE pairing), Briar (Bluetooth/Wi-Fi/Tor)
and Psiphon/Tor bridges (volunteer-run paths across censorship boundaries). See
[decisions.md](decisions.md).

# System Architecture

## 1. Goal

Deliver short, end-to-end-encrypted messages between people **with no working internet**,
using whatever links exist: phones near each other, long-range LoRa radios, and — when anyone
anywhere has connectivity — internet relays. The system degrades gracefully: each tier works
alone; together they form one network.

## 2. The three tiers

```
            ┌──────────────────────── Tier 2: WAN ─────────────────────────┐
            │      Relay servers (federated, untrusted, store-and-forward) │
            └──────▲──────────────────────────────────────────────▲────────┘
                   │ HTTPS / CDN-fronted / Tor                   │
   City A          │                                             │          City B
 ┌─────────────────┼───────────┐                     ┌───────────┼─────────────────┐
 │  📱 online ─────┘           │                     │           └───── 📱 online   │
 │   │  Tier 0: BLE/Wi-Fi mesh │                     │ Tier 0: BLE/Wi-Fi mesh │     │
 │  📱 ── 📱 ── 📱 ── 📱       │                     │      📱 ── 📱 ── 📱 ── 📱     │
 │               │ BLE         │                     │         BLE │               │
 │              📡 client node │   Tier 1: LoRa      │  client node 📡              │
 └──────────────┼──────────────┘     backbone        └─────────────┼────────────────┘
                └───── 📡 rooftop ~~~~~ 📡 repeater ~~~~~ 📡 gateway ┘
                                                          │ Wi-Fi (optional)
                                                          └──► Tier 2
```

| Tier | Links | Typical hop | Payload | Who runs it |
|---|---|---|---|---|
| 0 Local | BLE, Wi-Fi Direct, Wi-Fi Aware | 10–100 m | text + small media | every phone with the app |
| 1 Backbone | LoRa (SX1262, 433/868 MHz) | 1–15 km | text only, ≤255 B frames | client, repeater, gateway nodes |
| 2 WAN | HTTPS/WebSocket via direct, fronted, or Tor paths | global | anything | relay servers + any online phone |

## 3. Components

| Component | Dir | Tech | Doc |
|---|---|---|---|
| Wire protocol + test vectors | `spec/` | language-neutral spec, JSON vectors | [protocol.md](protocol.md) |
| LoRa node firmware ("hardware framework") | `firmware/` | C++17, PlatformIO, Arduino, RadioLib | [firmware.md](firmware.md) |
| Android app | `android/` | Kotlin, Jetpack Compose, foreground service | [android.md](android.md) |
| Relay server | `server/` | Go, single static binary, SQLite/Postgres | [server.md](server.md) |
| Network simulator | `sim/` | Python | [roadmap.md](roadmap.md) |

Three independent implementations (C++, Kotlin, Go) of the **outer** packet format are
kept compatible by shared test vectors in `spec/test-vectors/`. Only Android implements the
full end-to-end crypto; nodes and servers only handle the outer header, announces and receipts.

## 4. Core design decisions

1. **One packet format across all tiers.** A packet created on a phone can traverse
   BLE → LoRa → internet → BLE unchanged. Tiers are just *interfaces* to the router.
2. **Identity = keypair.** Address = 16-byte hash of public keys. No phone numbers, no accounts,
   no server-side registration.
3. **End-to-end encryption with Noise** (standard patterns, no custom crypto). Relays, nodes and
   servers see only: destination hash, packet ID, hop count, size.
4. **Sender is hidden from the network.** The sender's identity is inside the ciphertext only.
5. **Delay-tolerant store-and-forward.** Every phone, node and server holds packets until they
   are delivered (receipt seen) or expire.
6. **LoRa-first sizing.** Text messages are always encoded to fit LoRa frames. Packets that
   can't (media) are flagged `NO_LORA` and never offered to the backbone.
7. **Untrusted, replaceable servers.** Losing every server removes Tier 2 only; Tier 0 + 1
   keep working. Anyone can run a relay.

## 5. Message path (example)

Ali (City A, offline) → Sara (City B, offline), 10 km apart:

1. Ali's app encrypts for Sara's key, builds a packet `dest = H(Sara)`.
2. Router has no direct route → spreads it in the local BLE mesh (epidemic, TTL-limited).
3. A phone paired with a LoRa client node receives it; the node's routing table says
   `H(Sara)` is in the cluster behind the City B gateway (learned from cluster summaries).
4. Backbone forwards hop-by-hop with link-level ACKs: client → rooftop → City B node.
5. City B node hands it to its paired phone, which spreads it in City B's BLE mesh to Sara.
6. Sara's app decrypts and emits a signed **receipt**; it travels the reverse path and lets every
   holder purge its copy.
7. In parallel, if any phone in City A got internet (VPN), it uploaded the packet to a relay;
   any online phone in City B fetching for nearby identities brings it down. Whichever arrives
   first wins; duplicates are dropped by packet ID.

## 6. Relationship to existing projects

Ideas are borrowed from Reticulum/LXMF (hash addresses, announces, reverse-path proofs,
propagation nodes), Meshtastic (cheap LoRa hardware, BLE phone pairing) and Briar
(Bluetooth/Wi-Fi/Tor transports). We build our own stack because Tier 0 phone-to-phone
meshing and shutdown-oriented UX are the core of this project; see
[decisions.md](decisions.md).

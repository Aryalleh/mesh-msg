# Key Decisions

| # | Decision | Alternatives considered | Why |
|---|---|---|---|
| D1 | Own protocol, ideas borrowed from Reticulum/LXMF | Build directly on Reticulum; Meshtastic firmware | Tier 0 phone mesh, regional relays and shutdown UX are core; we need a compact LoRa format. Revisit if Reticulum interop becomes valuable. |
| D2 | Single packet format across all tiers | Per-tier formats with translation | No component must decrypt/re-encode; fewer trust points. |
| D3 | Noise K / X for E2E | Custom ECIES; Signal Double Ratchet | Standard, analysed; 48 B overhead fits LoRa; ratchet headers too big for 255 B frames. |
| D4 | Firmware in C++/PlatformIO + RadioLib | Rust embedded; fork Meshtastic | Widest board support (ESP32 + nRF52), mature SX1262 driver. |
| D5 | Android: Kotlin UI + transports, Go core via gomobile; no Play Services | Pure Kotlin core; Flutter/React Native; Google Nearby | One core shared with server/desktop/sim (supersedes the earlier pure-Kotlin core); Kotlin still owns BLE/Wi-Fi Direct/foreground service; must work on sideloaded, de-Googled phones. |
| D6 | Go for core, server and desktop | Kotlin/Ktor; Rust | Single static binaries, trivial cross-compile to Linux/Windows/macOS, gomobile for Android, strong censorship-circumvention ecosystem. Leaves only firmware as a second implementation. |
| D7 | Monorepo | Separate repos | Spec, vectors and implementations change together. |
| D8 | Regional relays + global federation joined by volunteer bridges | Only foreign relays reached via VPN | A country's domestic network often survives an international blackout; in-region relays connect everyone on it, and only a few bridges need a way out. |
| D9 | Relay federation = full replication (v1) | DHT / selective routing | Small text volume; maximises survival when relays are blocked or seized. Selective sync in v2. |
| D11 | LoRa is broadcast-only (managed flood, dedup, SNR-ordered rebroadcast); LoRa gateways move packets relay ↔ air | Next-hop unicast routing with hop ACKs | Matches the medium (every transmission is heard by all in range), no route state to go stale, and lets LoRa bridge relays that can't reach each other. Cost: airtime, handled by strict priority selection. |
| D10 | Desktop: Linux first, daemon + CLI + localhost web UI | Native GUI per OS | Linux is easiest to run as a service for gateway/bridge operators; web UI covers all three OSes without a GUI toolkit. |

# Key Decisions

| # | Decision | Alternatives considered | Why |
|---|---|---|---|
| D1 | Own protocol, ideas borrowed from Reticulum/LXMF | Build directly on Reticulum; Meshtastic firmware | Tier 0 phone mesh + shutdown UX are core; we need a compact LoRa format and Android-native router. Revisit if Reticulum interop becomes valuable. |
| D2 | Single packet format across all tiers | Per-tier formats with translation | No gateways that must decrypt/re-encode; simpler, fewer trust points. |
| D3 | Noise K / X for E2E | Custom ECIES; Signal Double Ratchet | Standard, analysed patterns; low overhead (48 B) fits LoRa; ratchet headers too big for 255 B frames. |
| D4 | Firmware in C++/PlatformIO + RadioLib | Rust embedded; fork Meshtastic | Widest board support (ESP32 + nRF52), mature SX1262 driver. |
| D5 | Android in Kotlin, no Play Services | Flutter / React Native; Google Nearby Connections | Needs deep BLE / Wi-Fi Direct / foreground-service control; must work on sideloaded and de-Googled phones. |
| D6 | Relay server in Go | Kotlin/Ktor (share code with Android); Rust | Single static binary, easy to deploy anywhere, strong ecosystem for censorship-circumvention tooling. Outer format is small enough to implement 3×, kept in sync by test vectors. |
| D7 | Monorepo | Separate repos per component | Spec, vectors and implementations change together. |

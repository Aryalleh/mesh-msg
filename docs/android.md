# Android App

## 1. Scope

The phone is the main router of the system: it owns the user's identity, does all end-to-end
crypto, meshes with nearby phones (Tier 0), drives a paired LoRa node (Tier 1) and talks to
relay servers when the internet is reachable (Tier 2).

- Kotlin, Jetpack Compose, minSdk 26 (Android 8), targetSdk latest.
- **No Google Play Services dependency** (works on de-Googled phones and sideloaded installs).
  Google Nearby Connections is therefore not used; we implement BLE / Wi-Fi Direct ourselves.
- iOS is out of scope for v1 (background BLE restrictions make iPhones poor relays).

## 2. Module layout

```
android/
  core/                 AAR built from Go `core/` via gomobile: codec, Noise, router, store,
                        relay client (client / gateway / bridge sessions). See core.md.
  data/                 Room + SQLCipher: contacts, messages, UI state
  transport-ble/        phone↔phone BLE mesh (GATT server + client)
  transport-wifi/       Wi-Fi Direct (+ Wi-Fi Aware where supported) for bulk transfer
  transport-lora/       link to LoRa node over BLE GATT or USB-OTG serial
  app/                  Compose UI, MeshService, onboarding, settings
```

## 3. Transport abstraction

Android-only links (BLE, Wi-Fi Direct, LoRa-over-BLE) are written in Kotlin and registered
with the Go router through the core's callback API (core.md "Interface contract"). The relay
interface lives inside the Go core, identical to desktop.

## 4. Tier 0: phone-to-phone mesh

- **Discovery:** BLE advertising of a service UUID + scanning (low-duty when idle).
- **Connection:** GATT; each phone acts as both server and client. Keep ≤ 4 concurrent links.
- **Sync (epidemic, summary-vector):** on connect, peers swap inventories
  (`packet_id` lists, compact), then each pulls packets it lacks. Receipts are exchanged first
  so purged packets aren't transferred.
- **Bulk:** if both sides support it and the payload is large (media, APK), upgrade to Wi-Fi
  Direct for the transfer.
- **Store quotas:** default 50 MB relay store; evict by (expired → most hops → oldest).
  Own outgoing messages are never evicted before expiry.

## 5. Tier 1: LoRa node link

- Pair over BLE (passkey) or plug in via USB-OTG; same framed protocol (firmware.md §6).
- Phone pushes LoRa-eligible packets (no `NO_LORA` flag) to the node and receives everything
  the node receives for local destinations or for spreading into Tier 0.
- Node configuration UI: role, preset, frequency, status, wipe.
- **Share my internet with my LoRa node:** if the paired node is a gateway and the phone can
  reach a relay, the app automatically tunnels the node's gateway session over BLE
  (firmware.md §2b), within a user-set data quota.

## 6. Tier 2: relays (gateway & bridge)

- Whenever a relay is reachable (domestic network, VPN, Starlink, unfiltered SIM), the phone
  connects and — if the user enabled it — **registers as a gateway** (server.md §4): it serves
  nearby identities (delegations + local summary), receives packets for them, and uploads
  its store. Modes `targeted` / `carrier`, with a data quota.
- **Bridge mode** (advanced): a phone that can reach two relays that can't reach each other
  (e.g. IR regional directly + EU through a VPN app) syncs them, within a data budget.
- Path fallback order configurable: direct HTTPS → CDN-fronted → Tor (via Orbot or embedded).
- Relay list ships with the app and is updated by **signed relay-list announces** that spread
  over the mesh itself, so new relay addresses reach people who are offline.

## 7. Background operation

- `MeshService` = foreground service (`connectedDevice` type) with a persistent notification;
  user is prompted to exempt the app from battery optimisation.
- Android 12+: `BLUETOOTH_SCAN` (with `neverForLocation`), `BLUETOOTH_CONNECT`,
  `BLUETOOTH_ADVERTISE`; Android 13+: `NEARBY_WIFI_DEVICES`.
- Battery modes: *Active* (full meshing), *Saver* (scan bursts every N minutes), *Off*.

## 8. Security & safety UX

- Database encrypted with SQLCipher; key in Android Keystore, optional app PIN.
- **Panic wipe**: PIN-duress code or long-press gesture erases keys + DB.
- **Stealth mode**: no BLE advertising (only connects to known peers / paired node).
- Optional disguised launcher icon/name.
- **Offline app sharing**: send the APK to a nearby phone over Wi-Fi Direct/Bluetooth,
  with the signing certificate fingerprint shown for verification.
- Contact verification via QR code and safety-number comparison.

## 9. Screens (v1)

Onboarding (create identity, permissions) · Chats list · Chat · Add contact (QR / link) ·
Network status (neighbours, LoRa node, internet, store size) · LoRa node settings · Settings
(battery mode, stealth, panic wipe, relays).

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
  core-protocol/        pure Kotlin/JVM: packet codec, Noise K/X, announces, receipts,
                        Bloom filter, router logic. No Android deps → unit tested on JVM.
  core-data/            Room + SQLCipher: identities, contacts, messages, packet store, routes
  transport-api/        Interface abstraction (see §3)
  transport-ble/        phone↔phone BLE mesh (GATT server + client)
  transport-wifi/       Wi-Fi Direct (+ Wi-Fi Aware where supported) for bulk transfer
  transport-lora/       link to LoRa node over BLE GATT or USB-OTG serial
  transport-internet/   relay client: HTTPS/WebSocket, fallback paths (fronted, Tor)
  app/                  Compose UI, MeshService, onboarding, settings
```

## 3. Transport abstraction

```kotlin
interface MeshInterface {
    val id: String
    val kind: Kind              // BLE, WIFI_DIRECT, LORA, INTERNET
    val maxPacketSize: Int      // LoRa = 255
    val state: StateFlow<State>
    val neighbours: StateFlow<Set<PeerId>>
    suspend fun send(peer: PeerId?, packet: ByteArray)   // null = broadcast
    val incoming: Flow<Received>
}
```

The `Router` (in core-protocol) knows nothing about Android; it consumes `MeshInterface`s and
applies the rules in protocol.md §6. This lets the same router run in `sim/`-style JVM tests.

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

## 6. Tier 2: internet

- Opportunistic: when connectivity is detected, sync with relay(s): upload store, fetch
  mailboxes for own identity and for nearby identities that set `gateway_delegation`.
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

# iPhone app

Status: **first version written; it has never run on an iPhone or a simulator.** The CI job
`iOS` (`.github/workflows/ios.yml`, macOS runner) only proves that it compiles.

## 1. Shape

```
ios/
  project.yml            Xcode project definition (XcodeGen)
  Mava/                  SwiftUI app
    Core.swift           the Go core behind Swift types (every gomobile call)
    AppModel.swift       state: core, radios, chats, call
    Views.swift          onboarding, chats, chat, add contact (link + QR), network, settings, call
    BleMesh.swift        Bluetooth mesh with phones (Core Bluetooth, central + peripheral)
    LoRaBle.swift        LoRa node over Bluetooth (host link as a byte stream)
    CallAudio.swift      call audio: AVAudioEngine + Opus
scripts/build-ios-core.sh   mobile/ → ios/Frameworks/Meshcore.xcframework (gomobile)
```

The app uses the same Go core as Android (`mobile/`, package `meshcore`), so protocol,
encryption, relays, direct links and call logic are the same code. Build on a Mac:
`scripts/build-ios-core.sh && cd ios && xcodegen && open Mava.xcodeproj`.

## 2. What is in the first version

| Feature | State |
|---|---|
| Account, contacts by link or QR shown on screen, chats, text messages, receipts | written |
| Relays (default `eu-1`, add/remove, automatic discovery) | written (core) |
| Direct links across NAT (normal media, calls) | core; iOS needs no socket binding |
| Voice calls: start, ring, answer, mute, speaker, code to compare | written; Opus only |
| Bluetooth mesh with Android phones and iPhones | written (§3) |
| LoRa through a node's Bluetooth host link | iPhone side written (§4); **the firmware side does not exist yet** |

Not yet: sending and showing photos/voice messages/files (received ones show as a file
row), groups management, QR scanning, backup/restore, profile pictures, folders, video
calls, CallKit (calls ring only while the app is open), push notifications, Persian
strings, the update check.

## 3. Bluetooth mesh on iOS

Same GATT service and framing as Android (android.md §4). Two differences forced by iOS:

- iOS can't put manufacturer data in an advertisement, which is where Android carries the
  4-byte session ID. An iPhone advertises the session ID as the **local name** (8 hex
  digits) next to the service UUID.
- An Android phone only connects to adverts with manufacturer data, so between an iPhone
  and an Android phone **the iPhone always connects** (as GATT client). Between two iPhones
  the larger session ID connects, as between Androids.

In the background iOS keeps existing connections but advertises and scans much less, and
it stops the app after a while: an iPhone is an endpoint, not a relay or gateway
(gateway mode is off in its core config).

## 4. LoRa on an iPhone

An iPhone can't use a LoRa node on a cable (no USB serial access for apps), so the node
must offer the host link over Bluetooth: boards with BLE such as Heltec V3, T-Beam,
T-Deck, T-Echo, RAK4631. The host link bytes are the same as over USB (firmware.md §2b):

```
Service  6d657368-6c72-0000-8000-00805f9b34fb
  RX     6d657368-6c72-0001-…   write without response   phone → node
  TX     6d657368-6c72-0002-…   notify                   node → phone
```

The app scans for the service, the user picks the node once (Settings → LoRa node), and
the core attaches it like a USB node (`AttachLoRa`). The firmware does not implement this
service yet (firmware.md §7, milestone 4); until it does, this path can't work. Android
will use the same service for nodes without a cable.

## 5. Calls

Audio is 16 kHz Opus in 20 ms frames, the same stream as Android (calls.md), captured with
the system's voice processing (echo cancellation). An iPhone offers only Opus; an Android
phone before Android 10 offers only AMR-WB, so those two can't call each other yet.

Calls arrive only while the app is open. Ringing when the app is closed needs Apple's
VoIP push (PushKit + CallKit) and a relay that sends pushes (§6).

## 6. What iOS does not allow, and the plan

| Limit | Consequence | Plan |
|---|---|---|
| No always-running background service | messages and calls arrive only while the app is open | relays send a push through Apple (APNs / PushKit) that says "something arrived", never content; Apple learns the time of arrival |
| No sideloading | distribution through TestFlight / App Store only (EU alternative stores aside); needs an Apple Developer account ($99/year), not sold in some countries | owner's account; TestFlight first |
| No USB serial | LoRa nodes over Bluetooth only | §4 |
| Background Bluetooth is throttled | an iPhone meshes well only while the app is open | accept |

## 7. Next steps

1. Run it: simulator first (relay chat), then two phones (Bluetooth, calls). Needs a Mac.
2. Firmware Bluetooth host link (ESP32 NimBLE, nRF52 SoftDevice) + pairing passkey.
3. Media (photos, voice messages), groups, QR scanner, backup, Persian strings.
4. CallKit + PushKit, relay push support, TestFlight.

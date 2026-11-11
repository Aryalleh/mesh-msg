# mesh-msg for Android

Design: [docs/android.md](../docs/android.md) · UI design system: [DESIGN.md](DESIGN.md).

```
app/src/main/java/org/meshmsg/app/
  MeshApp            owns the Go core and the radios; restarts them when settings change
  MainActivity       navigation: Chats · Network · Settings, chat, add contact; meshmsg: links
  data/              models + JSON from the core, settings, MeshBackend (the UI's only dependency)
  ui/                Compose screens (stateless: data in, callbacks out) and the theme
  ble/               BleProto (pure logic, unit-tested) + BleMesh (GATT server and client)
  lora/UsbLoRa       LoRa node on a USB-OTG cable (CDC-ACM) → core host link
  service/           foreground service that keeps the mesh alive
```

The identity, crypto, routing, stores and relay / gateway sessions are the shared Go core,
bound with gomobile ([`mobile/`](../mobile/meshcore.go), Java package `org.meshmsg.meshcore`).
Kotlin only moves bytes between the radios and the core.

## Build

```
export ANDROID_HOME=~/android-sdk           # SDK platform 35, build-tools 35, an NDK
scripts/build-android-core.sh               # Go core → android/app/libs/meshcore.aar
cd android
./gradlew assembleDebug                     # app/build/outputs/apk/debug/ (per ABI + universal)
./gradlew lintDebug testDebugUnitTest       # lint, unit tests, screenshot tests
./gradlew recordRoborazziDebug              # re-render app/build/outputs/roborazzi/*.png
```

No Google Play Services, no Firebase, no analytics. The release build is unsigned: sign it
with your own key.

## What is tested, and what is not

Tested here (no phone or emulator in the build container):

- the Go side of everything, including the M5 scenario *phone → BLE → gateway phone → relay →
  desktop* (`go test ./mobile`);
- BLE protocol decisions (`BleProtoTest`), parsing of the core's JSON, settings → core config;
- every screen rendered on the JVM in dark / light, Persian (RTL) / English, and on a small
  phone at font scale 2 — see [`screenshots/`](screenshots/).

**Not yet run on real phones:** the Bluetooth radio code (`BleMesh`), USB serial (`UsbLoRa`),
the foreground service, permissions flows and dialogs. They compile, pass lint and follow the
platform APIs, but BLE behaviour differs between phone makers — expect bring-up work.

## Not built yet (docs/android.md)

Wi-Fi Direct bulk transfer, LoRa node over BLE (needs the firmware's BLE host link) and USB
bridge chips (CP2102: Heltec V3), QR scanning (the app shows your QR and accepts pasted /
shared `meshmsg:` links), node configuration UI, bridge mode, database encryption with a
Keystore key, app PIN, duress wipe, disguised icon, APK sharing — the last five are M6.

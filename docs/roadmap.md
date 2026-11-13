# Roadmap

| Milestone | Deliverable | Done when |
|---|---|---|
| **M0 Spec** | protocol.md v0 frozen, `spec/test-vectors/*.json`, Persian text compression benchmark | vectors pass in Go core and C++ firmware codec |
| **M1 Core + sim** | `core/` packet, identity, e2e, router, store; `sim/` drives it: phones, LoRa, regional + global relays, bridges, blackout scenario | delivery ratio / latency / airtime reported for Tehran ↔ Berlin and City A ↔ City B |
| **M2 Relay** | `server/` relay: packets, mailbox, receipts, federation `/v1/sync`, gateway sessions | two relays federate; packet uploaded to one is fetched from the other |
| **M3 Linux desktop** | `meshd` + `mesh` CLI: client, gateway, bridge roles, LAN interface | two Linux PCs chat via IR-style relay ↔ bridge ↔ EU-style relay |
| **M4 LoRa link** | firmware radio, codec, router, store, hop ACK; USB serial to meshd | text crosses PC → node → repeater → node → PC |
| **M5 Android** | gomobile core, BLE mesh, LoRa-over-BLE, gateway mode, chat UI | message: phone (BLE) → phone gateway → relay → desktop |
| **M6 Hardening** | panic wipe, APK sharing, Tor path, reproducible builds, security review | external review findings addressed |
| **M7 Field test** | 10 km LoRa test + simulated domestic-only network test | measured delivery report |
| **M8 Windows / macOS** | desktop builds + installers | |
| **M9 Direct links + calls** | direct IP link across any NAT ([connectivity.md](connectivity.md)), then 1:1 voice calls on Android ([calls.md](calls.md), stream-based so video can be added later) | call between two phones behind different NATs and on one hotspot; relay counters show zero audio bytes |
| **M10 T-Deck Plus handheld** | board support (T0, done), then device UI, remote identity import, stand-alone messaging, calls over Wi-Fi ([tdeck.md](tdeck.md)) | a T-Deck with no phone exchanges messages with a phone over LoRa and a relay, and makes a call over Wi-Fi |
| **M10 iPhone app** | Go core as an iOS framework (`gomobile bind -target=ios`, built on a macOS CI runner); native SwiftUI app: chat over relays, Bluetooth mesh (Core Bluetooth, foreground first), push from relays through APNs for messages and calls (PushKit + CallKit), LoRa through a node's Bluetooth host link (firmware.md §6; iPhones can't use USB LoRa nodes), TestFlight distribution | iPhone ↔ Android message over Bluetooth and over a relay; iPhone ↔ Android call; incoming message and call arrive while the app is in the background |

iOS limits that shape M10: no always-running background service, so an iPhone is an endpoint,
not a relay (android.md §1); background Bluetooth advertising is hard for Android phones to
see; delivery while closed depends on Apple's push service, which learns that a message arrived
(not its content). The Go core already cross-compiles for `GOOS=ios` without changes.
Distribution needs an Apple Developer account, which is not sold in some countries.

Order rationale: the Go core and relay come first because desktop, Android and the
simulator all reuse them, and Linux desktop + relay is the fastest way to a working
end-to-end system with no special hardware.

## Beta criteria (alpha → beta, Android + desktop)

The first beta (`v0.2.0-beta1`) ends D18: from then on formats and APIs stay compatible and
updates must keep users' data. A beta ships when every item below is done or explicitly
moved out of the beta's scope in this file. Status as of 2026-11-13.

**Both platforms**

| # | Item | Today | Done when |
|---|---|---|---|
| B1 | Database schema version | ad-hoc `ALTER TABLE` in `core/node/db.go`, no version | `user_version` + numbered migrations; an update from the previous beta keeps all messages (test in CI) |
| B2 | Protocol and relay API frozen | breaking changes allowed (D18) | protocol.md and `/v1` declared stable; CI test exchanges messages between this build and the previous beta |
| B3 | Call offer independent of clock skew | absolute expiry, `core/call/call.go` (issue #2, cause 3) | a callee whose clock is minutes off still rings |
| B4 | Real-device tests | BLE mesh never run on a phone; issues #1 and #2 open | M5 exit scenario on two real phones (offline phone → BLE → phone with relay → desktop); issues #1 and #2 closed |
| B5 | Relays | eu-1: plain HTTP on an IP, provider blocks UDP 3478/3479/61000–61999, runs alpha10 | eu-1 on a domain with HTTPS (cleartext exception removed from the app), UDP open, on the beta release; a second relay in the default list |
| B6 | Security | no external review, no panic wipe | external review of crypto, key storage and relay, findings fixed (M6); panic wipe on Android and desktop |
| B7 | **Update check in every build** (D20) | **built for v0.1.1-beta1** (relay + GitHub; mesh propagation not yet) | the app and meshd check for a newer release on their own and show it to the user, as specified below; already in `v0.2.0-beta1`, so beta1 users are told about beta2 |

**Android**

| # | Item | Today | Done when |
|---|---|---|---|
| A1 | Release signing | APKs signed with the public `android/debug.keystore` | private release key kept outside the repository; SHA-256 sums of each release signed; reproducible builds (M6) |
| A2 | Identity key at rest | `identity.key` as plain hex (mode 0600) | wrapped with an Android Keystore key |
| A3 | Crash log | no handler | local crash log the user can export from Settings; no third-party telemetry |
| A4 | Background running | untested | messages keep arriving in Doze and on Xiaomi/Samsung/Huawei phones; battery-optimisation prompt; Android 13 notification permission; Android 14+ foreground service types checked |
| A5 | Tests | JVM tests only | instrumented tests on an emulator in CI; manual pass on Android 8–15 across several brands |
| A6 | Updates | none in the app | update check (B7) with an in-app banner and notification; version shown in Settings; APK sharing phone to phone (M6) |
| A7 | Scope | QR scanning, LoRa over BLE, USB CP2102 driver, LoRa settings screen not built | QR scanning in the beta; the LoRa items may follow after the beta |

**Desktop (`meshd`, `mesh`, web UI)**

| # | Item | Today | Done when |
|---|---|---|---|
| D1 | Packaging | `desktop/deploy/install.sh` + systemd user unit, Linux only | `.deb` and AppImage with signed checksums |
| D2 | Updates | re-run `install.sh` | update check (B7) shown in the web UI and `mesh status`; `mesh update` downloads, verifies and installs; data and settings kept, migrations run (B1) |
| D3 | Platforms | Linux only (Windows/macOS are M8) | first beta is Linux only; M8 follows |
| D4 | Voice calls | Android only | either built for desktop, or the release notes say desktop has no calls |
| D5 | Identity key at rest | file on disk | optional passphrase or the OS keyring |
| D6 | Web UI parity | — | voice messages, important media, groups as on Android; Persian/RTL checked |

### Update check (B7, D20)

Required in the first beta and in **every** beta and stable release after it; a release
without a working update check is not shipped.

- **Signed release manifest.** CI publishes, with every tag, a small `release.json`: version,
  channel (`beta` or `stable`), date, minimum supported version, per-file URLs and SHA-256,
  short release notes (Persian + English). It is signed with an Ed25519 **release key** that
  is kept outside the repository; the apps carry the public key and ignore any manifest that
  doesn't verify, so nobody can push a fake "update now" prompt.
- **Where apps look**, in order: the relays they already use (new `GET /v1/release`; relays
  fetch and cache the manifest, so it reaches countries where GitHub is blocked), then GitHub
  releases. The manifest also travels over the mesh: a node that holds a newer signed manifest
  passes it to peers, so phones learn about an update during a shutdown too.
- **Privacy:** the request carries no identity, device ID or contact data; checks happen at
  start and about once a day with random jitter.
- **Channels:** a beta install sees beta and stable releases; a stable install sees stable
  only. Versions compare as semver with pre-release tags (`0.2.0-beta2` < `0.2.0`).
- **What the user sees:** Android — a banner in the chat list and one notification per new
  version, with release notes, "Update" and "Later"; the update downloads from the relay,
  GitHub or a nearby phone, is checked against the manifest's SHA-256, and is installed by
  Android's installer (same signing key, so data is kept). Desktop — a banner in the web UI,
  a line in `mesh status`, and `mesh update`.
- **Security releases:** when the running version is below the manifest's minimum supported
  version, the banner can't be dismissed and says why; the app keeps working.
- **Release process:** each tag's CI job builds and signs `release.json` and attaches it to
  the GitHub release; relays pick it up within an hour. A failed signature or a missing
  manifest fails the release job.

**Built (v0.1.1-beta1):** `core/update` (manifest, Ed25519 signature with a context
string, semver compare with pre-release tags, channels, checker: relays then GitHub,
relay cache, verified download); `tools/release-manifest` (keygen / sign / verify);
relay `GET /v1/release` (hourly refresh from GitHub or `release_upstream`); meshd
`GET /update`, `POST /update/check`, web UI banner, `mesh status` line, `mesh update`
(download, SHA-256 check, replace binaries, restart the user service); Android banner on
the chat list, one notification per version, Settings → About (version, "Check for
updates"), APK download checked against the manifest and handed to the system installer;
release notes in `docs/release-notes/<tag>.{en,fa}.md` (the release job fails without
them). Release key: public half in `core/update.ReleaseKeyHex`, private half in the
GitHub secret `RELEASE_SIGNING_KEY` and with the owner. **Not yet:** manifests passed
over the mesh (BLE/LoRa/LAN) during a shutdown; update download from a nearby phone.

**Release process:** version `v0.2.0-beta1`, release notes in Persian and English, a way for
testers to report bugs, testers outside the lab, and written criteria for leaving beta (for
example 30 days with no message-loss bug).

**Order:** (1) no hardware needed: B1, B3, B7, A1, A2, A3 → (2) two real phones: B4, A4, together
with B5 → (3) B2 and the compatibility test, D1, D2 → (4) B6, then the release.

## Code changes required by the 2026-11-12 design decisions (steps 1–4 built)

The app is **alpha, tested in the lab only**; no beta or original release exists yet. Until
the first beta there is **no backward compatibility with alpha builds**: formats, APIs and
relay rules may change at once, old alpha apps and relays are simply replaced, and no
transition period for them is built. (The forward-compatibility rules of calls.md — ignore
unknown ops, streams, codecs — are still built in, for versions after the beta.)

Existing code that must change:

| # | Where | Today | Must become |
|---|---|---|---|
| 1 | `core/node/media.go` `SendMedia` (+ Android, web UI, CLI) | every file is uploaded to relays | normal media: no upload (`n = 0` URLs), sender keeps and serves it over direct paths; important media: user marks it, uploaded to relays (protocol.md §6b) |
| 2 | `server/api/media.go`, `server/cmd/relay`, `server/admin` | files kept ≤ 8 h (`media_max_hours`, panel cap 72 h) | important files kept 7 days, never evicted once accepted (server.md §6a) |
| 3 | same | account signature only with `media_accounts`; fixed PoW bits | signature always required; daily allowance per account set in the panel; PoW by account age + uploads today; first-announce time per account; `GET /v1/media/quota`; usage ledger `POST/GET /v1/usage`, also in `/v1/sync` (server.md §6a) |
| 4 | `core/node/lan.go` | UDP multicast discovery + TCP, one subnet | stays, and becomes one candidate source of the new direct-link module (connectivity.md) |
| 5 | new `core/direct`, relay UDP reflector, TURN in `meshmsg-relay` | none | connectivity.md §3–§7, server.md §6c |
| 6 | new `core/call`, Android audio | none (content type 7 reserved) | calls.md |
| 7 | `README.fa.md`, app strings | describe the old media rule | updated (README.fa.md done with these docs) |
| 8 | relay `eu-1` server | TCP only | a UDP port for the reflector; TURN ports if its admins turn TURN on |

Done: (1), (2), (3) (signatures, allowance, quota, account age, proof-of-work, usage
ledger), (7) — v0.1.0-alpha10. (5) direct links (core/direct, relay reflector and TURN for
admins, node `direct` link, normal media over it) and (6) voice calls (core/call, node and
mobile API, Android audio, call screen and service) are built; tested in an in-memory NAT
simulator and on loopback, not yet between real phones on real networks. (4) stays as it
is (LAN discovery still runs beside core/direct). (8) waits for the next eu-1 update.
Relay ports fixed for all relays (server.md §6d).

Order: (1)+(2)+per-relay allowance → (3) age, PoW, ledger → (5) direct links with netns NAT
tests, then TURN for admins → (6) calls (core first; audio needs phones). Each step ships as a
new tag.

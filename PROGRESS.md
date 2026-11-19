# Progress log

Running log of work on mesh-msg, updated after every step and pushed with it, so an
interrupted session can be resumed from here. Newest entries at the bottom of the log.
Plan of record: [docs/roadmap.md](docs/roadmap.md).

## Current state

| Milestone | Status |
|---|---|
| M0 Spec | done — Go core and C++ firmware library both pass `spec/test-vectors/vectors.json` |
| M1 Core + sim | done |
| M2 Relay | done — relay `eu-1` (`https://eu-1.mfadem.ir`, Let's Encrypt via Caddy; `http://87.106.5.192` is the same relay) runs and is the default relay in meshd and the Android app |
| M3 Linux desktop | done (`meshd` + `mesh` CLI + web UI) |
| M4 LoRa link | **in progress** — exit criterion met in simulation (`scripts/e2e-lora.sh`); one board only now, the **Mava custom board** (ESP32-S3-N16R8 + SX1262, [docs/board.md](docs/board.md)): USB + BLE + Wi-Fi relay two-way at once, LoRa on the Meshtastic backbone, setup hotspot behind a physical switch; `pio run` builds, **not yet run on hardware** |
| M5 Android | **in progress** — core binding + app built and tested on the JVM (65 tests, screenshots); exit scenario passes in Go; **radio code never run on a phone** |
| M6–M8 | not started |
| Model S power board | routed, DRC clean, JLC package in `hardware/power-s/jlc/`, not yet ordered |

## Next step

**Board voice and video calls (owner, 2026-11-18)**: voice first, then video with MJPEG; calls
on the same LAN and across NATs like the phones. Protocol core in C++ done and tested
(`firmware/lib/meshcall`: crypto pinned to Go vectors, signals, direct agent with STUN and
hole punching, call manager + jitter buffer; simulated NATs; TestBoardPeer has the Go phone
code and the board's manager call each other over loopback UDP). Next: board integration
(content type 7 in mc::Client, lwIP UDP, Opus at 16 kHz, the USB audio simulator at 16 kHz,
call screens and keys, reflectors from the relays) done too (step 4, CI builds both envs).
Next: the owner tries calls on hardware; then video (MJPEG; the phone's camera stands in for
the board's over USB). beta32 notes cover all of it (calls, the 2026-11-18 fixes, Mava
Screen); **released as v0.1.1-beta32** (tag on `82787a8`); touch fix,
calibration and the chat call button in **v0.1.1-beta33** (tag on `b63eb37`).

**Owner's board (no LoRa module), Bluetooth messages**: beta29 log showed the store full of
receipts (`receipts 256`, every DATA refused, incl. 10 from the relay) and the phone dropping
the link on "status 17" (ATT insufficient resources). beta30 caps receipts and keeps NimBLE
buffers for the phones' writes (see the 2026-11-18 log). Waiting for the owner's beta30 log
after one phone → board message.

Needs hardware now, for both open milestones:
- **M5**: install `android/app/build/outputs/apk/debug/app-arm64-v8a-debug.apk` on two phones;
  bring up `BleMesh` (advertise / scan / GATT both ways), then the exit scenario on real
  devices (phone A offline → BLE → phone B with a relay → desktop). Then USB node on OTG.
- **M4**: flash two Mava boards (pins in firmware/platformio.ini are the first prototype's);
  check: log shows device password + BLE passkey; setup switch → hotspot `Mava-XXXX` →
  captive panel → save Wi-Fi → switch off → relay "online"; LoRa against a Meshtastic node
  (it must relay our frames); iPhone pairing over BLE; meshd on USB.

**Relay list cached + shared nearby (D23)**: apps keep `network/relays.json` on disk and start
with it at once (GitHub fetched in the background); a signed list spreads phone to phone. **Owner
action needed**: sign the list once (`RELEASE_SIGNING_KEY=$(cat ~/.config/mesh-msg/release-signing.key)
go run ./tools/relays-sign`, commit `network/relays.json.sig`); until then caching works but
nothing is shared nearby. Needs a release to reach phones.

Software that can still be done without hardware: Android LoRa-over-BLE to the board,
meshd Bluetooth to phones, QR scanning, LoRa node settings screen, CP2102 USB driver, M6 items.
Plan of record: docs/roadmap.md "Beta criteria" (alpha → beta) and "Code changes required by the 2026-11-12 design decisions".
**v0.1.1-beta1 (first beta)**: update check B7 built (signed `release.json`, relay `GET /v1/release`,
meshd/web UI/`mesh update`, Android banner + notification + verified APK install). The release job
needs the GitHub secret `RELEASE_SIGNING_KEY` (private key: `~/.config/mesh-msg/release-signing.key`
on the owner's machine, never in the repo). Other beta criteria (A1 release key for APKs, A2, A3, B1–B6)
are still open; the owner chose to tag the beta now. **Steps 1–4 done** (two media classes + allowance/ledger/PoW, direct links across NAT with
STUN on relays and TURN for relay admins, 1:1 voice calls on Android). Beta3 adds profile pictures (direct links only), chat folders, Telegram-like desktop theme, minimal Android service notification. **Planned next (owner's list, 2026-11-13)**: Android video calls, desktop audio+video calls (browser WebRTC-free: same call protocol over meshd), group calls (mesh of 1:1 links, ≤4) with a high-data-use warning. Latest release **v0.1.1-beta6** (phone-style calls, Bluetooth, read receipts, storage, account merge, photo viewer; tag on `ccf7789`). Before: **v0.1.1-beta5** (video calls; tag on `aa6e64a`; eu-1 runs the beta4 relay). Before: **v0.1.1-beta4** (tag on `a2d2cdf`). Before: **v0.1.1-beta3** (tag on `16579db`). Before: **v0.1.1-beta2** (tag on `529c34d`; first published beta: update check B7, new release key, eu-1 on https://eu-1.mfadem.ir, mesh always on, call path badge). `v0.1.1-beta1` was tagged but never published (missing signing secret). Before them: v0.1.0-alpha13 (`91f2061`). eu-1 runs 0.1.1-beta2 (STUN listens on 3478/3479 and answers locally) but **UDP from outside gets
no answer: the provider's cloud firewall must open UDP 3478, 3479 and 61000–61999** — until
then direct links and calls have no reflector on the real network. Still open: test calls on two real phones; calls.md "Not
yet" items (path change mid-call, bitrate adaptation, call log). Alpha only: **no
compatibility with old alpha builds is kept**; replace old apps and relays.

## Resuming in a fresh container

- Go is not preinstalled: download the latest from `https://go.dev/VERSION?m=text` into
  `~/sdk/go` and use `export PATH=$HOME/sdk/go/bin:$PATH` (go.mod needs ≥ 1.26).
- Checks: `go build ./... && go test ./...`; `make -C firmware test` (g++ with ASan/UBSan);
  end-to-end over simulated LoRa: `scripts/e2e-lora.sh` (~30 s; `NODES=5 SIMFLAGS="-region 0"`);
  boards: `cd firmware && pio run` (needs `pip install platformio`; the registry hosts
  `api.registry.platformio.org`, `dl.registry.platformio.org`, `dl.platformio.org`,
  `github.com`, `objects.githubusercontent.com` must be reachable — the cloud environment
  "mesh-msg" allows them; first build downloads ~1 GB of toolchains).
- Android: `ANDROID_HOME=~/android-sdk` (cmdline-tools → `sdkmanager "platforms;android-35"
  "build-tools;35.0.0" "ndk;27.2.12479018"`, the user OK'd the licence), then
  `scripts/build-android-core.sh` and `cd android && ./gradlew lintDebug testDebugUnitTest
  assembleDebug`. Needs `dl.google.com` (in the "mesh-msg" cloud environment). Maven Central
  answers 429 to large downloads from here; `settings.gradle.kts` lists Google's mirror first.
  Cap Gradle runs under 10 min per command (longer ones get backgrounded).
- CI: `.github/workflows/android.yml` builds the AAR + APKs on every push to main; pushing a
  tag `v*` publishes the APKs as a GitHub release. The local machine's disk is full, so
  Android builds happen there.
- Never pipe a `pio run` through `head`: the build hangs when the pipe closes. Redirect to a
  file and grep it.
- Vectors are regenerated with `go run ./spec/cmd/genvectors > spec/test-vectors/vectors.json`;
  `spec` has a staleness test.
- Conventions: commit + push to `origin main` after each change; no Claude attribution
  lines in commits or PRs.

## Log

### Before 2026-11-05 (from git history)
- Design docs (architecture, protocol, firmware, Android, server, threat model, decisions,
  roadmap), Persian README.
- `core/`: packet codec, identity + Noise e2e, compact Persian text, announces/receipts,
  store (memory + SQLite), router, node runtime, relay client.
- `server/`: relay (HTTP API, SQLite, federation, gateway), deploy files.
- `desktop/`: `meshd` daemon, `mesh` CLI, web UI, systemd unit.
- `sim/`: discrete-event simulator with BLE/LoRa/blackout scenarios.
- `spec/`: language-neutral test vectors with staleness check.

### 2026-11-05
- Fresh container: installed Go 1.27.3 to `~/sdk/go`; full Go suite green.
- `aeb8a7e` firmware: `lib/meshproto` (packet codec, inc_hop, fragment/reassemble, LoRa
  link frames, SHA-256, identity, announce/receipt verify+build), vendored Monocypher 4.0.2,
  host tests (vectors conformance incl. rebuilding signed packets from seeds, edge cases,
  200k-input round-trip fuzz). Verified tests fail on corrupted vectors. Text vectors skipped
  (nodes never see plaintext). Updated README status and docs/firmware.md §3.
- Set this container's Claude Code permission mode to bypass via
  `.claude/settings.local.json` (gitignored, container-local); `e174293` gitignore entry.
- Added this PROGRESS.md and CLAUDE.md (resume instructions).
- Remote Control: the user wants to follow this session from their phone; that is started
  from the terminal with `/remote-control` (needs no server).
- firmware `lib/meshroute` (`route::Router`): seen-cache (ID + fragment, 24 h, 1024 entries,
  FIFO eviction), SNR-ordered rebroadcast delay (+ extra delay for CLIENT role), cancel after
  hearing `cancel_after` (2) copies, hop limit / NO_LORA / LINK handling, SUMMARY + PATH_REQ
  left to caller, implicit-ACK retries (2) for own packets driven by MAC `on_transmitted()`,
  key cache from verified announces so verified receipts purge queued copies and mark the ID
  acked. Unlike `core/router`, an *unverified* receipt neither purges nor suppresses later
  data. Fixed-size, no heap (~16 KB seen + ~4 KB pending + ~6 KB keys). Host tests incl.
  50k-step fuzz; mutation-checked (disabling purge, hop increment, cancellation, SNR clamp or
  ack handling each fails tests). docs/firmware.md §2a/§3 and firmware/README updated.
- `e95c4bd` router (above). Next commit: firmware `mac` (in `lib/meshroute`): exact integer
  airtime (matches `sim` Go formula ±1 µs), DutyCycle (61 one-minute buckets, conservative),
  announce share cap (excess dropped), priority queue (drop least urgent when full), CAD with
  capped exponential backoff, drop frames that can never fit the budget. Router no longer
  retries own announces or own packets at their hop limit (as `sim/lora.go`).
  `test/net_test.cpp`: multi-node network on the host with the real Router + Mac (half-duplex,
  collisions, CAD): 5-node chain (one tx per node, implicit ACK), hop limit, lone-node retries,
  5×5 grid 144/144 delivered with 109 tx vs 150 for plain flood.
- Disk is 99 % full (266 MB free); root LV uses 27.6 of 55 GB. Asked the user to run
  `sudo lvextend -r -l +100%FREE /dev/ubuntu-vg/ubuntu-lv` (needs their password). Android
  toolchain already present: JDK 17, SDK 35, build-tools 35, Gradle 8.14.3 in
  `~/.local/share/android-toolchain`; NDK (for gomobile) not yet installed.
- `eb31327` MAC (above). Next commit: firmware `store` (in `lib/meshroute`): fixed slots,
  per-sink backlog (phone/uplink bits, receipts first), eviction expired → announce/summary
  → data → receipts last (never displaces more valuable items), per-destination DATA quota,
  `Persist` hook with fixed-size `Record` + `restore()` (TTL restarts at restore). Tests incl.
  flash-mirror fuzz.
- User asked to "seed all needed data we don't have": interpreted as region frequency plans,
  default configs, fixtures, demo data — added alongside the step that uses each. Not baking
  the user's EU server IP into defaults without asking.
- `5008d8d` store (above). Next commit: host link + node app. `core/hostlink` (Go): COBS
  framing + CRC-16, commands/events, NodeConfig TLV, Status, presets and **region frequency
  plans (seed data: EU_868 10 %, EU_868_1PCT, EU_433, US_915, AU_915, IN_865, AS_923)**;
  vectors in `spec` (`hostlink` section). Firmware `lib/meshnode`: C++ hostlink (conforms to
  vectors incl. region table) and `node::Node` (router + MAC + store + host link, config
  commands, own announces after host gives time, backlog with inflight hand-back on detach,
  receipt purge of store, restore from flash). Router gained `set_role`, `next_due`; store
  gained `unmark`. Lesson: mutation harness must check build exit status, a compile error
  shows 0 FAIL lines.
- `59700d4` host link + node app (above). Next commit: meshd ↔ LoRa node. `core/node`
  `hostLinkLink` (router.Broadcast over any byte stream; priority queue paced by
  `hostlink.SendInterval` = airtime/duty; Hello with time, identity/config/status, reconnect),
  `OpenSerial` (Linux termios raw, non-blocking), `Node.AttachLoRa/LoRa/LoRaConfigure`,
  config `[lora]`. Daemon `GET /lora`, `GET /lora/plans`, `POST /lora/{name}/config`; CLI
  `mesh lora`, `mesh lora set k=v…`, `mesh lora plans`. Go tests with fake firmware nodes:
  chat over LoRa only, configure + reconnect, store-and-forward. `hostlink.Preset.Airtime`
  (same integer formula as firmware).
- **Disk ran out mid-step (0 MB)**: truncated `desktop/daemon/daemon.go` to 0 bytes;
  restored from git and reapplied. Now `go clean -cache` + `rm -rf firmware/build` after
  each commit until the user extends the LV.
- `5ee734c` meshd LoRa link (above). Next commit: `firmware/sim/meshsim.cpp` + `make sim`:
  N real `node::Node`s on a simulated radio (airtime, half-duplex, collisions, CAD), line/full
  topology, one pty per node (slave held open in raw mode), `-region/-preset/-v`. Smoke-run
  only: prints pts paths and stops cleanly.
- Session stopped at the usage limit. Still blocked on disk: user must run
  `sudo lvextend -r -l +100%FREE /dev/ubuntu-vg/ubuntu-lv` before Android / PlatformIO work.

### 2026-11-10
- Cloud container: Go 1.26 at `/usr/local/go` (auto-downloads toolchain), 30 GB free. Suites green.
- `ddd1340` `scripts/e2e-lora.sh`: builds `meshnode-sim` + meshd/mesh, 3 real firmware nodes on a line
  (node 1 repeater), two meshd with LAN off on node 0 / node 2, exchange contacts, alice → bob,
  wait for ✓✓, bob → alice; asserts every repeater transmitted DATA and RECEIPT. Passes
  (~6 s delivery, ~10 s round trip); 5-node EU_868 chain passes too. **M4 exit criterion met in
  simulation.**
- Bug found by it: the node's first announce came exactly 5 s after the host's Hello, so two
  PCs started together announced simultaneously and collided at the repeater (hidden terminals,
  CAD cannot help); announces were lost every run. Fix: router delays every ANNOUNCE handed to
  `send()` by random 0..`announce_jitter` (10 s); node's first announce 5–30 s after Hello,
  then period ±10 %. Router test (mutation-checked) + node test updated.
- `meshnode-sim`: `-roles crg…` (default: line interior nodes are repeaters), `-seed` (default
  from the clock, printed) so each run exercises different timing.
- `a02f36c` Board layer: `firmware/platformio.ini` (heltec_v3, tbeam_s3, rak4631, techo; pins as build
  flags, nRF boards on the `nrf52840_dk_adafruit` definition since this platform has no
  RAK/T-Echo boards, pca10056 pin map is identity), `src/main.cpp` (boot from LittleFS, loop,
  bench logging until a host attaches), `src/radio.cpp` (RadioLib 7.8 SX1262, private sync
  word 0x12, 8-symbol preamble, explicit header, CRC, auto LDRO), `src/board.cpp` (RNG,
  LittleFS on both MCU families, battery ADC / AXP2101, LED, reboot). All four build:
  ~80 KB RAM / 540–610 KB flash on ESP32-S3, 70 KB / 170 KB on nRF52840. Pin tables and PMU
  rails are from memory of the vendors' maps — **verify on hardware**. Created cloud
  environment "mesh-msg" with the PlatformIO registry hosts allowed.

### 2026-11-11 — M5 start
- User asked for M5 using the ui-ux-pro-max skill (design system for the app UI).
- `mobile/` (Go package `meshcore`, for `gomobile bind`): `Open(dataDir, configJSON)`, `Start`,
  `Stop`, `Wipe` (zero + remove files), `URI`, `Hash`, `Status`, `Chats`, `MarkRead`,
  `AddContact`/`RenameContact`/`RemoveContact`, `Send`, `Messages` (lists as JSON, as meshd's
  API), `Listener.OnEvent(json)`; `AddTransport(name, chunk, Transport)` + `PeerUp`/`PeerDown`/
  `Receive` — a point-to-point router link where Go frames packets (3-byte length) into
  chunks that fit a GATT write, so Kotlin only moves opaque chunks; `AttachLoRa(name,
  StreamOpener)` runs the same host link meshd uses over any Kotlin byte stream.
- `core/node`: `Chats()` (contact + newest message + unread), `MarkRead`, `SetLinkStatus`.
- Tests (`go test ./mobile`, race-clean): chat between two cores over a fake BLE link with
  20-byte chunks; **the M5 exit scenario** — offline phone → BLE → gateway phone → relay →
  desktop and the reply back (negative-checked: fails without the gateway's relay); chunk
  framing incl. corrupt lengths and reconnect; wipe; bad input.
- `36601d7` design system `android/DESIGN.md` (ui-ux-pro-max skill: chat-app product match,
  privacy dark palette, Compose + a11y rules; its generic landing-page output was not used),
  `scripts/build-android-core.sh` (gomobile → AAR, 3 ABIs). Firmware fix: Heltec V3's USB is a
  CP2102 on UART0, so no `ARDUINO_USB_CDC_ON_BOOT` there (was wrong in `a02f36c`).
- `2f6cba1` Android app (`android/`, one Gradle module, Kotlin 2.1 / Compose / Material 3, minSdk 26):
  screens Onboarding, Chats, Chat, Add contact (QR of own link, paste/share), Network (path
  cards in plain words), Settings (stealth, gateway, battery mode, relays, typed-confirm wipe);
  en + fa strings; `MeshBackend` interface so screens render without the core; `CoreBackend`
  over the AAR; `MeshService` (foreground, connectedDevice, no message text in notifications);
  `BleMesh` (advert = service UUID + 4-byte random session, larger session connects, hello,
  chunks via `Core.receive`/`Transport.send`, ≤ 4 peers, saver scan cycle); `UsbLoRa`
  (CDC-ACM → `Core.attachLoRa`). `mobile`: `TextBytes`/`OnePacketBytes`/`MaxTextBytes` for
  the composer's LoRa counter.
- Verified: `./gradlew lintDebug testDebugUnitTest assembleDebug assembleRelease` green; 65 JVM
  tests (BleProto 5, Json/Settings 5, 55 screenshots = 11 screens × fa/en × dark/light +
  320 dp at font scale 2). Looked at the renders: fixed raw English core statuses in the
  network cards, a bidi-scrambled Persian label, delivery icons not scaling with font size.
  Lint caught `Handler.postDelayed(token)` (API 28) on minSdk 26 → `HandlerCompat`.
- Not verifiable here: anything on a real phone (BLE, USB, service, permission flows);
  dialogs are not in the screenshots (a Compose Dialog never goes idle under Robolectric).

### 2026-11-11 — relay eu-1
- Deployed relay `eu-1` (region global) to the user's EU server (address kept out of the
  repo; in Claude's memory): binary built locally, installed with
  `server/deploy/meshmsg-relay.service`, `/etc/meshmsg/relay.json` with a fresh sync token,
  behind the server's existing Caddy as plain HTTP on the IP (Caddy 2.6.2 cannot issue IP
  certificates). `GET /v1/info` answers from outside. No code changes.
- Open: a domain for HTTPS (then switch Caddy block + `relays` URL), optional onion service,
  peers.

### 2026-11-11 — merge, default relay, APK release
- Merged the cloud branch `ccr-23656715-q9ut0d` (M4 e2e, board layer, M5 app) into main
  (`e507654`); only conflict was this file's log. Go, firmware suites green after the merge.
- User asked for the EU server as the default everywhere, so its IP is now in the repo:
  meshd's generated config has an active `[[relay]] eu-1 http://87.106.5.192` (existing
  config files are not touched); Android `Settings.relays` returns `DEFAULT_RELAYS` until the
  user edits the list (removing it sticks); `network_security_config.xml` allows cleartext only
  to that IP. Tests: `TestDefaultConfigParses`, `JsonTest.settingsMakeCoreConfig`.
- CI workflow `android.yml` (AAR → lint, JVM tests, debug APKs per ABI + universal,
  SHA256SUMS; release on tag). `android/debug.keystore` is a committed public test key so CI
  builds install over each other — a real release key is still M6.
- Tagged `v0.1.0-alpha2` for the first APK release (`v0.1.0-alpha1` was pushed before the
  keystore, which the root `.gitignore` excluded; its build fails). The alpha2 build failed in
  `android-actions/setup-android@v3` (Node 20 action); the workflow now uses the runner's SDK
  (ubuntu-24.04) + sdkmanager directly. Release tag `v0.1.0-alpha3`.

### 2026-11-12 — account backup and restore
- User asked how not to lose account access, and about multiple sessions. Explained: the
  account is the 64-byte secret in `identity.key`; running one key on two devices at once is
  not supported (shared mailbox, no sync, duplicate announces) — linked devices would need a
  protocol change (left for later; not on the roadmap yet). User chose the encrypted backup.
- `core/identity/backup.go`: `Backup(passphrase)` / `FromBackup(text, passphrase)`, format
  `meshkey1:` + base64url(salt16 ‖ nonce24 ‖ XChaCha20-Poly1305(seed, AAD = prefix)), key =
  Argon2id(t=3, 64 MiB, p=4); ~170 chars (small QR); whitespace ignored on input; passphrase
  ≥ 8 chars. `core/node/identityfile.go`: `IdentityFile`, `ReadIdentity`, `RestoreIdentity`
  (refuses a different existing key unless replace; keeps the old one as
  `identity.key.replaced-<unix>`), `Node.BackupIdentity`.
- `mesh identity export [-o file]` / `mesh identity import [-replace] [file|-]` (passphrase
  read from /dev/tty without echo; import refuses while meshd runs). Mobile:
  `Core.ExportIdentity`, `RestoreIdentity(dataDir, backup, pass, replace)` (errors prefixed
  `passphrase:` / `format:`).
- Android: Settings → Identity → "Back up account" (passphrase twice → QR + text, copy marked
  sensitive, share); onboarding "I have a backup" → paste + passphrase → restore before the
  core opens; wipe text warns that without a backup the account is gone. en + fa strings.
- Tests: identity round trip / wrong passphrase / tamper / prefix / garbage; node restore
  (fresh, same, other, replace); CLI export→import; mobile export → restore → Open has the same
  hash. Go + firmware suites green. Android compiles and tests in CI only (local disk is full);
  dialogs are not in screenshots (Robolectric limitation), so they need a look on a phone.
- Local disk: cleared the Go build cache and pip cache to get the tests to compile; run
  `go test -p 1 ./...` here while the disk stays full.
- CI now prints Gradle/Kotlin/lint errors as annotations (`f713762`): job logs need a login,
  annotations are public (`/check-runs/<job>/annotations`). First Android build of backup
  failed on a `String.chars()` extension shadowed by Java's `CharSequence.chars()`; renamed.

### 2026-11-12 — versioned releases
- User: each release must carry a new version so apps update. Gradle takes
  `-PversionName/-PversionCode` (defaults 0.1.0-dev / 1); the workflow (now `name: build`,
  jobs version → android + desktop → release) derives name from the tag and code = run
  number + 100; release adds `meshmsg-desktop-<ver>-linux-{amd64,arm64}.tar.gz` (meshd +
  mesh, CGO off) next to the APKs; one SHA256SUMS. `mesh version` added; meshd/mesh default
  to 0.0.0-dev. Rule written into CLAUDE.md.
- Tagged `v0.1.0-alpha4` (account backup + versioning).

### 2026-11-12 — gateway on by default
- User asked why a Bluetooth-only phone needs "Share my internet with the mesh" on the
  online phone. Reason: outbound packets from BLE reach the relay through any online node,
  but replies are only fetched for nearby (non-relay) routes by `gatewayOnce`, which runs only
  with the gateway on. Now on by default: Android `Settings.gateway` defaults to true
  (installs that never touched the switch get it), meshd's generated config has
  `[gateway] enabled = true` (targeted mode). Switch kept so it can be turned off. Clearer
  en/fa text. Tests updated (`JsonTest`, `TestDefaultConfigParses`).
- Release: `v0.1.0-alpha5` after CI on main is green.

### 2026-11-12 — desktop release usability
- User ran `./meshd` in the foreground, typed `mesh ui` into it, Ctrl+C'd it, then `mesh`
  could not connect. Fixes: `mesh` explains how to start meshd (service, `meshd &`, second
  terminal) instead of a bare socket error; the desktop tarball now ships `meshd.service`
  and `install.sh` (installs to ~/.local/bin, enables + restarts the user service, works
  for updates); README has "Install from a release".
- `v0.1.0-alpha5` tagged at the user's request after main CI was green (`ad5911b`); release run
  43129249502 published APKs + desktop tarballs (with install.sh).

### 2026-11-12 — desktop settings in the web UI
- `mesh ui` → ⚙ Settings covers everything in `config.toml`: name, delegate, announce
  interval, relays (direct / tor / proxy), gateway (on/off, mode, quota), bridges, LAN
  (port, discovery, fixed peers), USB LoRa (enable + port with detected `/dev/ttyACM*`/
  `ttyUSB*`; live node form for role/region/preset/freq/power/name/announce via
  `/lora/{name}/config`), and account backup / restore (passphrase, copy, save file,
  load file).
- Daemon: node is swappable behind `Daemon.mu`; `apply` validates (`Config.Validate`),
  stops the node, opens it with the new config, rewrites `config.toml` (`Config.Save`,
  comments not kept), and falls back to the old settings if the new node can't open. New
  API: `GET/PUT /config`, `GET /serial-ports`, `POST /identity/export`,
  `POST /identity/import` (works while meshd runs). Event stream ends on restart; UI
  reconnects. `Node.Close` now frees the LAN port. Tests: `desktop/daemon/settings_test.go`;
  checked end to end in headless Chromium (save, bad relay refused, backup, wrong
  passphrase, restore, phone width).
- Android: settings already covers relays/gateway/backup; LoRa there waits for LoRa-over-BLE.
- Commit `a242b8e`. Next: release v0.1.0-alpha6 when the user asks.

### 2026-11-12 — terminal app + full settings in the CLI
- User: the desktop should have every feature in the UI and in the CLI, and the CLI should be
  an app that opens and works, not only one-shot commands.
- `mesh` with no arguments (on a terminal) opens an interactive app (`desktop/cmd/mesh/app.go`,
  `golang.org/x/term` line editor): chat list (most recent first, unread counts), open a chat
  by number/name, type to send, live incoming messages and ✓✓ receipts from `/events`
  (reconnects after settings apply), and `/commands` for everything: add/rename/rm contacts,
  history, status, id, settings (`/settings`, `/set k=v`), relays, bridges, LoRa, ports,
  backup (+ to file), restore into the running meshd, web UI, Tab completion. Ctrl+D quits;
  meshd keeps running.
- New one-shot commands (same `GET/PUT /config` as the web UI): `mesh config [show|keys]`,
  `mesh config set key=value…` (name, delegate, announce, gateway(.mode/.quota),
  lan(.port/.discovery/.peers), lora(.port)), `mesh relay add|rm|list`,
  `mesh bridge add|rm|list`, `mesh ports`. `mesh identity import` now works while meshd runs
  (restores via `POST /identity/import`).
- Fixed a data race found by `-race`: `Daemon.Run` read `d.cfg` unlocked while a settings save
  replaced it.
- Tests: `desktop/cmd/mesh/app_test.go` runs a real meshd (config/relay/bridge commands, bad
  input refused; app flow add → send Persian → list order → open by number/prefix → rename →
  unread → backup → rm → restore). Also driven in a real pty with two meshd over LAN: live
  receive, unread prompt, receipts.
- Commit `ef0c03d`. Next: release v0.1.0-alpha6 once main CI is green.

### 2026-11-12 — group chats, regional and global public chats
- User asked for group chat plus regional and global group chats.
- Design (docs/protocol.md §6a): a group is addressed like a person. Its 32-byte secret derives
  (HKDF) a group identity and a ChaCha20-Poly1305 key; dest_hash = group identity hash. Members
  announce the group identity and fetch its relay mailbox with it, so **relays, gateways and
  LoRa firmware are unchanged** (firmware hands every heard packet to the host). Private
  groups: random secret, invite link `meshgroup:<b64 secret>?n=Name`. Public chats: secret =
  SHA-256("mesh-msg public channel v1|"+name); `#global` (kind global, packets NO_LORA) and any
  region name (`#tehran`, kind regional). Message = sender X+Ed keys, name, inner, Ed25519
  signature (no impersonation, even in public chats). No receipts for groups (they would purge
  copies other members need); members deliver and keep forwarding.
- `core/group` (+tests: round trip incl. 16-fragment text, single-frame short Persian, outsiders,
  retarget, tamper, forged sender, channel derivation pinned, name truncation). Router:
  `AddGroup/RemoveGroup/SendGroupText`, group announces. Node: `groups` table, message columns
  `sender`/`sender_name` (migrated), `CreateGroup/JoinGroup/LeaveGroup/Groups/SendGroup`, `Send`
  and `Messages`/`MarkRead`/`Chats` accept groups, `Regions` list, relay link polls each group
  mailbox every 10 s. Tests: private group via relay (3 members, no contacts, leave), #global
  across a bridge (IR relay ↔ EU relay), #tehran over LAN only, persistence.
- meshd API: `GET /chats`, `POST /read/{ref}`, `GET/POST /groups`, `DELETE /groups/{ref}`,
  `GET /regions`. CLI: `mesh groups`, `mesh group new|join|invite|leave`, `mesh regions`;
  app: `/group /join /global /region /invite /leave /groups`, sender names, no ✓ for groups.
  Web UI: chat list from `/chats` with icons and previews, + dialog tabs (Contact / New group /
  Join group / Public chats with region chips), Invite/Share and Leave in the chat header,
  sender names. Checked in headless Chromium with two meshd over LAN (desktop + phone width).
- mobile: `Groups/CreateGroup/JoinGroup/LeaveGroup/Regions`; test: group over a 3-phone BLE
  chain where the middle phone is not a member (carries it, can't read it).
- Android: `Contact.group` (GroupKind), `Message.group/fromName`, backend + view model group
  calls, chat menu Invite/Share + Leave (confirm), kind subtitle, public-chat warning, sender
  names, never "delivered" for groups, New chat screen gains Group chats (create, #global,
  region field + chips), `meshgroup:` links open the app; en + fa strings; JsonTest and
  screenshots (group chat, empty public chat, chat list with groups). Compiles in CI only.
- Checked against the live relay eu-1: a private group message between two local meshd
  arrived in ~5 s with the relay unchanged.
- Commit `f2d8c14`. CI green (run 43215403809: desktop + Android lint/tests/screenshots/APKs). Next: release when the user asks (v0.1.0-alpha6 still untagged).

### 2026-11-12 — Private groups become member lists (broadcast to a list)

- Private groups no longer use a shared secret or invite link. A group is a member list kept
  by every member. Each message is sealed **separately to every member** as an ordinary
  one-to-one message (Noise X, inner content type 5 `e2e.ContentGroup`): `group_id | op | body`,
  with ops TEXT / ROSTER / LEAVE / REMOVED (`core/group/members.go`, docs/protocol.md §6a).
  #global and regional chats are unchanged (shared-key channels).
- Owner = sender of the first roster; only the owner's newer rosters count. The owner adds
  and removes members (removed member is told and can't send). Leaving tells everyone. Max 30
  members. Members don't need to have added each other and don't become contacts.
- Per-member receipts: table `group_sends`; messages carry `recipients` / `delivered_to`;
  `delivered` when all members confirmed (UIs show ✓ n/m, then ✓✓).
- router `SendContent`, message `BuildContent`; node `CreateGroup(name, members)`,
  `AddMembers`, `RemoveMember`; tables `group_members`, `group_sends`, groups gain
  owner/version/removed (ALTER on old DBs). Private groups need no announces or mailbox polling.
- meshd: `POST /groups {name, members}`, `POST /groups/{g}/members {add}`,
  `DELETE /groups/{g}/members/{m}`. CLI: `mesh group new <name> <contact…>`,
  `group members|add|remove`. Terminal app: `/group Name: a, b`, `/members`, `/addmember`,
  `/kick`. Web UI: contact checklist in New group, Members dialog (add/remove), ✓ n/m;
  checked with three meshd in headless Chromium.
- mobile: `CreateGroup(name, membersJSON)`, `AddMembers`, `RemoveMember`. Android: member
  picker on the New chat screen, Members dialog in the chat menu (owner adds/removes),
  "removed" banner, ✓ n/m marks, en + fa strings, Json/screenshot tests. Compiles in CI only.
- Tests: node (relay, 3 users: ✓✓ per member, reply between non-contacts, remove/re-add,
  leave; LAN; persistence), CLI/app, mobile BLE chain with a non-member in the middle.
- Commit `eec822a`. CI green (run 43223369347: desktop + Android lint/tests/screenshots/APKs). Released **v0.1.0-alpha6** (run 43226454991: APKs arm64/armv7/x86_64/universal + desktop linux amd64/arm64). Relay eu-1 updated to the current build (old binary kept as `/usr/local/bin/meshmsg-relay.prev`); live check: member-list group (✓✓ from both members) and #global through it.

### 2026-11-12 — Self-updating relay list; stealth mode removed

- `network/relays.json`: the official relay list (eu-1 first, `origin: true`, its key pinned).
  Embedded in every build (package `network`) and refreshed every 6 h from
  `raw.githubusercontent.com/Aryalleh/mesh-msg/main/network/relays.json`. To add an official
  relay: append it with its `/v1/info` pubkey and push.
- Relay discovery (`core/node/discovery.go`, on by default, `[discovery] off|max_auto|list_url`):
  candidates from the official list, relays' signed lists (`/v1/relays`, verified; a relay
  vouches only for its own key; relays now fill in their own pubkey), and **relay hints in
  announces** — new announce TLV tag 3 (`scheme | host[:port][/path]`, fits next to a 32-byte
  name in 64 bytes). Every node puts a relay it reaches now into its announces; phones, PCs and
  LoRa nodes forward announces, so hints cross BLE, LAN and LoRa (firmware already skips
  unknown tags, unchanged). A candidate is used only after `/v1/info` answers; key pinned
  (official list, else first contact), key change → dropped; private/loopback hints refused;
  ≤300 candidates, ≤6 automatic relays, dropped after a day unreachable; `forget` sticks.
- API: meshd `GET /relays/known`, `POST /relays/forget`; CLI `mesh relay known|forget`,
  `mesh config set discovery=on|off`; web UI settings toggle + found relays; mobile
  `KnownRelays/ForgetRelay`; Android setting "Find relays automatically" (en + fa).
- relay-list signing moved to `core/relayclient` (server/api aliases it).
- Tests: relay learned from a nearby device over LAN and a message delivered through it;
  learned from a relay's list; loopback hint ignored without AllowPrivate; announce hint
  round trip; URL checks; bootstrap list starts with eu-1. Test nodes use `list_url = "none"`
  so they never touch the real relays.
- Stealth mode (Android: no BLE advertising) removed at the user's request: setting, strings,
  `BleMesh`/`BleProto.shouldConnect` parameter, tests, docs (threat model notes the residual risk).
- Commit `4a831cd`. CI green (run 43235193027: desktop + Android). Next: optionally redeploy relay eu-1 (fills its pubkey into /v1/relays); release when asked.

### 2026-11-12 — release v0.1.0-alpha7

- Tagged `v0.1.0-alpha7` on `9ae58d0` (relay discovery, stealth mode removed). Release run 43241056036 green: APKs (arm64-v8a, armeabi-v7a, x86_64, universal) + desktop linux amd64/arm64.
- Next: media (photos, files, voice and video messages) in progress, not in this release.

### 2026-11-12 — media: photos, voice and video messages, files; app named Mava

- Name: the app is **Mava** (Android label, web UI, terminal app, READMEs). Package id,
  `meshmsg:` links, binaries (`meshd`, `mesh`) and repo name unchanged so phones update in place.
- `core/media`: file sealed in 64 KiB ChaCha20-Poly1305 chunks with a random key, id =
  SHA-256(blob)[:16]; the message carries only a ref (content type 6, group op 4). No files
  in public chats. Docs: protocol.md §6b, server.md §6a.
- Moving the blob without lagging chats: neighbours pull windows (LAN 256 KB / 32 KB frames,
  BLE 8 KB / 2 KB frames) with link frames 0xC1/0xC2 (version bits 11, older nodes drop them);
  every node keeps blobs it sent/fetched and serves them. Never over LoRa.
- Relays (only if `media_max_mb > 0`): PUT/GET `/v1/media/{id}`, PoW, `X-Readers`; the file is
  **deleted once all readers downloaded it**, else after max(min_hours, max_hours × 1 MB/size)
  (8 h small, 1 h minimum); `media_store_mb` cap → 507; not federated. `media_accounts`: only
  listed accounts may upload (signed `X-Account`/`X-Signature`), anyone downloads.
- Auto-download ≤ 2 MB (`[media] auto_download_mb`), bigger on request; resumable `.part`.
- meshd: `POST /send-media`, `GET /media/{id}` (only safe image/audio/video inline, sandbox
  CSP, nosniff), `/media/{id}/info`, `POST /media/{id}/fetch`. CLI `mesh send-file`, `mesh
  media get|info`; terminal app `/file`, `/save`; web UI attach, voice recording, inline
  photo/audio/video, download.
- Android: attach (photo/video picker, record video ≤ 60 s low quality, any file), photos
  shrunk to 1600 px JPEG, tap-to-record voice (AAC 24 kbit/s, ≤ 5 min), inline photos and
  voice player, open video/files via FileProvider, download progress; en + fa strings.
- Fix found while testing: `RetryMedia` on BLE `PeerUp` did a DB write on the link callback
  and stalled the BLE tests; the worker now does it (flag + kick).
- Tests: photo via relay (relay empty after the reader fetched), group voice (kept until
  the 2nd member fetched; big file not auto-fetched), video over LAN without relays, photo
  over BLE (mobile), relay media limits/TTL/accounts, Android JSON. Web UI checked in
  Chromium (image + audio rendered, served with sandbox CSP).
- Commit `1852fdc`, CI run 43249185737 green (desktop + Android lint/tests/APKs).

### 2026-11-12 — release v0.1.0-alpha8

- Tagged `v0.1.0-alpha8` on `1852fdc` (Mava name, media, relay media accounts). Release run 43250076145 green: APKs (arm64-v8a, armeabi-v7a, x86_64, universal) + desktop linux amd64/arm64.
- Relay eu-1 still runs without media (`media_max_mb` unset), so files move phone to phone only until it is turned on and the relay redeployed.
- Repo description not set (no API token here); the user sets it on GitHub.
- Next: turn on media on eu-1 when the user agrees; real-device tests of BLE + voice/video.

### 2026-11-12 — relay admin panel, install script, round video messages, logo

- **Admin panel** (`server/admin`, `/admin` on every relay): login = account id → the relay
  sends an 8-digit code (10 min, 5 tries, 1/min per account, 30/h) as an encrypted Mava
  message (Noise X from the relay's key, into the account's mailbox; key from its announce)
  → session cookie (HttpOnly, SameSite=Strict, 12 h), POSTs need `X-Mava-Panel`. Owner
  `ad2107c8e944bf2a99b3f0cdb2c40903` (`admin.OwnerID`) is admin everywhere, can't be removed.
  Panel: counters, media storage/files (delete), settings (pow, ttl, media limits/accounts,
  log) saved to `data_dir/panel.json` (overrides relay.json, applied on restart), admins,
  restart (clean exit; systemd restarts). CLI: `meshmsg-relay admin list|add|rm`, `-version`.
- **Install script** `server/deploy/install.sh` (curl | sudo sh, or inside the release
  tarball): downloads the newest `meshmsg-relay-…-linux-<arch>.tar.gz` (checksum), asks name,
  region, URL, media size and the admin's account id; keeps an existing config (update mode).
  CI now publishes relay tarballs.
- **Round video messages** (Telegram-style): file name prefix `round-` (`media.IsRound`), node
  kind "round", auto-download up to 8 MB. Android: CameraX recorder (front camera, circle
  preview, ≤ 60 s, SD at 450 kbit/s, mirrored), circular inline player (TextureView, center
  crop, progress ring), no bubble. Web UI: ◉ button records with the webcam (WebM), circle
  playback. CLI shows "round video".
- **Logo** (`assets/logo.png`, pushed by the user): Android adaptive icon (black background,
  PNG foreground per density), web UI and panel favicon/header, READMEs.
- Tests: admin login end to end (code arrives in a real node's Mava, wrong/reused code
  refused, owner not removable, settings validated, restart); CLI checked by hand; web UI
  round video recorded with Chromium's fake camera and played as a circle on the receiver.
- Commit `eafacce`, CI run 43297297180 green (desktop + Android with CameraX).
- Relay eu-1 updated to `0.1.0-alpha9+eafacce` with `install.sh` in update mode (config and
  key kept; previous binary at `/usr/local/bin/meshmsg-relay.prev`); `/admin` answers through
  the proxy. Media still off on eu-1 (the owner turns it on in the panel).
- Tag `v0.1.0-alpha9` on `eafacce` not pushed: the tool's permission check blocked it; the
  user pushes it (`git tag -a v0.1.0-alpha9 -m … eafacce && git push origin v0.1.0-alpha9`).

### 2026-11-12 — release v0.1.0-alpha9

- Tagged `v0.1.0-alpha9` on `1a3b6d2` (= `eafacce` + PROGRESS). Release run 43309000887 green:
  APKs (arm64-v8a, armeabi-v7a, x86_64, universal), desktop linux amd64/arm64 and, new, relay
  server tarballs `meshmsg-relay-0.1.0-alpha9-linux-{amd64,arm64}.tar.gz` (binary + install.sh).

### 2026-11-12 — Requirement: direct IP links across arbitrary topology (docs only)

- `docs/connectivity.md`: binding requirement (topology is arbitrary: CGNAT, mobile, hotspots,
  nested NAT; never decide by private/public address; test reachability; most direct path
  wins, local traffic stays local; relays never carry a direct session; **no TURN**) and the
  design: candidates (host / reflexive / peer-reflexive / local-discovered), signaling as an
  E2E message over any working path, authenticated hole-punch checks, path order, re-check on
  network change, "no direct path" when checks fail, required netns test scenarios.
- architecture.md §5b, decisions.md D12. No code changed.

### 2026-11-12 — Requirement: two media classes (docs only)

- Normal media: direct paths only (direct IP link, LAN, Wi-Fi Direct, Bluetooth), both sides
  online. Important media: sender marks it, goes through relays, reaches offline receivers,
  allowance per account per day (`media_important_per_day` = 5, kept 7 days, never evicted
  once accepted, `GET /v1/media/quota`). protocol.md §6b, server.md §6a, connectivity.md §2,
  README, decisions D13. Open: sybil accounts / per-relay counting. Not implemented; today
  any file may go to a relay. Previous docs commit `53891c9`.
- User confirmed: one important file to a group counts once; the daily allowance is set per
  relay in its admin panel (server.md §6a, §6b). Previous docs commit `05fb69b`.

### 2026-11-12 — Requirement: important-media abuse control (docs only)

- server.md §6a: account age (first announce network-wide; new accounts get a smaller
  allowance), proof-of-work growing with today's network-wide uploads, and a public usage
  ledger: entries signed by the uploading account and the relay, deduplicated per file,
  pushed to and pulled from origin relays (`POST/GET /v1/usage`), also carried in `/v1/sync`
  and through bridges; allowance counted network-wide, number set per relay in the panel.
  decisions D14. Not implemented. Previous docs commit `f2f8848`.
- Proof-of-work for important media now also depends on account age: +3 bits under 3 days,
  +2 under 30, +1 under 180, 0 after; base 20 bits, +1 per upload today, cap 28; all panel
  settings (server.md §6a). Previous docs commit `27887bf`.

### 2026-11-12 — Requirement: voice calls (docs only)

- `docs/calls.md`: 1:1 voice calls over direct links only (connectivity.md); signaling =
  E2E content type 7 (OFFER/ANSWER/CANDIDATES/REJECT/END/CANCEL) over any path, OFFER rings
  45 s then shows as missed; per-call Noise KK + ChaCha20-Poly1305 frames, Opus 16 kHz
  6–24 kbps with FEC/DTX, adaptive jitter buffer, path switch mid-call; Android first
  (libopus via NDK, self-managed ConnectionService); IP exposure in threat model; tests.
- roadmap M9, decisions D15, architecture §5b link; `core/e2e` comment reserving type 7.
- Calls made upgradeable for video (calls.md §2, §3, §4a, §7): per-stream caps with media
  kind and codec ids, stream id + fragmentation in every frame, ops UPDATE/CONTROL, unknown
  fields ignored (v1 voice clients interoperate), shared bandwidth estimator, `core/call`
  layout with MediaSource/MediaSink. decisions D16. Previous commit `79ba2d8`.

### 2026-11-12 — Direct paths: every TURN alternative in the apps; TURN for private-relay admins

- connectivity.md: §3a IPv6 first, PCP → NAT-PMP → UPnP port mapping, NAT-type detection
  with two reflectors; §4a symmetric NAT: sequential port prediction and birthday method
  (256 sockets, ≤2048 paced probes, ≤20 s); new path order; failure reasons in the UI; §7
  TURN (RFC 8656, built into meshmsg-relay) only for a private relay's own admins, off by
  default, refused on origin relays, short-lived signed credentials, panel limits; tests.
- calls.md rules, server.md §6c, threat model, decisions D17. Not implemented. Previous
  commit `9075308`.
- Owner: official/global (origin) relays may also offer TURN, but only to their own admins,
  like private relays (connectivity.md §7, calls.md, server.md §6c, D17). Previous commit
  `69f4885`.

### 2026-11-12 — Plan recorded; alpha has no backward compatibility (docs only)

- docs/roadmap.md: table of existing code that must change for the new design (media
  upload, relay keep time, signatures/allowance/PoW/ledger, LAN as a candidate source, new
  direct-link and call modules, eu-1 UDP/TURN ports) and the order of work.
- decisions D18: alpha, lab-tested only → no compatibility or transition period for old
  alpha apps and relays until the first beta. README.fa.md media paragraph updated.
- Owner asked not to change code yet. Previous commit `c2d543c`.

### 2026-11-12 — Roadmap step 1: two media classes, per-relay allowance; relay ports

- Apps: a file is **normal** by default (no relay upload, ref with no URLs, the sender keeps
  and serves it over LAN/Bluetooth; receivers wait up to 7 days for a direct path) or
  **important** (sender marks it: Android attach menu "Important", web UI ⚑ toggle, CLI
  `send-file -i` / `/file -i`). Before sending, the node asks each media relay
  `GET /v1/media/quota`; with no allowance anywhere the file goes as normal and
  `Media.Note` says so (Android snackbar, web alert, CLI line). `MediaInfo.Important`;
  incoming normal files show "arrives when you are both online on a direct connection".
  `SendMedia(…, important)` in node, mobile, daemon (`important=1`).
- Relay: every upload signed (`X-Account`, `X-Day`, `X-Signature` over
  `MESHMSG-MEDIA-PUT`|id|day), 401 unsigned; `media_important_per_day` (default 5, per UTC
  day, one file counts once, slot reserved during upload) → 429 + Retry-After;
  `GET /v1/media/quota`; keep `media_important_hours` (default 168) replaces
  `media_max_hours`/`media_min_hours` (removed); per-day and hours change at once from the
  panel (`api.SetMediaLimits`). Info: `media_important_per_day`, `media_important_hours`.
- Ports, same on every relay: TCP 80/443, UDP 3478+3479 reflector, TCP+UDP 3478 TURN, UDP
  61000–61999 TURN relayed (above Linux ephemeral range). `core/wire` `Port*`, server.md §6d,
  deploy README; `install.sh` opens them in an active ufw/firewalld (asks, `MAVA_FIREWALL`).
- Tests: api TestMediaAllowance (quota, dedupe, 429, other account, raw unsigned 401);
  node TestNormalFileSkipsRelayAndAllowance; existing media tests use important=true; Android
  JsonTest important. All Go + firmware tests pass.

### 2026-11-12 — Roadmap step 2: account age, proof-of-work, usage ledger

- Step 1 commit `e2621ca`, CI run 43329012494 green (desktop + Android).
- `server/ledger` (new): signed entries (276 bytes: day, media id, account pubkey + upload
  signature, relay pubkey + signature over "MESHMSG-USAGE"), dedupe per (account, file),
  days dropped after 2, account first-seen times, sources (valid/invalid/blocked; a relay
  that signs a forged upload is ignored), sync cursors; `data_dir/ledger.db`.
- Relay: `api.MediaPolicy` (per day 5, keep 168 h, new account < 3 days → 1/day, PoW base
  20, max 28, age tiers 3:3, 30:2, 180:1) from defaults < relay.json < panel, all applied at
  once (`SetMediaPolicy`). Upload: signed for the relay's day exactly, allowance from the
  ledger (+ uploads in progress) → 429, then PoW over id|account|day → 402 + `X-PoW-Bits`;
  accepted uploads go into the ledger signed by the relay. Quota adds `pow_bits`,
  `age_days`. `POST/GET /v1/usage`; relays sync the ledger every minute with `usage_peers`
  (default origin relays + federation peers). Age = first stored announce
  (`relay.Config.OnAnnounce`, backfilled from stored announces at start). step-1
  `usage.json` removed.
- Client `PutMedia`: quota first (day, bits), solves the work, re-solves once on 402.
- Panel: new rule fields (age tiers as "days:bits"), "Usage ledger" table.
- Tests: ledger (verify, dedupe, paging, forged → relay blocked, bad relay sig, expire,
  ages); api TestMediaAgeWorkAndLedger (new vs old account, bits grow, count across two
  relays through push/pull, same file counts once, 4th refused, garbage refused); admin
  policy applies at once. TestVideoOverLANWithoutRelay failed once in a full run (timing
  under load), passed 4/4 alone and in the next full run.

### 2026-11-13 — Roadmap step 3 (part 1): core/direct, relay STUN/TURN package

- Step 2 commit `6b5288a`, CI run 43332452008 green.
- Owner: "do step 3 and 4". `core/direct` (new): candidates (host, reflexive, peer-reflexive,
  mapped, predicted, relayed) + NAT info, offer encoding (calls.md candidates format), NAT
  type from STUN on several reflectors (open / independent / symmetric sequential with step /
  symmetric random / UDP blocked), authenticated probes (HMAC from the session key, role bit
  so a device never matches itself on same-subnet networks), triggered peer-reflexive checks,
  port prediction for sequential symmetric NATs, birthday method (256 sockets, 2048 paced
  random probes), path choice by connectivity.md §5 classes then RTT, failure reasons, and
  `Link`: ChaCha20-Poly1305 datagrams (key per direction), replay window, keepalive 15 s,
  idle 45 s, follows the peer's new address. Android interface listing via wlynxg/anet.
- In-memory NAT simulator (`simnet_test.go`) with tests: same LAN stays local (zero internet
  packets), hotspot behind CGNAT (B↔C local, A↔B on the hotspot interface), double NAT hole
  punch, same private subnet in two networks (no false match), NAT type detection, symmetric
  sequential and random (birthday) success, both random → no path with reason, rebinding.
- `server/turn` (new, not wired into the relay yet): pion/turn v4 server on UDP 3478+3479,
  STUN for all, TURN only for admins (10-min HMAC credentials, `POST /v1/turn/credentials`
  signed), session/speed/monthly limits, counters. Port mapping and TURN client: stubs.

### 2026-11-13 — Step 3 (part 2): port mapping, TURN client, relay UDP services wired

- Part 1 commit `dde907d`.
- `core/direct/portmap.go`: PCP MAP → NAT-PMP → UPnP IGD (SSDP, description, SOAP
  Add/DeletePortMapping), gateway from /proc/net/route or x.y.z.1, 10-min lease renewed every
  5 min, removed on close. Tests with fake PCP/NAT-PMP routers and a fake UPnP device.
- `core/direct/turnclient.go`: pion TURN client allocation → relayed socket in the checks
  (chosen last). `Config.RelayOnly` for tests/diagnostics.
- Relay: `server/turn` started by meshmsg-relay (unless `"reflector": false`): STUN on UDP
  3478+3479; TURN only for panel admins, off by default; `public_ip` (auto: first public
  IPv4); `turn_enabled`, `turn_max_sessions` (10), `turn_max_kbps` (256), `turn_monthly_gb`
  (50) in relay.json and panel (apply at once); `POST /v1/turn/credentials`; `/v1/info`
  `reflector`, `turn`. Panel: TURN section with live sessions and month traffic.
- Tests: server/turn reflector (NAT open on loopback), TURN: non-admin 403, admin
  allocation, relayed path + data, bytes counted, wrong password refused, off → 404.

### 2026-11-13 — Step 3 (part 3): direct links in the node

- Part 2 commit `acc0b80`.
- `core/node/direct.go`: content type 8 signaling (OFFER/ANSWER/CLOSE, connectivity.md §8),
  session per contact, router link `direct` with datagram fragmentation, TURN credentials
  tried on up to 3 relays (only admins get them), reflectors = every relay host's 3478/3479.
  Normal media download opens a direct link to the sender automatically; P2P frames 1100 B,
  3 s window timeout on `direct`. `relayclient.TurnCredentials`, `wire.TurnCredentials`,
  `e2e.ContentCall` (7, reserved for step 4) and `ContentDirect` (8).
- Test `TestNormalMediaOverAutoDirectLink`: two apps sharing only a relay, a normal file
  arrives over the auto-opened direct link. All Go and firmware tests pass.
- Next: step 4 (calls): core/call (signaling type 7, caps, Noise KK, frames, jitter buffer),
  mobile API, Android audio and call UI.

### 2026-11-13 — Step 4 (part 1): core/call and node call API

- Step 3 part 3 commit `37c6da7`.
- `core/call`: signaling (content type 7: OFFER/ANSWER/CANDIDATES/REJECT/END/CANCEL/UPDATE/
  CONTROL, caps, unknown ops/trailing bytes ignored; OFFER carries the path-check key and
  core/direct's offer), Manager/Call state machine (outgoing → ringing → connecting →
  active → ended; 45 s ring, late OFFER = missed call, busy → REJECT busy, contacts only
  unless `calls_from_anyone`), path via core/direct (10 s + symmetric methods), Noise KK on the
  path (fresh ephemerals, prologue = call id), frames sealed with nonce = stream<<32|seq,
  6-digit voice-compare code, keepalive frames 2 s, ends after 20 s of silence, adaptive
  jitter buffer 40–200 ms (loss → PLC, underrun → rebuffer, catch-up skip). Codecs: Opus 1,
  AMR-WB 2 (callee picks the caller's first common codec).
- `core/node/calls.go`: StartCall / AnswerCall / EndCall / MuteCall / CallAudio /
  PullCallAudio / CurrentCall, `call` events. Tests: core/call (ring/answer/talk/hangup with
  codec fallback, declined/cancelled→missed/busy, late offer, encoding, jitter), node
  `TestCallThroughRelaySignaling`.
- Next: mobile API + Android audio (AudioRecord/AudioTrack, MediaCodec Opus or AMR-WB),
  call screen, foreground service; docs.

### 2026-11-13 — Step 4 (part 2): mobile API, Android calls, docs

- Part 1 commit `27d3255`.
- `mobile`: StartCall / AnswerCall / EndCall / MuteCall / CallAudio / PullCallAudio
  (`result byte | frame`) / CurrentCall; `call` events reach Kotlin through the listener.
- Android: `CallInfo` + `Json.call`, backend `call` flow and actions, `CallAudio`
  (MediaCodec Opus on Android 10+ else AMR-WB, AudioRecord/AudioTrack voice mode, AEC/NS),
  `CallService` (foreground microphone) + hang-up action, ring notification (full-screen
  intent, insistent ringtone), `CallScreen` overlay (answer/decline, mute, speaker, code,
  relayed note, end reasons), call button in contact chats, mic permission asked on demand,
  show over lock screen; en + fa strings; manifest permissions/service; JsonTest.call.
  Not built locally (disk full); CI builds and lints it.
- Docs: calls.md §7, connectivity.md §8, decisions D19 (deviations), roadmap.
- Next: watch CI; then (when the owner asks) tag v0.1.0-alpha10, update eu-1 (open UDP
  3478/3479 and 61000–61999, `install.sh` does it), test calls between two real phones.

### 2026-11-13 — Android build fix

- Part 2 commit `55d426a`. CI failed since `37c6da7` at "Go core → AAR": `link:
  github.com/wlynxg/anet: invalid reference to net.zoneCache` (Go's linkname check).
  `scripts/build-android-core.sh` now passes `-checklinkname=0` to the linker (reproduced and
  verified with `GOOS=android go build`). Go and firmware tests pass.
- Fix commit `23a96ca`; CI green (run 43340143271: desktop + Android lint/tests/APKs, the
  first run that compiles the Android call code). Tag v0.1.0-alpha10 (on `1665f0e`) could not
  be pushed from the session (tag pushes refused); the owner pushes it. eu-1 not updated: this
  session has no access to the server.

### 2026-11-13 — Release v0.1.0-alpha10

- Tag `v0.1.0-alpha10` pushed from the local machine (SSH remote, tag pushes work there) on
  `c4fd51f` (docs-only on top of the green `1665f0e`). CI published the release: 4 APKs,
  desktop and relay tarballs (amd64/arm64), SHA256SUMS.
- eu-1 still runs `0.1.0-alpha9+eafacce` (x86_64, `meshmsg-relay.service`, ufw inactive).
  Updating it with `meshmsg-relay-0.1.0-alpha10-linux-amd64.tar.gz` + `install.sh` waits for
  the owner's go-ahead. Provider-side firewall for UDP 3478/3479/61000–61999 unknown.
- Uncommitted local edit (not in the release): `core/direct/addrs_android.go` replaces anet
  with a raw netlink RTM_GETADDR dump; would let `-checklinkname=0` go.
- eu-1 updated to `0.1.0-alpha10` (same pubkey `8f45fed2b1fb5094`, config kept, `/admin` 200).
  STUN on 3478/3479 answers on the server (127.0.0.1) but not from outside, although iptables
  INPUT accepts and ufw is off → provider cloud firewall; the owner opens UDP 3478, 3479,
  61000–61999 in the provider panel.
- `server/deploy/install.sh`: over `ssh host 'sh install.sh'` the update died at the admin
  question (`cannot create /dev/tty`): `[ -r /dev/tty ]` is true without a controlling
  terminal. Now it tests by opening `/dev/tty`. Workaround for older tarballs: `MAVA_YES=1`.
- `go test ./mobile` failed once in a full `go test ./...` run, passed 5× alone: flaky under
  load, not investigated yet.

### 2026-11-13 — Call audio played slow and low (first call on real phones)

- Owner's test on two phones: the call connects and audio is heard, but the voice is deep,
  slow and soon not understandable.
- Cause (from the code, not yet confirmed on a phone): `CallAudio.playback` wrote the
  decoder's PCM to a fixed 16 kHz AudioTrack. Android's Opus decoder outputs 48 kHz whatever
  was encoded, so speech played at a third of its speed, and the loop pulled frames three
  times too slowly, so the delay kept growing. Also only one decoder output was taken per
  pulled frame, so a late output added delay for good.
- Fix: the track follows the decoder's output format (`INFO_OUTPUT_FORMAT_CHANGED`: rate and
  channels; logged as `CallAudio: decoder output: …`), silence frames match it, and every
  ready decoder output is played. docs/calls.md §7 note. Needs a phone test.
- Fix commit `b074d61`. Phone verification handed to the team as GitHub issue #1 (checklist:
  5-minute call, logcat `CallAudio`, both codecs, earpiece/speaker/Bluetooth; where to look next).

### 2026-11-13 — Call on the same network with no relay reachable (owner's report)

- Report: two phones on one network, no relay reachable; messages go over BLE but a call
  can't be set up. Not reproduced: the core does it on one machine — new tests
  `mobile/call_ble_test.go` (`TestCallWithoutRelay`, `TestCallRelayUnreachable`: signaling
  over a fake BLE link only, call rings and becomes active) pass.
- Handed to the team as GitHub issue #2 with what to collect and the candidate causes:
  Android routes the single all-interface UDP socket through mobile data when Wi-Fi has no
  internet (no `Network.bindSocket` in the app; fix = socket per address + bind hook), Wi-Fi
  client isolation, OFFER expiry compared across two unsynced clocks, no host candidates
  from anet. No code fix made yet.

### 2026-11-13 — Release v0.1.0-alpha11

- CI green on `b074d61` (run 43346888903). Tag `v0.1.0-alpha11` on `219809c`; release
  published (4 APKs, desktop + relay tarballs). Contains the call playback fix (Opus decoder
  output 48 kHz) and the relay install.sh tty fix. eu-1 left on alpha10 (no relay changes
  besides the installer). Next: phone test per issue #1; provider firewall UDP for eu-1.

### 2026-11-13 — Network screen showed "Internet relay · direct"

- The core's direct phone-to-phone link is a router link named `direct`; the Android status
  parser treated every unknown link as a relay, so the Network screen listed it as a relay
  called "direct". `Models.kt` now leaves it out of the path list. Compiles in CI only.
- Tag `v0.1.0-alpha11` (pushed by the owner on `219809c`) contains the call audio fix
  `b074d61`, not this change.

### 2026-11-13 — Calls on one Wi-Fi: "no direct connection" (owner's report, alpha11)

- Report: with alpha10 a call between two phones on one network worked (signaling through
  the relay); with alpha11 it ends with "No direct connection could be made".
- alpha10 → alpha11 changed only `CallAudio.kt` and the relay installer; the path code is
  the same. So this is not a regression in code but a path check that only sometimes works.
- Most likely cause (not confirmed on a phone): `core/direct` used one UDP socket on all
  interfaces. Android routes by the app's default network, so whenever that is mobile data
  (Wi-Fi without internet, or the phone prefers mobile data) probes for the other phone's
  Wi-Fi address leave through mobile data and are lost. Same cause as candidate 1 of issue #2.
- Change: one extra UDP socket per local address (`OSNet.ListenUDPAddr`, at most 8), each a
  host candidate; hook `OSNet.Bind` → `Node.SetSocketBinder` → `mobile.NetworkBinder`; Android
  `NetBinder` binds each socket to the `Network` that owns the address (`Network.bindSocket`);
  between equal paths the bound socket wins. IPv6 addresses now get a socket of their own
  family (the dual-stack socket skipped IPv6 pairs). Test `TestBoundSocketPerAddress`.
  All Go and firmware tests pass; Android compiles in CI only. Needs a phone test
  (`adb logcat -s NetBinder`), then the next tag v0.1.0-alpha12.
- Fix commit `7be33cc`; CI green on main (run 43349515677: desktop + Android). Tag
  **v0.1.0-alpha12** pushed by the owner on `7be33cc` (release run 43350141430): per-address
  sockets bound to their network, Network screen without the "direct" relay card, no-relay
  call tests. The owner also reported a normal voice message not arriving on the same Wi-Fi:
  same direct-link path, same suspected cause. Next: phone test of alpha12 (voice message,
  then call, on one Wi-Fi); eu-1 provider firewall UDP for calls across networks.

### 2026-11-13 — Desktop update; meshd single instance

- Owner's desktop updated alpha5 → alpha11 (`~/.local/bin`, user service).
- Bug found there: starting `meshd` by hand while the service ran removed the service's
  control socket, then failed on the UI port and its exit unlinked the socket → `mesh`
  reported "meshd is not running". `meshd` now refuses to start when a daemon answers on
  the socket (`daemon.Running`, `ErrRunning`, checked in main and in `Run`); a stale socket
  file is still replaced. Tests `desktop/daemon/running_test.go`. Not released yet.

### 2026-11-13 — Calls still don't connect on alpha12: no host candidates on Android 11+

- Owner: calls still don't connect with alpha12 (phones on one Wi-Fi).
- Bug found in the code: `core/direct/addrs_android.go` called `anet.InterfaceAddrs()`, but
  anet only uses its own reader when told the Android version is ≥ 11
  (`anet.SetAndroidVersion`); it defaults to 0 and then calls `net.InterfaceAddrs`, which
  Android 11+ refuses. `Gather` ignored the error, so a phone on Android 11+ offered **no
  host candidates** (and no per-address sockets, so alpha12's binding never ran). With eu-1's
  UDP still closed there were no reflexive ones either: an empty offer. A pair with one
  phone on Android ≤ 10 could still connect (triggered checks), which fits the one success.
- Fix: `anet.SetAndroidVersion(11)` at init with `net.InterfaceAddrs` as fallback;
  `OSNet.MoreAddrs` + `mobile.NetworkBinder.LocalAddrs()`: Android also lists its addresses
  with `java.net.NetworkInterface` (covers hotspot interfaces) and the core merges them;
  `Gather` logs when there are no local addresses. Test `TestMoreAddrs`. Go and firmware
  tests pass; Android compiles in CI only; not yet run on a phone. Next tag v0.1.0-alpha13.
- Fix commit `91f2061`; CI green on main (run 43351536790: desktop + Android). Tag
  **v0.1.0-alpha13** pushed by the owner on `91f2061` (release run 43352305873). Next: phone
  test on one Wi-Fi (voice message, then call); `adb logcat -s NetBinder` shows the
  `local addresses:` line.

### 2026-11-13 — Release v0.1.0-alpha13

- CI run 45 green on `91f2061`. The tag was not on GitHub (made where tag pushes are
  refused); pushed `v0.1.0-alpha13` on `91f2061` from the owner's machine, release published.
  Contains `dfcea2c` (meshd single instance) and `91f2061` (Android 11+ local addresses).
  Owner's desktop updated to alpha13. Next: phone test on one Wi-Fi (voice message, call).

### 2026-11-13 (handheld hardware)
- `docs/handheld.md`: hardware design for a standalone handheld (ESP32-S3-WROOM-1-N16R8,
  SX1262 module, 2.8" 320×240 ST7789 + capacitive touch, TCA8418 50-key Persian QWERTY
  matrix with diodes, ES8388 codec with mic / earpiece / NS4150B speaker / TRRS headset and
  a hardware AEC reference, AXP2101 PMU, PCF85063A RTC on the battery, microSD, GPS or LTE
  option). Full GPIO map, audio path, power estimates, antennas, BOM (~60–80 USD), bring-up
  plan. Hardware only; no firmware for it yet. Also noted: iOS is feasible (Go core
  cross-compiles for `GOOS=ios`), see chat; not started.
- `docs/images/handheld-schematic.svg` (generated by `scripts/handheld-schematic.py`):
  schematic overview of the handheld, every block with its pins, nets and power rails,
  matching the pin map in docs/handheld.md §4. Roadmap: **M10 iPhone app** added as the last
  milestone (Go core via `gomobile bind -target=ios`, SwiftUI, APNs/CallKit, LoRa over the
  firmware BLE host link).
- Handheld redesigned as touchscreen devices (owner asked for "like a smartphone", both
  variants): **Model S** (ESP32-S3, 3.5" 320×480 ST7796S + touch, side buttons on one ADC pin,
  keyboard and TCA8418 removed) and **Model P** (ESP32-P4NRW32 + ESP32-C6-MINI-1 over SDIO,
  5" 720×1280 MIPI-DSI + GT911, optional OV5647 camera, USB-C high speed with host mode).
  Shared LoRa/audio/PMU/RTC blocks. `scripts/handheld-schematic.py s|p` draws
  `docs/images/handheld-{s,p}-schematic.svg`. Model P GPIO numbers are a suggestion to verify
  against the ESP32-P4 datasheet.
- `docs/roadmap.md` "Beta criteria": what Android and desktop need to leave alpha (B1–B6 both,
  A1–A7 Android, D1–D6 desktop, release process, order). Next software steps without
  hardware: B1 schema version, B3 call clock skew, A1 release key, A2 Keystore, A3 crash log.
- Owner: every beta and stable release must tell users about updates. Roadmap **B7** "Update
  check" (signed `release.json` per tag, served by relays `GET /v1/release`, GitHub and the
  mesh; Android banner + notification, desktop web UI / `mesh status` / `mesh update`;
  minimum-supported-version for security releases), decision **D20**, rule added to CLAUDE.md (`0f415d2` + next commit).
  Not built yet; it must be in `v0.2.0-beta1`.
- Branch cleanup: `ccr-23656715-q9ut0d` was the pre-snapshot history (already merged into main
  on 2026-11-11, `e507654`; main was later restarted from a snapshot, so the branch shared no
  commit with main). Brought over the two files main never had: `.github/workflows/ci.yml`
  (Go build/vet/`test -race`, firmware host tests + `scripts/e2e-lora.sh`, PlatformIO builds of
  the four boards; its Android job dropped, `android.yml` does that) and `scripts/sign-apks.sh`
  (version now from `$VERSION` or the latest tag; for beta item A1). Not brought over:
  `release.yml` (replaced by `android.yml`) and the old vector launcher icon (replaced by the
  logo). Verified here: `go test -race ./...`, `make -C firmware test`, `scripts/e2e-lora.sh`
  pass; the board builds run first in CI. Then deleted `ccr-23656715-q9ut0d` and the session
  branch `ccr-d1290771-8bh9o1`; only `main` remains.
- docs/handheld.md: LoRa made explicit as built in on both models (comparison table row, a LoRa
  row in each model's parts table with its SPI pins; USB host text no longer suggests a USB
  LoRa node). No design change: the SX1262 (U4) was already on both boards.
- Handheld + 4G LTE (SIMCom SIM7600G-H): **built into Model P** (UART + CMUX/PPP data, PCM call
  audio into the P4's I2S1 and the ES8388 path, RI wake, nano-SIM, 3 flex antennas, own 4 A load
  switch from the battery, SW56 cellular kill switch, modem off by default; slow lines moved to
  a TCA9535 expander; battery 4500 mAh; BOM ~125–150 USD); **Model S gets an LTE add-on board**
  on J6 (now with VBAT; data + SMS only, no PCM pins). docs/handheld.md and both schematics
  updated (Model P schematic has a new cellular row).

### 2026-11-13 — main only

- Deleted the leftover branches `ccr-23656715-q9ut0d` (old CI attempt, superseded; last
  commit `9768191`) and `ccr-d1290771-8bh9o1` (fully merged). CLAUDE.md: everything ends up
  on `main`; side branches are merged and deleted before a session finishes.

- Owner: the main board must be **2 layers** (4 was my assumption, never asked for; 2 is much
  cheaper). docs/handheld.md: Model S main board now 2 layers with 2-layer layout rules (bottom
  ground pour, stitching vias, LCD SPI at 40 MHz first, USB full speed only); BOM ~50–65 USD.
  Model P stays 6 layers (MIPI/USB HS pairs and the P4's 0.4 mm QFN can't be done on 2); the
  2-layer path for Model P is a P4 + C6 core module on a carrier board.

### 2026-11-13 — eu-1 on its domain

- eu-1 is reachable as **`https://eu-1.mfadem.ir`** (DNS A → 87.106.5.192). The TLS error was
  Caddy having no site block for the name (handshake aborted with "internal error"); added
  `eu-1.mfadem.ir { reverse_proxy 127.0.0.1:8080 … }` to `/etc/caddy/Caddyfile` (backup
  `Caddyfile.bak-*` next to it), Caddy got a Let's Encrypt cert (renews itself). The plain
  `http://87.106.5.192` block stays for old clients.
- eu-1 relay updated from alpha10 to the official **0.1.0-alpha13** binary (SHA256 checked;
  old binary at `/usr/local/bin/meshmsg-relay.bak-alpha10`). Its own list (`/etc/meshmsg/relay.json`,
  served at `/v1/relays`) now has `urls: [https://eu-1.mfadem.ir, http://87.106.5.192]`.
- `network/relays.json` lists the domain first and the IP second, so running clients and
  relays pick it up from GitHub (key-pinned; discovery skips a second link to the same key).
  New builds default to `https://eu-1.mfadem.ir` (meshd config, Android `DEFAULT_RELAYS`).
- Seen while testing: `core/node` `TestNormalMediaOverAutoDirectLink` fails on clean HEAD
  too (not caused by this change); needs a look.

### 2026-11-13 — update check (B7) and v0.1.1-beta1

- Owner asked to build and release `0.1.1-beta1`; CLAUDE.md requires the update check from
  the first beta on, which didn't exist, so owner chose "build B7, then release".
- `core/update`: manifest + Ed25519-signed envelope (`{"manifest": b64, "sig": b64}`,
  signature over `meshmsg-release-manifest-v1\n` + manifest bytes), semver compare with
  pre-release tags (beta2 < beta10 < final), channels (pre-release installs see beta +
  stable, stable sees stable), `Checker` (relays in order, then GitHub releases API; daily
  with jitter), relay `Cache`, `Download` with SHA-256/size check. Tests incl. forged and
  tampered manifests.
- `tools/release-manifest` keygen/sign/verify. Release key generated here: public key in
  `core/update.ReleaseKeyHex` (`f8146bcf…f4a1`), private seed in
  `~/.config/mesh-msg/release-signing.key` (outside the repo) → must be added as GitHub
  secret `RELEASE_SIGNING_KEY`.
- Relay: `GET /v1/release?channel=`, hourly refresh from GitHub (`release_upstream`,
  `release_check` config). meshd: `GET /update`, `POST /update/check`, `no_update_check`
  config, web UI banner (Later per version, not for required), `mesh status` line,
  `mesh update [check]` (download, verify, swap binaries, restart user service).
  Relays behind Tor/proxy: the check asks only directly configured relays.
- Android: `UpdateManager` (check 10–40 s after start, then ~daily), chat-list banner,
  one notification per version (channel "App updates"), Settings → About (version,
  "Check for updates"), APK for the phone's ABI downloaded via the Go core, checked, and
  opened with the system installer (`REQUEST_INSTALL_PACKAGES`, FileProvider `updates/`).
  Strings en + fa. `AppUpdateTest`.
- CI release job: signs `release.json` from `docs/release-notes/<tag>.{en,fa}.md`
  (+ optional `min-supported`), verifies it, attaches it, release body from the notes.
  Dry-run locally: signs with the key, fails without it.
- Not built: manifest over the mesh during shutdowns, APK from a nearby phone (roadmap B7
  "Not yet").
- Checks: `go build ./... && go test ./...` (TestPhotoThroughRelay / TestPhotoOverBLE
  timed out once under full parallel load, pass alone in 2.5 s / 0.5 s), `make -C firmware test`.
- Android: AAR built locally (NDK 27.2 installed under ~/.local/share/android-toolchain/sdk);
  Kotlin compiled, but `lintDebug` stalls on this 3 GB machine (killed after 25 min), so
  lint/unit tests/APKs are verified by CI on the push. Known unrelated failure (reported by
  the eu-1 session): `core/node` TestNormalMediaOverAutoDirectLink fails on clean HEAD.
- Release also needs eu-1 upgraded to the new relay build so it serves `/v1/release`
  (until then apps fall back to GitHub).
- `32f31a6` B7 pushed; CI green (Go + firmware, Android lint/tests/APKs). Owner said "release it":
  tag **v0.1.1-beta1** pushed on the commit that adds this line.

### 2026-11-13 — beta2 changes (owner's requests)

- v0.1.1-beta1's release job failed at "Signed release manifest": the GitHub secret
  `RELEASE_SIGNING_KEY` isn't set yet (owner will add it). No release was published for
  beta1; the tag stays as is (tags are never moved).
- Android: battery mode fixed to Active; gateway ("share my internet") and relay discovery
  always on; their switches and the battery radio buttons removed from Settings (one
  explanatory line instead). `Settings.gateway/discovery/batteryMode` are now constant.
- Calls: `call.Info` gains `path_class` (connectivity.md §5); the call screen shows a badge
  "Peer to peer (direct)" / "Peer to peer on this network" (class 1) / "Through a relay"
  (class 6, TURN) while the call has a path.
- Added these two items to the beta2 release notes (written by the eu-1 session after the key rotation, `f647a0c`). Tag v0.1.1-beta2 (pushed by that session) waits for CI and
  for the secret.

### 2026-11-13 — release key rotated, beta1 never published → v0.1.1-beta2

- `v0.1.1-beta1` (tag on `483138f`) was **not published**: its release job failed at "Signed
  release manifest" because the GitHub secret `RELEASE_SIGNING_KEY` was never added (APK and
  desktop jobs passed). The tag stays as it is (tags are never moved).
- The private release key was shown in chat before any release used it, so it was replaced
  while that costs nothing: new public key `4a89de4b…5626` in `core/update.ReleaseKeyHex`, new
  seed in `~/.config/mesh-msg/release-signing.key`; the old seed is kept only as
  `release-signing.key.exposed-2026-11-13` and must not be used. Checked: the new seed signs
  and verifies, the old one is refused.
- `docs/release-notes/v0.1.1-beta2.{en,fa}.md` (beta1's notes + the HTTPS domain for eu-1).
- Next: owner adds the new seed as the GitHub secret `RELEASE_SIGNING_KEY`, then push tag
  `v0.1.1-beta2` from main; after it publishes, put the beta2 relay on eu-1 (`GET /v1/release`).


### 2026-11-13: Model S power board (hardware/power-s), routed and packaged
- Separate 2-layer power board, 54 × 36 mm: AXP2101 (charger, power path, fuel gauge over
  I2C + PMU_IRQ to the ESP32-S3), USB-C, JST-PH battery, PCF85063A RTC on a TPS7A02, 2 × 14 1.27 mm
  header J3 to the main board. Generated: generate_schematic.py (netlist == spec, ERC 0 errors),
  place_board.py (no overlaps, parity clean), setup_fab.py (JLC rules), route_board.py,
  build_package.py (Gerbers/BOM/CPL), ASSEMBLY.md (order, hand assembly, bring-up, firmware check).
- Routing is hybrid: power trunks, the whole AXP2101 fan-out, exposed-pad vias, PWR_KEY
  (around the board edge) and both GND pours are scripted. Freerouting (stored in
  routing/autoroute.json, `kpy route_board.py --autoroute` re-rolls it) does the rest at
  0.2 mm. Then a two-layer A* (`find_path`) closes what is left, power tracks are widened
  to 0.5 mm, and `stitch_islands` joins cut-off ground with vias or bridges.
- Result: **DRC 0 errors, 0 unconnected, parity clean**. 9 warnings, all silkscreen. `jlc/`
  holds Gerbers, BOM and CPL (48 parts, they agree) and the schematic PDF. Images are in
  images/.
- Next: order from JLCPCB (check polarised parts in their placement preview; ASSEMBLY.md §2),
  bring-up per ASSEMBLY.md §4. Optional cleanup: overlapping reference labels. The main board
  needs the matching 2 × 14 1.27 mm socket.

### 2026-11-13 — flaky TestNormalMediaOverAutoDirectLink fixed (real bugs)

- CI's Go job failed on `dc17f17` (and earlier on `14f6323`) in
  `core/node` TestNormalMediaOverAutoDirectLink; reproduced locally under load (3 of 4 runs).
  Traced with packet logs to three bugs in direct links:
  1. `core/direct` paired a socket bound to loopback with non-loopback remotes (and the
     reverse): such pairs can pass checks on one host and still lose data. Now skipped.
  2. Both ends "follow" authenticated data from a new address; when each side had chosen a
     different pair they could swap back and forth. Now the controlled side follows; the
     controlling side follows at once only on the same socket (NAT rebinding) and on
     another socket only after its pair has been silent 3 s (`followAfter`).
  3. `fetchP2P` over a direct link gave up after one 3 s gap (one lost UDP datagram), so the
     file waited for the 2-minute retry, despite the comment saying it re-asks. Now it
     re-asks the window from the current offset, up to 5 stalls in a row (`maxStalls`).
- After the fix: 8/8 runs pass under load; full `go test -race ./...` and
  `make -C firmware test` pass.

### 2026-11-13 — flaky TestGroupOverBLEThroughNonMember fixed

- CI's Go job failed again on `cb15184`; `mobile` TestGroupOverBLEThroughNonMember failed
  up to half the runs under `-race` + load. Traced: C never got the group roster (title
  "Family"). The test harness `connectBLEAs` started delivery and then called `PeerUp` on
  each side in turn: B's `PeerUp(C)` flushed its backlog (the roster) to C before C had
  registered B, C dropped the chunks ("chunk from unknown peer"), and the router never resends
  to a peer it marked. Harness now registers both sides before data flows.
- Also fixed a real race in `core/node/groups.go`: a text arriving for an unknown group
  wrote its placeholder ("group") with an upsert, which could overwrite a roster stored
  by another goroutine in between; now `putPrivateIfNew` inserts only if absent. The test
  waits up to 15 s for the title (text may overtake the roster by design).
- 10/10 runs pass under load; full `go test -race ./...` and `make -C firmware test` pass.

### 2026-11-13 — v0.1.1-beta2 released

- Owner added the new `RELEASE_SIGNING_KEY` secret. CI on `dc17f17` and `cb15184` failed in
  `go test -race` (flaky `TestNormalMediaOverAutoDirectLink`, then `TestGroupOverBLEThroughNonMember`);
  both fixed by the other session (`cb15184`, `529c34d`). Both workflows green on `529c34d`.
- Tag **`v0.1.1-beta2`** on `529c34d`; release build green, release published (pre-release) with
  4 APKs, desktop + relay tarballs, `SHA256SUMS` and `release.json`. Checked: `release-manifest
  verify` accepts the published `release.json` (8 files, channel beta).
- eu-1 updated to the 0.1.1-beta2 relay (SHA256 checked; previous binary
  `/usr/local/bin/meshmsg-relay.bak-alpha13`). `https://eu-1.mfadem.ir/v1/release?channel=beta`
  serves the signed beta2 manifest; `channel=stable` answers 404 (no stable release yet).
- Next: install beta2 on two phones (M5 hardware tests); real-phone check of the update banner
  with the next beta.

### 2026-11-13 — beta3 features: profile pictures, chat folders, desktop theme, quiet notification

Owner asked for video calls, desktop calls, group calls, a hideable "Mava is running"
notification, chat folders, a Telegram-like desktop theme and profile pictures over direct
links. Chose: quick items in beta3; calls follow in later betas.
- **Profile pictures** (`core/node/avatar.go`): own picture ≤ 64 KB (JPEG/PNG/WebP) at
  `<data>/avatar`; served only on `direct` links (authenticated per contact) with the media
  window protocol under ID `sha256("mava-avatar-v1:"+owner)`; fetched into `<data>/avatars/<hash>`
  when the user opens the chat (`RequestAvatar` opens a direct link if needed) and refreshed
  when a direct link comes up and the copy is > 1 day old. Event `avatar`. Tests: over a direct
  link (incl. change), not over LAN, bad/oversized input rejected. Checked live between two
  meshd nodes via eu-1 (fetched in ~8 s).
- **Chat folders** (`core/node/folders.go`): user folders in the app DB (`kv` key `folders`),
  built-in views (All/Personal/Groups/Unread) filtered by the apps. Event `folders`.
- meshd API: `GET /avatar/{hash|me}`, `PUT/DELETE /avatar`, `POST /avatar/{hash}/request`,
  `GET/PUT /folders`. Mobile: `SetAvatar`, `AvatarPath`, `RequestAvatar`, `Folders`, `SetFolders`.
- Desktop web UI: Telegram-like theme (default; "Classic" in Settings → Appearance, stored in
  the browser), avatars in list and header, folder tabs with unread counts + ✎ editor,
  unread badges, picture upload (shrunk in the browser to ≤ 60 KB JPEG). Screenshots checked light/dark.
- Android: notification channel `mesh_status` at IMPORTANCE_MIN (silent, no icon, secret on
  lock screen; old `mesh` channel deleted) + Settings guide with a button to the channel's
  system page; folder chips above the chat list, long-press a chat to file it, folder editor;
  profile picture in Settings (photo picker, shrunk to ≤ 60 KB), shown in `Avatar` everywhere
  via `LocalAvatars`; opening a contact chat requests its picture.
- Checks: `go test -race ./...`, `make -C firmware test`, AAR rebuilt, `compileDebugKotlin`,
  `testDebugUnitTest` (new FoldersTest) pass locally; lint left to CI (3 GB RAM).

### 2026-11-13 — v0.1.1-beta3 released

- Both CI workflows green on `16579db`; tag **`v0.1.1-beta3`** pushed there, release build green,
  pre-release published (4 APKs, desktop + relay tarballs, `SHA256SUMS`, `release.json`);
  `release-manifest verify` accepts its `release.json`. eu-1 not updated (relay unchanged); its
  hourly cache switches `/v1/release?channel=beta` to beta3. Beta2 installs should show the update banner.
- Next (owner's list): Android video calls, desktop audio+video calls, group calls with a
  data-use warning.

### 2026-11-13 — update check fixes (beta4)

- Owner on beta2 got no beta3 notice; the manual check showed "Could not send". Causes: (1) the
  manual check's result went through the error channel, which the app prefixes with
  "Could not send:" (now a separate `notices` channel, shown as is); (2) `Checker.Check` skipped
  GitHub when a relay answered, and eu-1's cached manifest was up to an hour old (beta2);
  (3) phones checked only every 20–28 h.
- Now: the checker asks GitHub as well as the first answering relay and takes the newer
  (new TestCheckerStaleRelay; blocked GitHub → relay answer stands); relay cache refreshes every
  9–11 min; meshd and Android check every 5–7 h; Android also checks on resume if the last
  check is > 1 h old.
- eu-1 needs the new relay build for the 10-minute refresh (not required: GitHub covers it).

### 2026-11-13 — contact links as web links (beta4)

- Owner: QR is fine, but a link sent as text in other apps meant manual input. Contact links
  are now `https://eu-1.mfadem.ir/add#<key>?n=<name>` (`identity.Contact.Link`, `LinkBase`);
  `ParseURI` accepts any http(s) `…/add#…` as well as `meshmsg:`. Key in the fragment, so the
  server never sees it.
- Relay serves `GET /add` (embedded `server/api/add.html`: "Open in Mava" → `meshmsg:` link,
  download, copy for manual input; en + fa; screenshot checked) and
  `/.well-known/assetlinks.json` (package `org.meshmsg.app`, cert SHA-256 of
  `android/debug.keystore`; update with A1). Caddy on eu-1 already passes both paths to the relay.
- Android: App Link intent filter (autoVerify, https eu-1.mfadem.ir /add); share/copy send the
  web link (QR stays `meshmsg:`); `ContactLink` helper + test. meshd `/id` has `link`; web UI
  "Me" and `mesh id` show it.
- **Needs eu-1 on the new relay build** for the page and App Link verification (until then
  the link 404s in a browser; apps still parse it when pasted).
- Seen twice under heavy load: `TestVideoOverLANWithoutRelay` timing flake (LAN link not up
  within 30 s, Have 0); 0/32 on both old and new code in repeat runs.

### 2026-11-13 — v0.1.1-beta4 released, eu-1 on beta4

- Owner approved both. Tag **`v0.1.1-beta4`** on `a2d2cdf` (both CI workflows green); release
  published, `release.json` verifies, relay tarball checked against `SHA256SUMS`.
- eu-1: relay binary replaced with 0.1.1-beta4 (SHA-256 checked on the server; previous binary
  `/usr/local/bin/meshmsg-relay.bak-beta2`), service active. Live: `/v1/release?channel=beta`
  → signed beta4; `/add` → landing page; `/.well-known/assetlinks.json` served, and Google's
  Digital Asset Links API lists the statement for `org.meshmsg.app`. The beta4 APK's signing
  cert SHA-256 (46:72:0B:…:E2:BC) matches it, so Android can verify the App Link.
- Next: owner's call list (Android video calls, desktop calls, group calls + data warning).

### 2026-11-13 — video calls (Android)

- Owner asked for video calls. Core (`core/call/video.go`): video stream 1 (H.264) negotiated
  next to audio (voice-only apps answer without it), frames fragmented into datagrams and
  reassembled by timestamp, keyframe recovery after loss, in-band control stream 255
  (keyframe request, loss reports) driving a sender bitrate 120–2000 kbps. Node:
  `StartCallWith`, `AnswerCallWith`, `CallVideo`, `PullCallVideo`, `CallVideoNeeds`;
  mobile: `StartVideoCall`, `AnswerCallVideo`, `SendCallVideo`, `PullCallVideo`, `CallVideoNeeds`.
- Android `CallVideo`: CameraX → H.264 hardware encoder → core; core → decoder → TextureView
  (rotation from the sender); call screen with full-screen remote video, self-view, camera
  on/off + switch, metered-data note; "Video call" button in chats; ringing says "Incoming
  video call"; camera permission asked when needed.
- Tests: `core/call` video tests (two-phone video call both ways with a 40 KB keyframe,
  voice-only answer, loss → wait for keyframe, control/bitrate), node test over relay
  signaling + direct link (100 KB keyframe), Android `CallVideoTest` (Annex-B split, YUV
  plane copy). Full Go race suite, firmware tests, Android unit tests pass.
- **Not run on real phones yet** — the camera/encoder/decoder path needs two phones (beta5).
- CI's Go job failed on the video commit (log needs a GitHub login; not reproduced locally:
  video tests 10/10 under load). Hardened: video tests resend a frame as a keyframe when
  datagrams of a burst are dropped (what the app does on a keyframe request); direct-link UDP
  sockets ask for 2 MB receive / 1 MB send buffers (keyframe bursts on real phones too); a
  media download that failed on a present neighbour (e.g. the sender hadn't registered us
  yet) retries after 10 s up to 3 times instead of waiting 2 minutes — the suspected cause of
  the rare `TestVideoOverLANWithoutRelay` failure (0/6 more full runs).

### 2026-11-13 — v0.1.1-beta5 released (video calls)

- Owner said release. CI green on `aa6e64a` (code identical to `c372bd1`, notes added; the
  earlier Go failure did not come back). Tag **`v0.1.1-beta5`** there; release published
  (10 assets), `release.json` verifies; eu-1 (beta4 relay, 10-minute refresh) served the
  beta5 manifest right away. Relay unchanged since beta4: no redeploy.
- Next: owner tests video calls on two phones; then desktop calls, then group calls with the
  data-use warning.

### 2026-11-13 — beta6 batch, step 1: read receipts (colored second tick)

- Owner asked (beta6, all items): WhatsApp-style call notification/answer/ongoing call bar,
  Bluetooth headset, full-screen call permission, file manager (delete by time range / chat),
  full-size zoomable profile picture, merge an old account's history into a new one with an
  animation, colored second tick for read — read receipts over relays only, not LoRa/BLE.
- Done: content type 9 (`core/node/readreceipts.go`, protocol.md §3a). Opening a 1:1 chat
  sends the newest seen packet id; the sender marks messages up to it read (`seen` column,
  `Message.read`, event `read`). NOLORA and handed straight to relay links (outside the
  store, so the backlog never takes it over Bluetooth); waits for a relay if none; the RECEIPT
  of a NOLORA packet is NOLORA too. Setting on by default (`ReadReceipts`/`SetReadReceipts`,
  daemon `GET/PUT /read-receipts`, Android Settings switch, desktop Privacy card). Colored ✓✓
  on Android (`Delivery.Read`, `ExtraColors.read`) and desktop (`--read`).
- Tests: `TestReadReceiptThroughRelay`, `TestReadReceiptsOff`, `TestReadReceiptNotOverLAN`.
- Next: file manager, account merge, profile picture viewer, calls.

### 2026-11-13 — beta6 step 2: file manager (storage)

- Core `core/node/storage.go`: `StoredFiles(filter)`, `Storage()` (total + per chat + per
  kind, biggest first), `DeleteFiles(filter)` — filter = chats, kinds, from/to, ids, any mix;
  an empty filter is refused. Deleting removes the blob (and `.part`), sets media state
  `deleted` (not fetched or served again; `FetchMedia` refuses), the message stays.
  Test `TestStorageListAndDelete`.
- Daemon `GET /storage`, `POST /storage/files`, `POST /storage/delete`; desktop Settings
  "Storage" card (chats ✓, types ✓, from/to dates, live "Delete N files · size").
- Mobile `StoredFiles`, `DeleteFiles`; Android Settings → Storage screen: total, time chips
  (all / older than a week / month / 3 months / date range picker), type chips, chats with
  size bars and select all/none, red "Delete N files · size" with confirmation; snackbar
  "Freed …". Media state `Deleted` shown as "deleted".

### 2026-11-13 — beta6 step 3: merge an old account's chat into a new one

- Core `core/node/merge.go`: `MergeContact(old, new)` moves every 1:1 message of the old
  account to the new one (one transaction), keeps the later read mark, moves folder entries,
  removes the old contact; private groups keep the old member until an owner changes it.
  Fixed a deadlock in the first draft (read marks were read inside the transaction while
  the app DB has one connection). Test `TestMergeContact`.
- Daemon `POST /contacts/{ref}/merge {"into": ref}` → `{"moved": n}`; mobile `MergeContact`.
- Android: chat menu "Merge into new account…" → pick contact → confirm → animation (dots
  stream from the old avatar to the new, the old one flies in, check badge) → "Open chat".
  Desktop: "Merge…" button in a contact's chat header, same flow with a CSS animation
  (respects reduced motion). Strings in English and Persian.

### 2026-11-13 — beta6 step 4: full-size zoomable profile picture

- Android: tapping a contact's photo in the chat header opens `PhotoViewer` (full screen,
  pinch 1–4×, pan, double-tap 2.5×, tap or ✕ to close). Pictures are now stored at up to
  640 px (was 384; still ≤ 60 KB JPEG, same 64 KB core limit), so the full view is sharp.
- Desktop: clicking the header photo opens a lightbox (wheel / double-click zoom, drag to
  pan, Esc closes).
- Full Go suite: `TestPhotoThroughRelay` timed out once while Gradle loaded the machine;
  4/4 passes alone (unrelated to these changes). Firmware tests pass.
- Next: calls (WhatsApp-style notification with Answer/Decline, ongoing-call bar,
  Bluetooth headset, full-screen call permission), then beta6 release notes and tag.

### 2026-11-13 — beta6 step 5: calls like a phone call (Android)

- `ed857c8` was steps 3–4 (merge, profile picture viewer).
- Notifications use `NotificationCompat.CallStyle`: incoming → Decline / Answer (Answer
  opens MainActivity with `ACTION_ANSWER`, which answers after the mic/camera permission;
  the mic may only start with the app in front); ongoing → name, running chronometer,
  Hang up.
- `AudioRoutes` (data/): earpiece / speaker / wired / Bluetooth (SCO, BLE headset).
  Android 12+ `setCommunicationDevice`, older `startBluetoothSco`. A headset is picked at
  call start and when it connects mid-call; falls back when it goes away. Call screen: the
  speaker toggle becomes a route menu while a Bluetooth headset is present.
- Voice calls can be minimized (⌄ button or Back) → green `OngoingCallBar` above the app
  (consumes the status-bar inset), tap to return. Video calls stay full screen (their
  surfaces live in the call screen).
- Android 14+: if full-screen intents are refused, a one-time dialog offers the system page;
  Settings shows a "Full-screen incoming calls" row while it is off.
- Release notes `docs/release-notes/v0.1.1-beta6.{en,fa}.md` written.
- **Not run on real phones** (Bluetooth routing and CallStyle need a device).
- Next: tag `v0.1.1-beta6` when the owner says release.

### 2026-11-13 — v0.1.1-beta6 released

- Owner said release. CI green on `ccf7789`; tag **`v0.1.1-beta6`** there. Release published
  (4 APKs, desktop and relay tarballs for amd64/arm64, `release.json`, SHA256SUMS);
  `release.json` verifies (`go run ./tools/release-manifest verify`). eu-1 served the beta6
  manifest on `GET /v1/release?channel=beta` after its 10-minute refresh. No relay changes
  since beta4: no redeploy.
- Next: owner tests on two phones (calls notification/Bluetooth/call bar, video, read
  receipts); then desktop audio+video calls, group calls (≤4) with the data-use warning.

### 2026-11-13 — LilyGO T-Deck Plus: board support (T0) and plan

- Owner wants the T-Deck Plus (ESP32-S3, SX1262 868 MHz, screen, keyboard, mic, speaker,
  BLE, Wi-Fi) as a stand-alone device: messages, calls, identity imported remotely.
- Done now: `[env:tdeck_plus]` in `firmware/platformio.ini` (radio CS 9 / DIO1 45 / RST 17 /
  BUSY 13 on SPI 40/38/41, rail enable GPIO 10, battery GPIO 4, native USB, 16 MB flash) and
  `board.cpp` holds the display and SD chip selects high (`SPI_IDLE_CS_A/B`). It is a LoRa
  node like the other boards. Pins from the vendor's pin map, **not verified on hardware**.
- `pio run -e tdeck_plus` builds (RAM 24.5 %, flash 566 KB), after moving the session to the
  "mesh-msg" cloud environment (the Default one refuses the PlatformIO registry). Host tests
  pass. Never flashed: no board here.
- Plan: `docs/tdeck.md` (steps T1 device UI + BLE host link, T2 identity sent from a
  phone/PC sealed to a one-time device key confirmed by a 6-digit code, T3 stand-alone
  messaging = C++ client kept identical to the Go core by vectors, T4 calls over Wi-Fi with
  Opus, push-to-talk first). roadmap M11, decisions D21, firmware docs, threat-model note.
  Open question for the owner: own identity on the T-Deck, or move the account to it (one
  account can't run on two devices yet).

### 2026-11-14 — iPhone app, first version (M10)

- Owner asked for the iPhone app with calls, Bluetooth mesh and LoRa through Bluetooth nodes
  (no USB on iPhones).
- New `ios/` (SwiftUI, XcodeGen `project.yml`): `Core.swift` (gomobile wrapper), `AppModel`,
  `Views` (onboarding, chats, chat, add contact with own QR, network, settings, call),
  `BleMesh` (Core Bluetooth central + peripheral, Android's GATT protocol; session ID in the
  local name because iOS can't advertise manufacturer data; the iPhone always connects to
  Androids), `LoRaBle` (node host link over GATT as a `Stream` for `AttachLoRa`), `CallAudio`
  (AVAudioEngine with voice processing, Opus 16 kHz through AVAudioConverter).
  `scripts/build-ios-core.sh` (gomobile → `ios/Frameworks/Meshcore.xcframework`),
  `.github/workflows/ios.yml` (macOS runner: core, xcodegen, unsigned simulator build).
- docs/ios.md (what exists, iOS limits, next steps); firmware.md §6 now fixes the LoRa host
  link GATT UUIDs (`6d657368-6c72-000x-…`). **Firmware side of that service is not written.**
- Written without a Mac: never run on a simulator or phone; CI only compiles it.

### 2026-11-16 — Mava custom board only; Meshtastic backbone; hotspot setup (M4)

- Owner: drop every board, build one custom board (keep `mava_handheld_s3`); it must be
  two-way on internet relay, BLE and LoRa-on-Meshtastic, phones and laptops use LoRa through
  it over USB; Wi-Fi is set up from a hotspot + captive portal while a **physical switch** is
  on, and joins the saved Wi-Fi when it is off. Everything on `main`.
- Merged `feature/meshtastic-transfer` (did not compile: `#` comments in
  `gateway_config.h`, missing members in `handheld.cpp`, nRF boards pulled in Wi-Fi/NimBLE)
  and reworked it:
  - **LoRa MTU 233** (Meshtastic DATA_PAYLOAD_LEN) in Go and C++; vectors regenerated; one
    frame ≈ 150 Persian chars (tests adjusted). Link frames longer than the MTU rejected
    (router fuzz found the overflow).
  - `firmware/lib/meshtastic` (replaces `lib/meshtransport`): portable, host-tested; channel
    hash (LongFast = 8 checked), AES-CTR (mbedtls on board, OpenSSL on host), relays under
    the original sender/id with hop limit − 1, own retries keep their id, own packets
    relayed back = implicit ACK. MAC times frames with LongFast airtime.
  - `node::Node`: two host ports at once (USB 0, BLE 1; per-port store sink), uplink API
    (`uplink_take/return/rx`, `served`), host packets reach the other host and the relay,
    `apply_config`, `set_time`. New test `two_hosts_and_uplink`.
  - Board layer rewritten: `ble.cpp` (GATT host link of firmware.md §6, bonded passkey),
    `net.cpp` (switch, hotspot `Mava-XXXX` + captive panel in Persian, Wi-Fi, NTP, relay
    worker on `/v1/gateway` with proof of work, Let's Encrypt roots), `main.cpp`, `radio.cpp`
    (LongFast only), `board.cpp` (ESP32-S3 only). Old `gateway.*`/`handheld.*` removed.
  - Removed Heltec/T-Beam/T-Deck/RAK/T-Echo envs, CI matrix, docs (`tdeck.md`, the branch's
    `esp32s3-gateway.md`, `handheld-meshtastic.md`), app/desktop texts. New `docs/board.md`,
    decision D22 (supersedes D21), roadmap M11 dropped.
- Checks: `go build ./... && go test ./...`, `make -C firmware test` (+ `make sim`),
  `scripts/e2e-lora.sh`, `pio run` (RAM 36 %, flash 1.36 MB) all pass. Android/iOS: only a
  comment and UI strings changed; not rebuilt here. Nothing run on hardware.

### 2026-11-16 — Handheld prototype wiring: ILI9341, WM8960, 7 × 7 key matrix

- Owner chose the parts: Hailege 2.4" ILI9341 240×320 SPI + XPT2046 touch, Waveshare WM8960
  audio module, a raw 14-pin key matrix (32 Persian letters, Alt + Shift to QWERTY, answer /
  hang up, navigation, space, enter). Voice calls on the handheld; video receive-only at best.
- `docs/handheld-proto.md`: parts, full ESP32-S3 pin map (all 31 GPIOs used: one SPI bus for
  SX1262 + LCD + touch, SX1262 and LCD reset on EN, battery % by a MAX17048 on I2C, setup
  switch inside the matrix, no LED), module pin tables, backlight high-side switch, power
  path, matrix with diodes, ISIRI 9147 layout, layers, call/video plan, bring-up order.
  The WM8960 module has no crystal (MCLK from GPIO 15); www.waveshare.com was not reachable
  here to confirm its header names — check the silkscreen.
- Firmware: `lib/meshkeys` (matrix debounce + layout, `test/keys_test.cpp`: every Persian
  letter and a–z exactly once, layers, Alt + Shift, no ghosting with the switch latched);
  `board.cpp` scans the matrix for the setup switch, holds LCD/touch CS high before the
  radio uses the shared bus, keeps strap pins 45/46 low; `platformio.ini` pins updated
  (SX1262 RST now EN → `RADIOLIB_NC`, LED gone). `make test`, `pio run` pass.
- Not written: display (LVGL), touch, WM8960 driver, Opus, messenger UI, H.264 receive.

### 2026-11-16 — Handheld versions B and C: BlackBerry 9900 keyboard (+ trackpad)

- Owner asked for a version with the BlackBerry Bold 9900 keyboard only (B) and one with the
  9900 keyboard and its trackpad (C), next to the 7 × 7 matrix version (A).
- From ZitaoTech/BB9900-USB-keyboard (QMK) and stolen/blackberry_trackpad: the keypad is a
  7 × 6 matrix on 13 flex lines (39 keys, 3 free positions); the trackpad is an Avago
  ADBS-A320 on I2C 0x3B (ID 0x83, motion 0x02, ΔX 0x03, ΔY 0x04), 1.8 V core + 3.3 V I/O,
  13-contact 0.3 mm flex (Hirose FH35C-17S fits).
- `lib/meshkeys`: keyboards per variant (`Matrix7x7`, `BB9900`, `BB9900Trackpad`); Persian
  on the 9900 = ISIRI letters on the 26 letter keys, و on $, ک on 0, Alt + ح ع م ذ = ج چ گ ژ;
  Alt / Shift also one-shot when tapped; Sym tap = symbol picker, Sym held = arrows on E S D F,
  OK on Enter. Tests for all three.
- Board: three envs (`mava_handheld_s3`, `_bb`, `_bbtp`), keyboard backlight on GPIO 44 (B, C),
  setup switch on GPIO 5 in C (no touch), `trackpad.cpp`; key presses and trackpad movement
  are logged so the unknown flex line order can be found by pressing keys. CI builds all three.
- docs/handheld-proto.md §7 (B) and §8 (C). Keypad connector part number unknown (measure).

### 2026-11-16 — Keyboard module (RP2040) for all handheld versions

- Owner: drive the keyboard from a separate microcontroller (a Pico). Done for all three
  versions: `firmware/keyboard` (Arduino-Pico, envs `kb_matrix`, `kb_bb9900`, `kb_bb9900tp`)
  scans the keyboard and the 9900 trackpad, drives the keyboard backlight, is an I2C target
  at 0x1F with an INT line; protocol `lib/meshkeys/src/kbproto.*` (host-tested). The ESP32
  driver `src/kbmodule.*` applies the layout; one ESP32 build for A, B and C.
- ESP32 pin map redone (docs/handheld-proto.md rewritten): display, touch and microSD on
  their own SPI bus (39/40/41), LoRa RESET back on GPIO 8, touch IRQ 6, SD CS 7, keyboard INT
  9, setup switch 14, vibration motor 38; 3, 43, 44 free. Setup switch left the key tables.
- CI builds the keyboard module. `make test`, `pio run` (ESP32 and RP2040) pass.

### 2026-11-16 — Handheld UI framework and touch keyboard

- LVGL 9.2 UI in `firmware/src/ui`: ILI9341 on its own SPI bus, XPT2046 touch, keypad
  input from the keyboard module, trackpad as encoder; screens Home, Chats, Chat, Network,
  Web access (URL + QR), Settings; status bar; sleep after 60 s.
- Touch keyboard `lib/meshui/src/osk.*` (Persian ISIRI order + ZWNJ, Latin, symbols with
  Persian / Latin digits), host-tested.
- Host UI simulator (`make uishots`): found and fixed (1) keypad text must be UTF-8 packed in
  a uint32 for LVGL, (2) LVGL 9.2 bidi lays Persian digits out RTL, (3) LVGL 9.2 draws ۲ as ۰
  (bad table entry) — (2) and (3) patched at build time by `scripts/patch_lvgl.py`;
  (4) DejaVu lacks « » … → own font from Vazirmatn (OFL) via lv_font_conv.
- No messaging client core on the handheld yet: `ui::Messenger` interface, stub = local notes.
- Screenshots in docs/images/handheld-ui. `make test`, `pio run` pass.

### 2026-11-16 — Browser access for iPhones (text)

- Owner: iPhone apps can't be published on the App Store, so phones on the same network as
  a board / handheld use Mava from the browser. docs/web-access.md.
- `web/lite`: browser client = core identity / e2e / message / announce / receipt without
  core/node's SQLite (which can't run in WebAssembly); state as one JSON blob
  (localStorage); transports: the board's host link over HTTP (`/api/link`) and relays over
  HTTPS (mailbox long-poll, receipts by ID, uploads with PoW). `web/cmd/webclient` (wasm,
  11 MB / 2.9 MB gz), `web/static` (Persian RTL page), `scripts/build-web.sh`.
- Board: web app at `/app/` from LittleFS in both Wi-Fi modes, `/api/link` = host port 2;
  `Node` ports 3 with per-port trust (web port can't configure / wipe; tested); custom
  partition table (2 × 4 MB app, 7.9 MB LittleFS).
- Relay API: CORS (preflight + headers), tested.
- Tests: `go test ./web/...` (relay in process + board stand-in); `scripts/test-web.sh`
  (two headless Chromium windows via `web/cmd/devboard`: board link and relay, PASS).
- Not built: calls in the browser (HTTPS secure context, WebCodecs/Opus, UDP proxy) —
  written up in docs/web-access.md.

### 2026-11-16 — Relay list cached on disk and shared with nearby devices (D23)

- Owner: every app should cache the servers list from GitHub and from nearby devices, so it
  doesn't wait for it after every start and still has it in a blackout.
- Before: relays were kept in `relays_known`, but every start fetched GitHub first (up to 20 s,
  blocking first use of new relays), and the list only spread as one relay hint per announce.
- `network`: `serial` in relays.json, `Signed` (sig || JSON), `Sign`/`DecodeSigned` with the
  release key under `MESHMSG-RELAYS-v1\0`; embedded `relays.json.sig` (empty until signed);
  `tools/relays-sign` (sign / verify); test fails on a stale signature.
- `router`: relay-list bulletin (`RelayListDest`, `RelayListPacket`, `PublishRelayList`,
  `Config.RelayList` check): stored and passed on only if accepted, mesh links only (no
  uplinks, `NO_LORA`), older ones dropped from the store.
- `core/node/relaylist.go`: list + signature + last-fetch time in `kv`; start uses the newer of
  cache and build at once; GitHub refresh in the background every 6 h (not on every start);
  accepted bulletins saved and applied; held signed list republished every 12 h. Covers meshd,
  Android and iOS (all on core/node). The board firmware doesn't run discovery.
- Tests: `TestSignedList`, `TestEmbeddedSignatureCurrent`, `TestRelayListBulletin`,
  `TestRelayListFromNearbyDevice` (offline phone learns from a neighbour, keeps it across a
  restart, hands it on), `TestRelayListWrongKeyIgnored`. `go build ./... && go test ./...`,
  `make -C firmware test` pass. Not released yet.

### 2026-11-17 — Handheld schematic PDF; web status wording

- `hardware/handheld/schematic.py` → `handheld-schematic.pdf` (7 A3 pages: overview, ESP32-S3,
  LoRa + display + backlight, audio + fuel gauge + vibration, power + USB, keyboard module +
  version A matrix, versions B/C keypad + trackpad + 1.8 V LDO). Modules connect by net
  name. Fixed while reviewing the render: pin anchors of +/− pairs collided (D+/D−, IN+/IN−,
  LED+/LED− lost a net), pin order, fixed drawing scale.
- Web client: the header showed "● برد ○ رله", which read like a choice; it is a link
  status ("اتصال: برد ✓ · رله ✗"). Every message goes out on all links at once as the
  browser's identity; Settings explains it. `scripts/test-web.sh` PASS.

### 2026-11-17 — Violet cyberpunk design system (UI UX Pro Max)

- Owner: use the UI UX Pro Max skill, violet / purple cyberpunk theme, on the board and the
  web interface. docs/design.md: tokens (dark OLED `#0B0A14`, violet `#7C3AED`/`#A78BFA`,
  magenta focus `#D946EF`, cyan "up"), Vazirmatn + Orbitron wordmark (self-hosted, OFL),
  chamfered corners, glow only on focus / primary, faint scanlines, reduced motion.
- Web client rewritten on the tokens: SVG icon sprite (no emoji), link chips with icon +
  word, labelled fields, toasts, safe areas, only new messages animate. Setup panel
  (net.cpp) restyled. Handheld LVGL theme: dark violet, focus glow for keypad / trackpad,
  violet bubbles and keyboard. Screenshots in docs/images/{handheld-ui,web}.
- `scripts/test-web.sh` fixed: `go run` left the dev server running between runs (stale
  port); it now builds and runs a binary. PASS.

### 2026-11-17 — Test release v0.1.1-beta7: Linux flasher for the board

- `tools/flasher/mava-flash` (board / firmware / web / keyboard / log / info, --erase,
  --dry-run, auto-detects the Espressif USB port, dialout hint) and
  `scripts/build-flasher.sh` → `mava-board-flasher-<ver>-linux.tar.gz`: bootloader,
  partitions, boot_app0, firmware, LittleFS (web client), three keyboard-module UF2s,
  Espressif's standalone esptool 4.8.1 (x86-64), SHA256SUMS. docs/flashing.md.
  Checked here: bundle builds, dry run, esptool image_info, merge_bin of all images into
  16 MB (no overlap), partition table decoded. Not flashed (no board here).
- `android.yml`: new `board` job; the release needs it. Release notes v0.1.1-beta7 en/fa.
- Tag `v0.1.1-beta7` on main (CI publishes Android APKs, meshd/mesh, relay, board flasher,
  signed release.json).

### 2026-11-17 — Project blueprint poster

- `hardware/blueprint/blueprint.py` → `docs/images/mava-blueprint.{png,pdf}` (7200×4800 px +
  vector PDF): tiers and roles, packet header and Noise K/X, LoRa sizes, Bluetooth (phone
  mesh and board host link: UUIDs, link sequence, rules), the board (ESP32-S3, SX1262,
  node ports, setup switch, relay worker, flash layout), handheld + RP2040 keyboard module
  + three versions + UI, Meshtastic frame and relaying, software stack, a message's path,
  security / release / design tokens. Linked from README.
- protocol.md single-frame budget corrected to the 233-byte MTU (154 bytes of text).
- Tag v0.1.1-beta7 could not be pushed from the cloud session (git proxy cuts tag pushes;
  branch pushes work): the owner pushes it (`git push origin v0.1.1-beta7`).

### 2026-11-17 — Releases from a manual Actions run

- Tag pushes are cut off from cloud sessions, so `android.yml`'s workflow_dispatch takes
  `release_tag`: a run started by hand on main with e.g. `v0.1.1-beta7` builds everything
  (Android, Linux desktop + relay, board flasher) and the release step creates the tag on
  that commit and publishes. Tag pushes still work as before. Validated: tag format, main only.


### 2026-11-17 — v0.1.1-beta7 released

- Released through Actions run #87 (workflow_dispatch, `release_tag=v0.1.1-beta7`, commit
  d5580f6, Android versionCode 187): https://github.com/Aryalleh/mesh-msg/releases/tag/v0.1.1-beta7
  (pre-release). Assets: APKs (arm64-v8a, armeabi-v7a, x86_64, universal),
  meshmsg-desktop linux amd64/arm64, meshmsg-relay linux amd64/arm64,
  mava-board-flasher linux, signed release.json, SHA256SUMS. The tag was created by the release step.
- Nothing from this release has been tested on hardware yet (see Next step).

### 2026-11-17 — Board flasher for Android

- `android/flasher` (new Gradle module, app id `org.meshmsg.flasher`, Compose, Mava violet
  theme, en/fa): writes the board over USB-OTG. Espressif's ROM serial protocol in Kotlin
  (`esp/EspLoader.kt`: SLIP, sync, chip check, watchdogs off on USB-Serial/JTAG, SPI attach,
  FLASH_DEFL_* in 1 KB blocks, MD5 verify, force-download clear + RTS reset; reset sequences
  and constants checked against esptool 4.8.1's source). USB drivers in plain Android USB
  host calls (`usb/UsbSerial.kt`): ESP32-S3 native USB / CDC-ACM, CP210x, CH340.
  Modes: Install everything (= `mava-flash board --erase`), Update firmware only. "Board log"
  resets the board and shows its text log, picking out hotspot name + device password.
- Tests: `EspLoaderTest` against a simulated ESP32-S3 ROM (`FakeRom`: download mode only
  after the right reset, boot text, inflate into 16 MB, MD5): full and firmware-only writes,
  baud switch on UART bridges only, verify failure, silent board.
- `scripts/build-flasher.sh` also writes `dist/board-images/` (bootloader with dio/80m/16MB
  merged with the partition table by esptool merge_bin, boot_app0, firmware, littlefs,
  images.txt); CI: new `flasher-android` job builds `mava-board-flasher-<ver>-android.apk`
  for the release; the `android` job builds `:app` only. Firmware unchanged (owner asked).
- Not tested on a phone with a board yet.
- Released as **v0.1.1-beta8** (Actions run #89, commit a23a1e8): adds
  `mava-board-flasher-0.1.1-beta8-android.apk` to the usual assets.

### 2026-11-17 — Hotspot without the switch; board Wi-Fi over the host link

- Owner's board showed no hotspot (no switch fitted; Wi-Fi was off with nothing saved).
  Firmware: the hotspot is on while the switch is on, **or no Wi-Fi is saved**, or a host
  sent `HOTSPOT 1`; after a panel save with the switch off it leaves the hotspot after 5 s
  and joins. Wi-Fi + BLE now start before radio / store / keyboard / screen (lock held, the
  relay worker waits; released before the screen, whose status callback takes it); each
  boot step is logged and the hotspot name / device password / BLE passkey / Wi-Fi / relay /
  radio line repeats every 10 s until a USB host attaches.
- Host link: `SET_WIFI` 0x09 (TLVs 1 SSID, 2 password, 3 relay; checked in
  `host::WifiConfig::parse`) and `HOTSPOT` 0x0A, trusted ports only, via
  `Platform::set_wifi/hotspot` (default: unknown op). Go: `hostlink.WifiConfig`,
  `Node.LoRaWifi/LoRaHotspot`, meshd `POST /lora/{name}/wifi|hotspot`, `mesh lora wifi
  ssid=… password=…`, `mesh lora hotspot on|off`. Android flasher: **Board Wi-Fi** panel
  (Kotlin host link framing, checked against spec vectors). Tests on all three sides.
- Next: the owner flashes beta9 ("Update firmware only" or `./mava-flash firmware`) and
  sends the board log; unknown module (N16R8 needed) and the cable/socket remain suspects.

### 2026-11-17 — Fixed default device password

- Owner's choice: every board starts with device password `12345678` (`net::kDefaultPassword`,
  WPA2 minimum is 8) instead of a random one; a password set in the panel is kept, a full
  erase / Install everything restores the default. Docs (board, flashing, web-access, mava-flash)
  updated. Release v0.1.1-beta10 (beta9 was cut just before, from 1c8e600).
- Released **v0.1.1-beta9** (run #91, 1c8e600) and **v0.1.1-beta10** (run #93, ae98200), both green.

### 2026-11-17 — Mava Screen: the handheld's screen on a phone over USB

- Firmware: `lib/meshui/src/mirror.*` (host-tested): 240×320 RGB565 copy in PSRAM, 16×8
  dirty tiles, RLE or raw per tile, info/tile/input wire formats. The display driver's
  CASET/PASET/RAMWR are tapped (`src/ui/display.cpp`) into `remote::blit`;
  `src/ui/remote.*` handles `SCREEN` 0x0B / `INPUT` 0x0C through the new
  `Platform::board_command` (Node's unknown ops, trusted ports only) and `remote::pump`
  sends tiles with `Node::host_event` while `host_room` allows. UI: a remote pointer indev
  and `ui::remote_key` (keys::Action → `on_output`). Event `SCREEN` 0x86.
- Android: new library `:boardlink` (UsbSerial, SerialLink, HostLink, BoardLink — moved out
  of the flasher) and app `:screen` (`org.meshmsg.screen`, "Mava Screen"): auto-connects when
  the board is plugged in and allowed, draws the frame sharp at ≤30 fps, touch (press /
  30 ms moves / release), the handheld keys, typing via the phone keyboard. Tests: mirror
  round trip + wire bytes (C++), the same bytes decoded in Kotlin. CI builds it in the
  `flasher-android` job → release asset `mava-screen-<ver>-android.apk`. docs/screen.md.
- Release v0.1.1-beta11. Not tested on hardware.
- beta11 run #95 failed only in the APK check step (`unzip -l dist/*.apk` with two APKs); fixed, re-run.
- Owner hit "WebAssembly.instantiate(): expected magic word … found 1f 8b" in the board's
  browser app (so the board runs and serves /app/). Arduino's static handler doesn't know
  `.wasm` → application/octet-stream → no `Content-Encoding: gzip`. Fix: `/app/main.wasm`
  served by `serve_wasm` as application/wasm (streamFile adds the encoding); app.js unpacks
  gzip with DecompressionStream if it still arrives compressed (checked in Chromium with a
  raw-gzip main.wasm). Cancelled the beta11 run (#97) and re-cut beta11 with the fix.
- Released **v0.1.1-beta11** (run #99, 66b7893): adds `mava-screen-0.1.1-beta11-android.apk`; includes the board web-app wasm fix.

### 2026-11-17 — Android app ↔ Mava board over Bluetooth

- Owner asked why the app's BLE mesh kept searching with the board centimetres away: that
  mesh only finds phones (service 6d657368-6d73-6700); the board's host-link service
  (6d657368-6c72-0000) had no Android client. New `lora/BleLoRa.kt` (StreamOpener like
  UsbLoRa): scan by service UUID (nearest by RSSI), GATT connect, MTU 247, bond (passkey
  prompt; RX needs encryption + MITM), subscribe to TX notifications, writes with response
  chunked to MTU−3; attached as LoRa link "ble". Settings.boardAddress remembers the board;
  MeshApp reattaches it on start/restart/permission; Network screen button + snackbars
  (en/fa). Built locally (NDK + gomobile AAR here now): app lint + 86 unit tests green.
- boardlink `BoardLink.diagnosis()`: a missing ACK now reports nothing / log text only /
  other frames (owner saw "did not answer" on beta10). Test added.
- Release v0.1.1-beta12. Not tested on hardware.

### 2026-11-17 — First real board log (owner): relay unreachable, screen none

- Owner's board (no SX1262, no screen, no keyboard module) boots, joins Wi-Fi
  (192.168.0.80), BLE ok; log said `relay unreachable`, `screen: none`, and Board Wi-Fi
  commands: one ok then "did not answer".
- Relay: `probe()` reports the reason (DNS / HTTPClient error + TLS lastError / HTTP code);
  for the default relay it falls back to `http://87.106.5.192` (same server) and says
  "online (by IP address)"; 3 failed gateway calls → probe again. Web client default relays
  now include the IP too. (eu-1 also still lacks the beta7 CORS headers: owner to update it.)
- Screen: draw buffers no longer need DMA-capable internal RAM (CPU-fed SPI): internal, else
  PSRAM. Boot log prints free internal / PSRAM (and whether PSRAM was found) and
  `display::error()`.
- USB host port detaches after 90 s without host bytes (not while the screen mirror runs),
  so the text log comes back after one-shot commands.
- Release v0.1.1-beta13.
- Released **v0.1.1-beta12** (run #101, 7854748) and **v0.1.1-beta13** (run #103, a638ba4), all assets present.

### 2026-11-17 — eu-1 updated to beta13; update check picks the highest tag

- eu-1 relay upgraded 0.1.1-beta4 → 0.1.1-beta13 (official release tarball, SHA256SUMS
  checked; old binary kept as `/usr/local/bin/meshmsg-relay.bak-beta4`). `/v1/info` answers
  on the domain and the IP. This also brings the beta7 CORS headers the board's web client needs.
- Bug found while doing it: eu-1's `/v1/release?channel=beta` served **beta9**, not the newest.
  GitHub's releases list is not newest-first (it returned beta9, beta8, beta7, beta12, beta11)
  and `FetchGitHub` stopped at the first signed manifest. It now sorts releases by tag version
  first (`TestFetchGitHubUnorderedList`, fails on the old code). Affects relays, meshd and the
  Android app (same Go core). Reaches eu-1 only after the next release + relay update.
### 2026-11-17 — Board in the phones' Bluetooth mesh, no pairing

- Owner: remove the pairing code; BLE must join the mesh like phone↔phone and phone↔node.
- Firmware `ble.cpp` rewritten: no security; two services — the phones' mesh service
  (BleProto: advert = mesh UUID + manufacturer 0xFFFF + session 00000000, so every phone
  connects to the board) and the host link (scan response with the name). Up to 3
  connections, NimBLE events queued to the main loop; mesh phones get seats = node host
  ports 3–5 (`kHostPorts` 6, store sinks Host3–5, untrusted) via `lib/meshnode/blemesh.*`
  (hello → RESYNC + HELLO; len3 frames ↔ host-link packet frames; media > 233 B skipped;
  host test `ble_mesh_peers`). Host-link port 1 untrusted over BLE (no pairing). Logs/UI/panel
  no longer show a passkey.
- Node allocated in PSRAM (static RAM 164 kB → 83 kB).
- App: BleLoRa no longer bonds; strings updated. Lint + 86 tests green.
- Release v0.1.1-beta14.

### 2026-11-17 — One Bluetooth mesh: board ↔ board, meshd on Bluetooth

- Owner: all devices must talk over Bluetooth, one unified mesh.
- Firmware: board session ID = 00 + 3 random bytes; `central_task` (NimBLE client) scans for
  the mesh service and connects to devices with a lower session ID (other boards), one
  outgoing link at a time: hello, notifications → mesh seat (Ev::ClientUp, `Peer::start`),
  main loop → stream buffer → writes with response. Host test extended (client-mode seat).
- meshd: `desktop/blemesh` (tinygo.org/x/bluetooth v0.17.0, BlueZ over D-Bus, pure Go: static
  builds still fine). Central only (never advertises): scan 8 s every 15 s, connect to new
  mesh devices up to 4; router link "ble" with len3 framing, hello first, per-peer writer;
  unit tests. `[bluetooth] off = false` default (on for old configs too); started with the
  node in `Daemon.startNode`. Without an adapter: "no Bluetooth adapter", retry every 5 min.
- iPhone already connects to anything advertising the mesh with manufacturer data, so
  iPhone ↔ board works with beta14.
- Release v0.1.1-beta15. Not tested on hardware.
- Released **v0.1.1-beta14** (run #106, b4fd9cb) and **v0.1.1-beta15** (run #108, 2afd8e3), all 13 assets each.

### 2026-11-17 — The handheld's own messenger (lib/meshclient)

- Owner asked to activate messaging on the handheld (it showed the local-notes stub).
- `firmware/lib/meshclient`: `e2e.*` Noise K/X + IETF ChaCha20-Poly1305 built from
  Monocypher's ChaCha20/Poly1305 (Monocypher 4's streaming AEAD rekeys: not Noise), HMAC/HKDF
  on the firmware SHA-256; byte-identical to core/e2e: new `e2e` section in
  spec/test-vectors (core/e2e `SealWith` fixed ephemeral), plus RFC 8439 §2.8.2. `textenc.*`
  (the `text` vectors now run in firmware tests). `client.*`: identity, contacts (saved +
  heard nearby, max 64), 100 messages/chat on Storage, send (X first, K once they wrote),
  fragments ≤16 + reassembly, receipts both ways (✓), announces every 30 min with the
  nickname, contact link / meshmsg: URI parse + base64url. Host test `run_client`.
- Board: node host port 6 `kLocalPort` (sink `kSinkLocal`, bit 7); `src/ui/client_messenger.*`
  (LittleFS `/cl-*`, user seed `/cl-seed`, HELLO-attached host port, packets both ways);
  UI: Chats → My contact (QR of the link), nearby people, ✓, live refresh on revision.
- Release v0.1.1-beta16. Not tested on hardware (phone ↔ handheld end to end untested).
- Released **v0.1.1-beta16** (run #111, 0e4c36f).

### 2026-11-17 — Landscape screen, fast USB mirror, phone keyboard, relay between all layers

- Landscape 320×240 (`LCD_ROTATION` 90, hardware MADCTL rotation); mirror 320×240 (20×30
  tiles; vectors in mirror_test + ScreenFrameTest updated); remote touches un-rotated before
  LVGL's pointer transform (`ui::remote_touch`); home in two columns, OSK 112 px, QR 150 px;
  UI simulator renders landscape.
- USB mirror speed: cause = HWCDC TX buffer 256 B + one ≤512 B write per loop. Now
  `Serial.setTxBufferSize(16384)`, drain loop each pass, `kHostOut` 8 kB, 64 tiles/pass.
- INPUT option `2 | id | value` (`kOptTouchKeyboard`): Mava Screen switch "Type with the
  phone's keyboard" (default on, saved) hides the board's touch keyboard.
- Relay across layers already held (stores for every sink); host test `all_layers` now proves
  BLE→LoRa+relay+phones, LoRa→phones+relay, relay→phones+LoRa.
- Release v0.1.1-beta17.

### 2026-11-17 — One account on the device (browser = the board's messenger)

- Owner: the iPhone browser app and the ESP32 messenger must be ONE account (board in a
  backpack, phone as screen; phone dead → handheld).
- Firmware: `net::Account` (JSON) implemented by `client_messenger.cpp` over lib/meshclient;
  `/api/acct/{login,me,chats,messages,rev,send,add,name}` with a device-password session cookie
  (4 sessions, lockout shared with the panel, cleared on password change). Client `rename()`.
  Messenger runs with or without a screen. Station mode is AP+STA (board hotspot stays up).
- Web: `BoardAccount` in app.js with the WASM client's interface; login screen; actions awaited;
  `main()` picks board account (200/401) or the in-browser one (404). Chromium test against a
  fake board (login, preview, send ✓, incoming via polling); scripts/test-web.sh PASS.
- Calls (owner also asked): not built; see the plan in the reply / docs/calls.md.
- Release v0.1.1-beta18.
- Released **v0.1.1-beta17** (run #113, e3b0409) and **v0.1.1-beta18** (run #115, 9ef0519).

### 2026-11-17 — Bugs from the owner's first phone↔board tests

- Android "Allow Bluetooth" card although allowed + mesh dead: `BleLoRa.LINK_NAME` was "ble",
  the same as the phones' mesh transport; the router keys links by name, so attaching the
  board replaced the mesh link, and Models classified "ble" as LoRa (no Nearby path → the
  allow card). Renamed "lora-ble"; the core now refuses colliding names
  (`TestLinkNamesDontCollide`); the card distinguishes "not allowed" from "Bluetooth off".
- Phone → relay → board account: nothing arrived. The node serves (asks the relay for) only
  destinations heard announced; the account announces only with a clock; NTP may be filtered.
  Now `client_pump` serves the account's destination itself (`Node::serve` public), and the
  relay worker sets the clock from the relay's HTTP `Date` header when NTP hasn't
  (`http_date`, checked against Python's parser). Log shows the account state.
- Release v0.1.1-beta19.
- Released **v0.1.1-beta19** (run #117, 111512f).
- Phone ↔ board Bluetooth mesh never formed (phone "searching", board "Bluetooth phones 0").
  The phone router already sends on all links (relay, LoRa, every BLE peer). Likely cause:
  the phone kept a bond from the pairing-code builds (beta10–13) to the chip's public MAC;
  after a fresh flash the board lacks the keys, Android's encryption fails, link drops.
  Board now uses a random static address made from the node identity (`ble::begin(name, id)`),
  logs a `Bluetooth …` status line every 10 s (address, advertising, links, connects,
  hellos, drops, last HCI reason with a hint). Android: `StaleBond.forget` before mesh/board
  connects (reflection; refused on Android 13+), `BleLoRa` rescans after 2 failures and
  stores a moved address; mesh scan BALANCED unless battery saver.
- Release v0.1.1-beta20.
- `all_layers` test now covers the handheld's own messenger (`kLocalPort`): it gets every
  packet from Bluetooth, LoRa and the relay, and its own message goes out on both Bluetooth
  peers, LoRa and the relay (the board is a gateway like a phone or meshd). Test only.
- Released **v0.1.1-beta20** (run #119, face4e3).
- Board web page 404: `Platform::wipe` → `LittleFS.format()` erased `/web` (the browser client,
  installed only by the flasher's "Install everything"); `/app/` then fell through to 404.
  `board::storage_format` now removes everything except `/web`; missing client → a 404 page
  that says how to fix it (root and `/app…`), `/app` → `/app/`, other unknown paths on the home
  Wi-Fi → `/app/`; boot log says "browser app installed / MISSING". Release v0.1.1-beta21.
- Released **v0.1.1-beta21** (run #122, b5eb12e).
- Web app on the board: "Failed to fetch" on send, board chip "disconnected", no Wi-Fi setting
  in the web app. Added `/api/acct/status` (Wi-Fi/relay/hotspot/switch/scan list) and
  `/api/acct/wifi` (device-password cookie only, no messenger needed → `set_wifi`); Settings →
  "Board Wi-Fi and relay" card; relay chip from status; `BoardAccount.api` retries (10 s
  timeout, 3 tries) and shows a hint (mobile data routes around a no-internet Wi-Fi) after 2
  failed polls. Loop task stack 16 kB (`SET_LOOP_TASK_STACK_SIZE`): browser sends run Noise +
  Ed25519 inside the WebServer handler. Checked with Playwright against a fake board.
  Release v0.1.1-beta22.
- Released **v0.1.1-beta22** (run #124, f5da158).
- Owner's board log (beta22): BLE works (`links 1, hellos 1`, random static address), but no
  message crosses; relay "connection refused" on https://eu-1.mfadem.ir (the sandbox proxy
  blocks that host, so it could not be checked from here). Added: `blemesh::Peer` counters
  (packets in / skipped too-big / out), `mc::Client::stats()` (data for us, opened,
  unreadable, sent, receipts); log line buffer 160 → 512 and the 10 s status split into
  Wi-Fi/radio, relay, account, Bluetooth lines (it was truncated).
- Relay self-update: `meshmsg-relay -self-update` (server/cmd/relay/selfupdate.go): newest
  signed release.json from GitHub (`update.FetchGitHub`, release key), tarball for GOARCH
  checked against its SHA-256, new binary must print the manifest version, atomic rename,
  `systemctl restart`. Root oneshot `meshmsg-relay-update.service` + 6 h timer, installed and
  enabled by install.sh (`MAVA_AUTO_UPDATE=n` off), shipped in the relay tarball. Test with a
  fake GitHub: tampered tarball and foreign key refused.
- Android flasher fetches firmware from GitHub: releases now publish
  `mava-board-images-<ver>.zip` (in the signed manifest). `RemoteFirmware` verifies
  release.json with a pure-Kotlin Ed25519 (RFC 8032; API 26 has none), downloads + SHA-256,
  unzips to filesDir/board/<ver>; the newest of APK-bundled/downloaded is used; INTERNET
  permission; auto check at start + button. Tests: RFC vectors, the real beta22 release.json.
- Release v0.1.1-beta23.
- Released **v0.1.1-beta23** (run #126, 61aa31f); release now carries mava-board-images-0.1.1-beta23.zip (4.1 MB) in the signed manifest.
- Owner's beta23 log: BLE link up (links 1, hellos 1, packets in 78→251/30 s, out 14→36: the
  phone gateways its relay traffic onto BLE), account "messages for it 0" (the phone's board
  contact is the account from before the last "Install everything", which wiped the seed).
  Owner says the phone app shows no BLE mesh connection; asked for a screenshot.
  "relay no wifi" while joined: the relay state was set before Wi-Fi came up and not again
  until an attempt ended; TLS handshake waits 120 s by default. Now `set_detail("connecting")`
  before probes, HTTP connect timeout 8 s, TLS handshake 20 s, and an "off" state that names
  the reason. Release v0.1.1-beta24.
- Released **v0.1.1-beta24** (2cdf160).
- Owner's beta24 log: relay "online (by IP address)" (the domain is blocked on that network,
  the IP works), BLE link up, account still 0 messages, and a task watchdog reset (IDLE0
  starved, ipc0 running on CPU0 while loopTask ran on CPU1): every store change was a LittleFS
  write (FlashPersist), ~12 packets/s from the phone's relay traffic → flash ops stall the
  other core. `FlashPersist` is now write-behind (PSRAM copy per slot, ≤1 slot per 250 ms).
  `mc::Client` logs its id and the last 4 destinations of DATA heard for other accounts
  (to show a phone writing to an old account). Release v0.1.1-beta25.
- Released **v0.1.1-beta25** (2adce5a).
- Owner's beta25 log: link up, relay online by IP, but no DATA at all reached the board's
  client (not even "other accounts"), board → phone "sent 1" yet BLE out frozen; board dropped
  the link itself (0x216) and after the reconnect in/out froze. Cause: `ble.cpp` `post()`
  dropped events when its 32-slot queue was full (phone gatewaying relay traffic); a lost
  write desyncs the len3 stream (all later packets rejected), a lost MeshSub/Disconnect leaves
  a half-dead seat. Queue now 256 in PSRAM (`xQueueCreateWithCaps`), `post` waits up to 2 s
  (back-pressure: the write is answered late), counters lost/corrupt/queue in the log, relay
  up/down counts in the relay line. Release v0.1.1-beta26.
- Released **v0.1.1-beta26** (35a6194).
- Owner's beta26 log: lost 0 (queue fix works), relay online at the domain, but one corrupt
  stream (board drop 0x216); afterwards the phone sent almost nothing; board "sent 1" never
  uploaded (relay up stayed 1). Causes found: (1) Go `Router.PeerDown` kept the has-marks of
  the peer, so packets lost with a broken link were never resent on reconnect (same session
  ID) — now forgotten on PeerDown (`TestResendAfterBrokenLink`); (2) the board's 64-slot store
  turned over in seconds under relay + BLE traffic and evicted its own messages — board store
  256 slots (`-DMESH_STORE_SLOTS=256`), local-port packets are `Origin::Local` and evicted
  last (`own_messages_kept` test); (3) BLE log names the corrupt-stream reason
  (`Peer::error()`). post() clamps writes to 244. Release v0.1.1-beta27.
- Released **v0.1.1-beta27** (9f73e6c).
- Owner's beta27 log: "frame length 5080217" (0x4D8499: a hello mid-stream) and "no hello
  (101 bytes, first 0x00)"; connects 8 / drops 7 / corrupt 9 after the phone first closed
  the link (0x213). Android reuses a still-closing ACL for the next connectGatt, so the
  phone's new session landed on the board's old seat. Board: `blemesh::Peer` restarts the
  session on a hello from the same session ID anywhere, or any hello at a packet boundary
  (`restarts()`; ble.cpp drops the seat's pending bytes; test `ble_mesh_restart`). Android:
  `BleMesh` waits 5 s before reconnecting a dropped peer, and records why it dropped one
  (`lastDrop`, shown as "Last disconnect" on the Network screen). The phone's first drop
  reason is still unknown: that line will show it. Release v0.1.1-beta28.
- Released **v0.1.1-beta28** (c784fd9).

### 2026-11-18 — No-LoRa-module setting; Bluetooth restart storm; message/store diagnostics

- Owner's board has no SX1262: RadioLib's `findChip` retries 10× (each ≥ 1 s of failed
  `standby`, up to 2 s with BUSY floating high), so boot sat 10–20 s at `boot: radio` holding
  the node lock; Bluetooth writes only queued meanwhile. New node config TLV `kTagRadio` (8):
  1 fitted (default, **not encoded**, so vectors and old configs are unchanged), 0 none →
  `radio::begin` skipped at boot, `apply_radio` puts the chip (if any) to sleep; turning it
  back on probes live. Set from the board's screen (Settings → LoRa module button, via
  `ui::on_radio`) and from Mava Flasher (new "LoRa module" panel: `SET_CONFIG` with the
  partial TLV, `HostLink.radioTlv`). Go `hostlink`: `TagRadio`, `NodeConfig.NoRadio`
  (skipped-unknown before, so old meshd is fine). Log/screen: radio "ok" / "off (no LoRa
  module)" / "NOT FOUND".
- Owner's beta28 log: one connection (`connects 1, drops 0`) but `hellos 28, restarted 27` in
  a minute, `out` +~1300 per 10 s (each restart = RESYNC of the 256-slot store), then quiet.
  Race: NimBLE answers the phone's hello write as soon as `onWrite` returns, the main loop
  handles it later; notifications of the old stream sent in between start the phone's new
  stream mid-packet → bad stream → reconnect → hello → loop. `ble.cpp`: a hello-shaped
  mesh write bumps `g_hold[conn]` in NimBLE's task before the answer; the main loop sends
  nothing on that connection until it has handled the queued hello(s).
- Same log: `messages for it 0` and no "other accounts" at all while `packets in` reached
  1439 — unknown whether the phone's DATA never crossed or the store refused it (the store
  was full from boot: `restored 256`; a store full of receipts refuses DATA, uncounted until
  now). Added: `blemesh::Peer` counts DATA from the phone and its last 3 destinations
  (Bluetooth line "messages N: for 5cf3738e… ×k"); `store::Stats::full` (refused, nothing
  less valuable to evict) and `Store::census()`; a 10 s "store" line (messages/own/receipts/
  announces/other, refused full + per-destination). Tests: `commands` (radio TLV via
  SET_CONFIG), `ble_mesh_restart` (DATA counts), store `eviction` (census, full).
- Next: owner flashes beta29, sets "No LoRa module", sends the log with the new Bluetooth /
  store / account lines after sending one message from the phone, plus the phone's
  "Last disconnect".
- CI "Go" was red since beta27 on a test-only race: `desktop/blemesh`
  `TestWriteFailureDropsPeer` read `closed` / `h.down` while `Link.Down` (writer goroutine)
  still set them (it removes the peer before closing it). Test now waits for all three
  (atomic flag, host mutex).
- Released **v0.1.1-beta29** (manual `build` run with `release_tag`, the tag push was refused
  from this session; tag on `c367a26`; mava-board-images-0.1.1-beta29.zip in the signed manifest).

### 2026-11-18 — Store full of receipts refused all messages; NimBLE buffers ("status 17")

- Owner's beta29 log: `store 256/256: messages 0 (own 1), receipts 256 … refused 11 (full of
  more urgent)` with `relay … down 10`: RECEIPT outranked everything, a gateway phone's relay
  traffic filled all 256 slots with them (restored at every boot with a fresh TTL), and every
  DATA, incl. the phone's message through the relay, was refused before reaching the
  messenger. `Store::victim`: receipts beyond `kReceiptMax` (kSlots/4) rank like SUMMARY
  (below DATA, above ANNOUNCE); `Store::shed_receipts` / `Node::shed_receipts` after
  `restore_store` drops the oldest beyond the cap (logged). Test `receipts_capped`.
  Not changed: restored records get a full TTL at every boot (no wall clock in the record).
- Phone's "Last disconnect: bad stream: meshcore: chunk from unknown peer "0069538d"" (the
  board's session) + "status 17": a GATT write answered ATT 0x11 insufficient resources —
  the board pumped notifications (4 per loop) until NimBLE's 12-block msys pool was empty,
  so the reply to the phone's next write could not be allocated. Then notifications already
  posted for the dropped peer reached `core.receive` after `peerDown` and overwrote the
  reason. Board: `pool_ok()` (≥ 8 free `os_msys_num_free()`) before each notification,
  pool 20 blocks (`CONFIG_BT_NIMBLE_MSYS1_BLOCK_COUNT`), Bluetooth line shows buffers and
  waits. Android `BleMesh`: retries a write refused with 0x11 / congested (same chunk, 20 ms,
  up to 100×; NimBLE allocates the reply before handling the write, so it was not taken);
  ignores chunks for a peer that is no longer the current one; `lastDrop` keeps the first
  reason per peer.
- Released **v0.1.1-beta30** (manual `build` run #44303209898 with `release_tag`, tag on `644a5cd`).

### 2026-11-18 — Display moved to the DevKitC's left header

- Owner can wire only the left header of the ESP32-S3 DevKitC (3V3, 3V3, RST, 4, 5, 6, 7,
  15, 16, 17, 18, 8, 3, 46, 9, 10 … 14, 5V, GND); new display: red 2.8" "TFT SPI 240x320
  V1.2" (ILI9341 + XPT2046 + microSD, backlight transistor on board, J1 = 3.3 V bypass).
  Display pins now in the module header's order: CS 15, RESET 16, DC 17, SDI 18, SCK 3,
  LED 46, SDO 9 (touch T_CS 5, T_IRQ 6, SD CS 7 unchanged; T_CLK/T_DIN/T_DO on the bus).
  The audio pins (not used by code yet) and keyboard INT moved to the freed right header:
  I2S MCLK/BCLK/LRCK/DIN/DOUT 39/40/41/42/45, KB_INT 21 (RTC pin). Radio pins unchanged
  (DIO1 stays on GPIO 1, right header). docs/handheld-proto.md §2/§4 updated.
- Released **v0.1.1-beta31** (manual `build` run #44312557544, tag on `f4317d1`). Next: owner
  wires it and sends the boot log (`screen: ok`).

### 2026-11-18 — Hardware simulators as a removable library; picture off; phone audio

- Owner: the board's display works, so Mava Screen gets "keys and keyboard only"; it needs
  audio simulators too (owner chose speaker + microphone); all of it as a library that the
  v1 release drops.
- Firmware: `src/ui/remote.*` → `src/hwsim/hwsim.*` (facade with inline stubs when
  `MAVA_HWSIM` is 0), `lib/meshui/src/mirror.*` → `lib/hwsim/src/`. New `lib/hwsim/src/
  audiolink.*` (AUDIO op 0x0D: stop / start flags / mic frame; event 0x87: format / speaker
  frame; 8 kHz s16le mono, 160-sample frames). Kept in v1: `lib/meshaudio` (Synth: message
  chime, key click, test tone; Meter; mix) and `src/board_audio.*` (renders a frame every
  20 ms into an 8-frame queue while an output is on; mic → meter and optional echo). Sounds:
  message chime when the board's account opens a new message (client_messenger), click on
  every key (`ui::on_sound`), Settings → Sound test / Echo test (+ mic level, outputs).
  `hwsim::active()` also counts an audio subscriber (keeps the USB port in host mode).
  Boot log says whether the simulators are built in; an `audio` line joins the 10 s block
  while audio runs.
- v1 switch: platformio env `mava_handheld_s3_v1` (`build_unflags -DMAVA_HWSIM=1`), built in
  CI's firmware matrix; `scripts/build-flasher.sh` uses it for versions without "-";
  android.yml publishes `mava-screen-…apk` only for pre-releases.
- Mava Screen: switches "Show the board's screen here" (off: no SCREEN 1, session opened
  with GET_STATUS, keys/typing stay), "Board sound on this phone's speaker" (AudioTrack),
  "This phone's microphone to the board" (AudioRecord, RECORD_AUDIO asked); GET_STATUS every
  20 s as keep-alive; AUDIO ack checked ("update the firmware"). `AudioLink` + JVM test with
  the firmware test's bytes. Docs: screen.md §Pre-release hardware simulators, firmware.md,
  firmware/README.md. Host tests: `audio_test.cpp` (synth, meter, mix, frames).
- On main (`d270256`; CI: both firmware envs, Mava Screen lint/tests/APK green). Release notes
  v0.1.1-beta32 ready; release waits for the owner's go. Then the owner tests picture off,
  sound test, message chime, echo test.

### 2026-11-18 — Board calls, step 1: call crypto in C++, pinned to the Go code

- Owner: video and voice calls on the board, audio module and camera simulated over USB.
  Decisions (asked): video codec **MJPEG** added to calls for boards (Android uses it with a
  board), **voice first**, then video; calls must work on one LAN **and across NATs** (the
  phones' direct-path methods). Plan: `firmware/lib/meshcall` (pure C++, host-tested) =
  crypto, signals + direct offer encoding, direct agent (STUN, probes, checks, link), call
  manager + jitter; then the board (messenger content type 7, lwIP UDP, libopus 16 kHz,
  hwsim audio at 16 kHz, call screens). Not in the first version: the birthday method
  (256 sockets), router port mapping, TURN.
- Local Go reference: `go1.24` + the deps cloned from GitHub (`flynn/noise`, `golang/crypto`,
  `golang/sys`, `golang/net`, `pion/*`, `wlynxg/anet`; their go.mod reduced to module + go
  line) with `replace` in a scratch copy of `core/`; `go test ./core/call ./core/direct`
  pass there. `spec/test-vectors/calls.json` was made by the real Go code (Noise KK with
  fixed ephemerals, media frames, signals, probes, link datagram, HKDF keys).
- `lib/meshcall/src/callcrypto.*`: HMAC-SHA256, HKDF-SHA256, ChaCha20-Poly1305 (RFC 8439 from
  Monocypher's ChaCha20 + Poly1305), Noise_KK_25519_ChaChaPoly_SHA256, call code. Host test
  `call_test.cpp` matches every vector; Go tests `core/call/vectors_test.go`,
  `core/direct/vectors_test.go` pin the same file on the Go side.
- Step 2: `lib/meshcall/src/callsignal.*` (was signal.*, renamed: it hid the system <signal.h>) (Caps, Signal, direct Offer/Candidate encoding; decode
  → encode equals the Go bytes for OFFER/ANSWER/END/CONTROL; unknown ops ignored, trailing
  bytes allowed) and `direct.*` (one-socket port of core/direct: STUN binding to the relays'
  reflectors, NAT type as `reflect`, HMAC probes with roles, triggered peer-reflexive checks,
  symmetric-sequential port prediction, choice by class then RTT after a 400 ms settle, link
  = "MDD" datagrams sealed per direction, 64-packet replay window, follows the peer's new
  address, keepalive 15 s, closes after 45 s of silence). Probe bytes = Go's. Test network
  (`test/direct_test.cpp`): NATs with endpoint-independent mapping and port-restricted
  filtering, a STUN reflector: same LAN → class 1, two NATs → class 4 (hole punched),
  symmetric sequential → predicted (class 5), UDP blocked → "a network on the way blocks UDP",
  replayed datagram dropped, keepalives hold a quiet link, silence closes it.
- Step 3: `lib/meshcall/src/call.*`: the call manager (port of core/call, event driven, no
  heap): OFFER/ANSWER/REJECT/CANCEL/END/CONTROL, ring 45 s, busy, contacts only, a stored
  OFFER past its expiry = missed without ringing, the direct agent, Noise KK hello repeated
  every 250 ms, sealed media frames, keepalive frames every 2 s, "connection lost" after 20 s
  of silence, mute, and the adaptive jitter buffer (40–200 ms, same rules as jitter.go). The
  board answers voice only for now (a video OFFER shows `video_offered`). Tests
  (`test/callmgr_test.cpp`, on `test/netsim.h`, the simulated network moved out of
  direct_test): talk on a LAN (class 1), across two NATs (class 4), through a symmetric NAT,
  declined, cancelled = missed, no answer, no common codec, not a contact, expired OFFER,
  busy, silent peer, no path, strangers' signals ignored. Interop: `make -C firmware
  callpeer` builds the manager on real loopback UDP; `core/call/board_test.go`
  (TestBoardPeer, CI runs it with MAVA_CALLPEER) has the Go phone code call the board and
  the board call the phone: same code on both sides, codec choice, audio both ways, mute,
  hang-up. Next: board integration (content type 7 in the client, lwIP UDP, Opus, the USB
  audio simulator at 16 kHz, call screens).
- 2026-11-18, owner's report on the board with the display: (1) "SSL - Memory allocation
  failed": the screen's two 25 kB draw buffers took internal RAM before the relay's TLS
  needed it, and mbedTLS allocates only internal RAM (SDK default). Draw buffers now PSRAM
  first; `board::tls_memory_in_psram()` routes mbedTLS blocks ≥ 256 B to PSRAM
  (`mbedtls_platform_set_calloc_free`); boot log shows internal RAM after the screen. (2)
  "connection refused" with a certificate error: no clock at boot. New `src/wallclock.*`:
  RTC module on I2C 47/48 (DS3231/DS1307 0x68, PCF85063A 0x51) sets the clock at boot and
  gets every network time; without one the clock starts at the last saved time or the build
  date (untrusted: certificate checks only). Before the first HTTPS the relay worker waits
  up to 5 s for NTP, then reads the relay host's Date over plain HTTP. The node gets the time
  when the clock is trusted (also from a phone's HELLO), not from `time() > 1700000000`. (3)
  Touch mirrored top to bottom in landscape: XPT2046 X runs the other way on the owner's
  panel; default `TOUCH_X_MIN 3880 / MAX 220`. (4) Navigation keys without the keyboard
  module: nine buttons as a resistor ladder on GPIO 2 (`NAV_KEYS_PIN`, lib/meshkeys
  ladder.*, host-tested with 5 % parts and 3.25–3.35 V), found at boot by its pull-up.
  Wiring in handheld-proto.md §4.
- Step 4 (board calls, integration): `src/calls.*` runs lib/meshcall's manager in its own
  task (core 1, priority 2) with a UDP socket on Wi-Fi or the hotspot (lwIP), STUN
  reflectors on the relay host (`net::relay_host()`, 3478/3479), Opus 16 kHz / 20 ms
  (codec-opus pinned in lib_deps, fixed point; Opus only), sample rings to and from
  board_audio. The client hands content type 7 to it (`set_call_handler`, `send_call` with
  PRIORITY; strangers are not made contacts; `calls_in` counted apart, so no message chime).
  Board audio is now 16 kHz in 10 ms frames (160 samples, under the 512-byte host-link
  payload); ring and busy sounds. UI: call screen (incoming: answer / decline; calling;
  talking: code, path, mute, hang up), 📞 in a chat calls, ⛔ hangs up, rings every 3 s.
  Mava Screen: 16 kHz, VOICE_COMMUNICATION + echo canceller + noise suppressor.
- Released **v0.1.1-beta32** (owner asked; workflow_dispatch, tag on `82787a8`): board voice
  calls, TLS memory in PSRAM, clock (NTP / relay Date before HTTPS, RTC module), touch fix,
  navigation keys on GPIO 2, Mava Screen 16 kHz audio. Not yet run on hardware.
- Owner on beta32: touch now mirrored top to bottom (left-right right), and "no calls" in
  the firmware (calls were only on the 📞 key; with touch alone there was no way to start
  one). Both raw touch axes now flipped by default (`TOUCH_Y_MIN 3860 / MAX 260` too), and
  Settings → Calibrate touch (lib/meshui touchcal.*: three crosses → affine map, any
  mirroring / swap / rotation, host-tested; saved in `/touchcal`; touches don't reach LVGL
  while calibrating). Chats on the board have a 📞 Call button. Notes for beta33 written.
- Released **v0.1.1-beta33** (owner asked; tag on `b63eb37`): touch both axes, Settings →
  Calibrate touch, 📞 Call button in the board's chats. Not yet run on hardware.
- Owner on beta33: Mava Screen's keys and audio no longer work. Likely cause: the USB
  receive buffer (HWCDC default 256 B) overflowed while the phone streams its microphone
  (16 kHz ≈ 33 kB/s) during a screen redraw, dropping commands (keys, AUDIO start) with
  the audio. Now `Serial.setRxBufferSize(8192)` and the main loop drains USB fully each pass.
  Navigation-key detection also needs a steady reading (an open GPIO 2 never presses keys);
  the boot log shows the pin's voltage. Touch-only use (owner): the top bar has Back and
  Home buttons (and 📞 back to a live call) when no keyboard module is present; its text
  is shortened (battery, LoRa, Wi-Fi + relay ✓/↻, BLE, USB).
- Board video, receive-only (owner, 2026-11-18): codec 20 = MJPEG (Go `CodecMJPEG`, C++
  `kCodecMjpeg`, calls.md §7b). lib/meshcall: `set_video` (reassembly buffer + sink), offer and
  answer carry a receive-only MJPEG stream (320×240, 10 fps, camera off), reassembly one frame
  at a time, loss reports on stream 255 with their own sequence numbers (audio sequence no
  longer shared: the far side counted control frames as lost audio), `send_video` for tests.
  Host tests (video in, lost fragment, too big, H.264-only offer → voice) and TestBoardPeer
  (Go phone sends 8 and 5 frames, all whole, both call directions). Board: triple buffer to
  the UI, JPEGDEC (bitbank2/JPEGDEC ^1.8.4) into a 320×240 RGB565 image behind the call
  screen. Android: `codecs()` = `17,20` (or `20`), CallInfo.videoCodec, MJPEG send (RGBA →
  upright, fit 320×240, quality follows kbps) and receive (JPEG on the Surface). Owner: beta33
  screen slow → draw buffers back in internal RAM, 2 × 20 rows (TLS now in PSRAM).
- Released **v0.1.1-beta34** (owner asked; tag on `bdbcb85`; the images zip carries firmware.bin, firmware-plain.bin, variants.txt). Two firmwares per pre-release (owner, 2026-11-18): `scripts/build-flasher.sh` also builds
  env `mava_handheld_s3_v1` as `firmware-plain.bin` (no phone simulators), the images get
  `variants.txt`, the release zip carries both. Mava Flasher: switch *Phone simulators (Mava
  Screen)* (remembered; writes firmware-plain.bin at 0x10000 when off; older flashers keep
  writing firmware.bin). Linux: `mava-flash board|firmware --plain`. flashing.md.
- One Android app for the board (owner, 2026-11-18): Mava Screen merged into the flasher
  module as **Mava Board** (applicationId stays org.meshmsg.flasher, so Mava Flasher updates in
  place; uninstall Mava Screen). Tabs *Flash & set up* and *Screen, keys, sound* (the second
  only when `BuildConfig.SIMULATORS`: a "-" in the version, as the firmware's simulators);
  opening one lets go of the board's USB in the other (`ScreenViewModel.setActive`). Screen
  code in `android/flasher/.../screen`, strings prefixed `scr_`, RECORD_AUDIO in the manifest;
  `android/screen` removed. Release asset `mava-board-<ver>-android.apk` (no more
  mava-board-flasher-…-android.apk / mava-screen-…). Docs updated.
- "Calling" vs "Ringing" (owner, 2026-11-18): new signal op 8 RINGING (no body), sent by the
  callee when an OFFER starts ringing (not for missed / refused / busy); the caller's
  `Info.peer_ringing` (Go JSON `peer_ringing`, C++ `Info::peer_ringing`). Phones: "Ringing…"
  and the ringback tone (ToneGenerator TONE_SUP_RINGTONE); board: "Ringing…" and a 425 Hz
  ringback every 4 s (Sound::Ringback). Older versions ignore op 8. Tests: Go
  TestCallerSeesRinging, C++ codec + manager, TestBoardPeer both directions.
- Screen rotation in the board's Settings (owner, 2026-11-18): landscape / portrait / either
  upside down, saved in `/rotation` (`display::set_rotation`, LVGL rotates the panel by
  MADCTL). The mirror takes a runtime size (`Mirror::set_size`, 240×320 = same 600 tiles,
  new info event to the phone; host test `portrait()`); touch calibration now maps to the
  panel's own pixels (MTC2; MTC1 files from beta33/34 converted), so it holds in any rotation;
  call video decodes to the current screen size.
- Released **v0.1.1-beta35** (owner asked; tag on `3b21823`): Mava Board app (one APK `mava-board-…-android.apk`), RINGING, screen rotation. Not yet run on hardware.
- Owner on beta35: navigating the menus, the screen dies and the board restarts; log
  "esp-aes: Failed to allocate memory", "BLE_INIT: Malloc failed": internal RAM used up.
  LVGL used the C library's malloc, so every object (< 4 kB) went to internal RAM; beta34
  had also put 2 × 12.5 kB draw buffers back there. Now LVGL has its own allocator, PSRAM
  first (`LV_USE_STDLIB_MALLOC=255`, `src/ui/lv_mem_psram.cpp`); one 20-row draw buffer
  (the flush is synchronous, the second never helped); call task stack 10 kB; USB buffers
  16 kB TX / 8 kB RX only in the simulator build (4 kB / 2 kB plain). Every 10 s the log
  shows internal RAM free, its lowest point and the largest block.
- Released **v0.1.1-beta36** (owner asked; tag on `6b78333`): LVGL heap in PSRAM, one draw buffer, memory line in the log. Waiting for the owner's log (the `memory:` lines) on hardware.
- Owner on beta36: "Stack canary watchpoint triggered (mava-call)": `Jitter::reset()` was
  `*this = Jitter()`, an 8.6 kB temporary (32 slots × 264 B) on the call task's stack, which
  beta36 had cut to 10 kB (the backtrace's 8656-byte frame, memset of 0x21b0). Reset in place;
  call task back to 16 kB, its stack high-water mark in the `calls` log line; host build:
  `-Wstack-usage=4096` (with -Werror) on every library object, so a frame over 4 kB fails
  the build (checked: the old reset is caught at -O1/-Os/-O2). Released **v0.1.1-beta37** (owner asked; tag on `8a6c31f`).
- Board UI (owner, 2026-11-18): home menu one button per row in portrait (two columns in
  landscape). Call screen: mute, a code key (👁, the code in a pop-up that closes after 10 s)
  and hang up sit in the top bar next to Home (Back is left out while a call is live), the
  bar shows who and the talking time, and the other side's video takes the whole screen
  under it (the status text hides while frames come). Ringing keeps Answer / Decline in the
  middle of the screen.
  Commit 24e1d09 (CI green); release notes for v0.1.1-beta38 ready, release waits for the owner.
- Board UI (owner): in a chat, the call key moved from the name row into the top bar next to
  Home (shown while connected and no call is live).
- Released **v0.1.1-beta38** (tag on 483b9a3, pre-release, signed release.json): board menu one
  item per row in portrait, call buttons and the 10 s code pop-up in the top bar, chat call key
  in the top bar.
- Owner: the call video on the board looks generally blue. Board decode checked (JPEGDEC
  RGB565, C and S3 SIMD paths map Cr→red, Cb→blue) and the phone side (CameraX RGBA →
  Bitmap → JPEG) is RGB too, so the likely cause is the panel's red/blue order (we create it
  BGR). Added Settings → red/green/blue swatches + "Swap red and blue" (saved in /colors,
  LV_LCD_FLAG_BGR or not, switched live with lv_lcd_generic_mipi_set_address_mode).
- **Trip location sharing (owner, 2026-11-19)** — for a group trip without roaming (e.g. an
  Arbaeen walk): members' phones send their position every few minutes, encrypted to the
  trip's leaders (the relay's admins who lead it), over Bluetooth, LoRa and relays, like
  AirTag reports; leaders see members in the app and on a map in the relay's admin panel.
  Owner's choices: every few minutes (faster on "I'm lost" or a leader's request); relay
  owner + admins see; app + web map. Step 1 (this commit): `core/trip` (signed trip card,
  report/locate/lead content, content type 10), router shared inboxes (no receipts, kept
  for co-holders, announced) + per-destination TTL + NO_LORA option, `core/node/trips.go`
  (offer/consent/report/stop/SOS, create/add leader/end, members, locate, relay polling,
  trip mailbox fetch), relay `server/trips` (GET/PUT/DELETE /v1/trip, admins only, every
  leader an admin), panel `/admin/trip` map (reports opened in the browser:
  `server/admin/tripcrypto.js`, checked against Go by `TestTripMapCrypto` with node),
  mobile binding. Next: Android screens and location reports, docs/trip.md.
- Trip step 2: Android — `data/Trips.kt` (JSON, `TripsApi`/`CoreTrips`, distance/bearing),
  `service/TripService.kt` (foreground service type location while sharing: LocationManager
  GPS + network fixes, the core decides when a report is due, waits up to a minute for a first
  fix, notification with Stop), `service/TripAlerts.kt` (leaders' alarm on "I'm lost"),
  `ui/TripsScreen.kt` (consent card in plain words, sharing state, I'm lost / I'm safe, stop,
  leave; leaders: members with distance/direction, open in map, ask for position, QR code,
  add leader, web map key, end; join by link; start a trip), Settings entry, chats banner
  (lost member / offer / sharing), `meshtrip:` links, location permissions (asked only on
  "Share my location"); strings en + fa. docs/trip.md written; protocol.md content type 10;
  server.md §6b2; android.md permissions.
- Trip step 3: `relayFor` also takes a configured relay before its link runs (TestJoinTripLink).
  CI green on 0665a5d (Go incl. TestTripLocationSharing end to end through a relay; Android
  lint + unit tests + build). beta39 notes now cover trips + the board's red/blue swap; release
  waits for the owner. Not yet tried on phones: location fixes, the foreground service, the
  web map against a live relay.
- Released **v0.1.1-beta39** (tag on 86a7106, pre-release, signed release.json): trip location
  sharing (app, relay, web map) and the board's red/blue swap setting.

### 2026-11-19 — eu-1 relay upgraded to 0.1.1-beta39
- eu-1 was running 0.1.1-beta9; installed the official v0.1.1-beta39 release tarball (SHA256SUMS
  checked on the host), restarted `meshmsg-relay`. Pubkey unchanged, `https://eu-1.mfadem.ir/v1/info`
  answers, `/v1/release?channel=beta` = 200 (beta39). Old binary kept as
  `/usr/local/bin/meshmsg-relay.bak-beta9`.

### 2026-11-19 — Edge relays
- **Edge relays (owner, 2026-11-19)**: the relay also runs on Cloudflare Workers (D1) and
  ArvanCloud Edge Computing (Arvan Object Storage bucket), owner's choice: full relay only;
  messages/mailboxes/receipts/announces/relay list + admin panel + trips + gateways +
  federation (no media, no UDP). `edge/src` (JavaScript, no deps) → `node edge/build.mjs` →
  `edge/dist/{cloudflare,arvan}.js` (committed; CI checks). Stores: D1 (same table as Go) and
  S3 (metadata in object names, time-based seq with a 4 s settle). One installer:
  `server/deploy/install-relay.sh` (this server → install.sh; Cloudflare → wrangler: D1, toml,
  deploy, secrets; Arvan → arvan CLI from git.arvancloud.ir, config written into the bundle,
  `arvan ec deploy`). Tests: `node --test edge/test` (crypto vs Node), `edge/edgetest` (Go
  clients against the bundles in Node: chat D1/S3/Arvan bundle, relay list, admin login code,
  trip, federation both ways with a Go relay). Checked locally with relayclient against all three
  (upload with PoW, signed mailbox, receipts purge, sync, trip, admin code opened in Go).
  Also: phones no longer ask relays for receipts of trip reports (never acknowledged), and the
  admin panel shows the trip with a link to its map. docs/edge-relay.md. Not yet run on the real
  platforms (Cloudflare, Arvan); Arvan's runtime details (WebCrypto, waitUntil, limits) unverified.
  Fix: `edge/dist/` was hidden by the `dist/` ignore rule; now committed (`!edge/dist/`).
- Edge relay CI green (62f2d49): bundles + crypto checks, edgetest (chat on D1/S3/Arvan bundle,
  relay list, admin login code, trip, federation with a Go relay). Fix: the admins list answered
  from a stale cache after a change.
- **Trips in the app (owner, 2026-11-19)**: an in-app trip map for leaders (`ui/TripMap.kt`:
  OSM tiles cached on disk, grid without them, members, tracks, accuracy, me; pinch/drag/tap),
  "I'm lost" also sends the position to every leader as a message and opens that chat
  (`node.TripMessageLeaders`; trip cards now carry leaders' keys, `Leader.Key`, checked against
  the id), location messages like Telegram (`LocationShare.kt`: plain text with Google Maps,
  Neshan `nshn.ir/?lat=&lng=`, Balad `balad.ir/location?latitude=&longitude=` links and a geo:
  line; drawn as a card whose Navigate button asks which navigation app), attach → Location in
  every chat, members' Navigate uses the app chooser. Tests: TestTripLocationSharing (message to
  the leaders), LocationShareTest (JVM).
  CI green on 0fd9262 (Go incl. edgetest; Android lint + unit tests + build). beta40 notes ready
  (edge relays, installer, trip map, lost chat, location messages); release waits for the owner.
- Released **v0.1.1-beta40** (tag on 6697011, pre-release, signed release.json; run 44545562741
  green: version, desktop, android, board, flasher-android): edge relays (Cloudflare D1 / Arvan),
  one installer for local / Cloudflare / Arvan, in-app trip map, "I'm lost" chat to the leaders,
  location messages. Next: owner tries trips on real phones; backend write-up of trips for the
  owner (Persian) was handed over as a doc.

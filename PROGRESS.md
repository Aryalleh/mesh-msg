# Progress log

Running log of work on mesh-msg, updated after every step and pushed with it, so an
interrupted session can be resumed from here. Newest entries at the bottom of the log.
Plan of record: [docs/roadmap.md](docs/roadmap.md).

## Current state

| Milestone | Status |
|---|---|
| M0 Spec | done — Go core and C++ firmware library both pass `spec/test-vectors/vectors.json` |
| M1 Core + sim | done |
| M2 Relay | done — relay `eu-1` (`http://87.106.5.192`, plain HTTP, no domain yet) runs and is the default relay in meshd and the Android app |
| M3 Linux desktop | done (`meshd` + `mesh` CLI + web UI) |
| M4 LoRa link | **in progress** — exit criterion met in simulation (`scripts/e2e-lora.sh`); board layer builds for all four boards (`pio run`), **not yet run on hardware** |
| M5 Android | **in progress** — core binding + app built and tested on the JVM (65 tests, screenshots); exit scenario passes in Go; **radio code never run on a phone** |
| M6–M8 | not started |

## Next step

Needs hardware now, for both open milestones:
- **M5**: install `android/app/build/outputs/apk/debug/app-arm64-v8a-debug.apk` on two phones;
  bring up `BleMesh` (advertise / scan / GATT both ways), then the exit scenario on real
  devices (phone A offline → BLE → phone B with a relay → desktop). Then USB node on OTG.
- **M4**: flash two boards, verify pin tables (firmware/README.md).

Software that can still be done without hardware: firmware BLE host link (docs/firmware.md §6)
+ Android LoRa-over-BLE, QR scanning, LoRa node settings screen, CP2102 USB driver, M6 items.

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

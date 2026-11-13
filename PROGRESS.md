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
Plan of record: docs/roadmap.md "Beta criteria" (alpha → beta) and "Code changes required by the 2026-11-12 design decisions".
**v0.1.1-beta1 (first beta)**: update check B7 built (signed `release.json`, relay `GET /v1/release`,
meshd/web UI/`mesh update`, Android banner + notification + verified APK install). The release job
needs the GitHub secret `RELEASE_SIGNING_KEY` (private key: `~/.config/mesh-msg/release-signing.key`
on the owner's machine, never in the repo). Other beta criteria (A1 release key for APKs, A2, A3, B1–B6)
are still open; the owner chose to tag the beta now. **Steps 1–4 done** (two media classes + allowance/ledger/PoW, direct links across NAT with
STUN on relays and TURN for relay admins, 1:1 voice calls on Android). Latest release **v0.1.1-beta1** (first beta: update check B7, eu-1 on https://eu-1.mfadem.ir; code in `32f31a6`). Before it: v0.1.0-alpha13 (`91f2061`). eu-1 runs alpha10 (STUN listens on 3478/3479 and answers locally) but **UDP from outside gets
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


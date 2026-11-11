# Progress log

Running log of work on mesh-msg, updated after every step and pushed with it, so an
interrupted session can be resumed from here. Newest entries at the bottom of the log.
Plan of record: [docs/roadmap.md](docs/roadmap.md).

## Current state

| Milestone | Status |
|---|---|
| M0 Spec | done — Go core and C++ firmware library both pass `spec/test-vectors/vectors.json` |
| M1 Core + sim | done |
| M2 Relay | done |
| M3 Linux desktop | done (`meshd` + `mesh` CLI + web UI) |
| M4 LoRa link | **in progress** — exit criterion met in simulation (`scripts/e2e-lora.sh`); board layer builds for all four boards (`pio run`), **not yet run on hardware** |
| M5 Android | **in progress** — gomobile binding `mobile/` (package `meshcore`) done and tested in Go incl. the exit scenario; Android app next |
| M6–M8 | not started |

## Next step

M5 Android (docs/android.md). Done: `mobile/` binding. Next, in order:
1. Android toolchain in the container (SDK 35, build-tools, NDK; `gomobile bind` → AAR). Needs
   the user's OK for the Android SDK licence (the auto-accept was blocked).
2. `android/` Gradle project: Compose UI from the design system (`android/DESIGN.md`),
   `MeshService` (foreground), BLE mesh transport (GATT server + client → `Core.AddTransport`),
   LoRa node over USB-OTG serial (`Core.AttachLoRa`); BLE host link needs firmware work (§6).
3. Build the APK; screenshot tests for the UI (no emulator: the container has no KVM).

M4 hardware bring-up is still open (flash two boards, verify pin tables) — needs boards.

## Resuming in a fresh container

- Go is not preinstalled: download the latest from `https://go.dev/VERSION?m=text` into
  `~/sdk/go` and use `export PATH=$HOME/sdk/go/bin:$PATH` (go.mod needs ≥ 1.26).
- Checks: `go build ./... && go test ./...`; `make -C firmware test` (g++ with ASan/UBSan);
  end-to-end over simulated LoRa: `scripts/e2e-lora.sh` (~30 s; `NODES=5 SIMFLAGS="-region 0"`);
  boards: `cd firmware && pio run` (needs `pip install platformio`; the registry hosts
  `api.registry.platformio.org`, `dl.registry.platformio.org`, `dl.platformio.org`,
  `github.com`, `objects.githubusercontent.com` must be reachable — the cloud environment
  "mesh-msg" allows them; first build downloads ~1 GB of toolchains).
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

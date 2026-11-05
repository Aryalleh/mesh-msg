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
| M4 LoRa link | **in progress** — `lib/meshproto` + `lib/meshroute` done; MAC/store next |
| M5–M8 | not started |

## Next step

Firmware, board-independent modules with host tests (`make -C firmware test`):
1. `mac/Airtime` — LoRa airtime calculation (Semtech formula, presets of docs/firmware.md §4),
   duty-cycle budget over a sliding hour, priority queue
   RECEIPT > DATA(PRIORITY) > DATA > SUMMARY > ANNOUNCE (use `route::Priority`), announce
   airtime share ≤ 10 %, CAD backoff hooks. Consumes `route::Router::poll()` frames and
   calls `on_transmitted()`; mirror `sim/lora.go` budget logic.
2. `store/` — bounded ring-buffer packet store with expiry/quotas (flash backend later).
3. Then `platformio.ini` (heltec_v3, tbeam_s3, rak4631, techo + native) and the RadioLib
   radio layer → firmware milestone 1 (two boards exchange raw packets).

## Resuming in a fresh container

- Go is not preinstalled: download the latest from `https://go.dev/VERSION?m=text` into
  `~/sdk/go` and use `export PATH=$HOME/sdk/go/bin:$PATH` (go.mod needs ≥ 1.26).
- Checks: `go build ./... && go test ./...`; `make -C firmware test` (g++ with ASan/UBSan).
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

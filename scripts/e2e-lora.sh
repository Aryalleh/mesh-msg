#!/usr/bin/env bash
# End-to-end LoRa test without hardware (roadmap M4 exit criterion):
#   PC A (meshd) → node 0 → node 1 (repeater) → node 2 → PC B (meshd), and the
#   delivery receipt back the same way.
# The nodes are the real firmware (node::Node) on a simulated radio
# (firmware/build/meshnode-sim, line topology: node 0 cannot hear node 2).
# meshd runs with LAN off, so LoRa is the only path between the two PCs.
#
#   scripts/e2e-lora.sh            # needs go, a C++ compiler, make
#   KEEP=1 scripts/e2e-lora.sh     # keep the work dir (logs) afterwards
#   NODES=5 SIMFLAGS="-region 0" scripts/e2e-lora.sh   # longer chain, EU_868 duty cycle
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
TIMEOUT=${TIMEOUT:-180}
NODES=${NODES:-3}
LAST=$((NODES - 1))
((NODES >= 3)) || { echo "NODES must be ≥ 3 (PC A, repeater(s), PC B)"; exit 2; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/e2e-lora.XXXXXX")
PIDS=()
cleanup() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  if [[ "${KEEP:-}" == 1 || "${FAILED:-}" == 1 ]]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT
fail() { FAILED=1; echo "FAIL: $*"; for f in "$WORK"/*.log; do echo "--- $f"; tail -n 40 "$f"; done; exit 1; }

echo "== build"
make -s -C firmware sim
go build -o "$WORK/meshd" ./desktop/cmd/meshd
go build -o "$WORK/mesh" ./desktop/cmd/mesh

echo "== start $NODES simulated nodes (line 0-…-$LAST, interior nodes are repeaters)"
"$ROOT/firmware/build/meshnode-sim" -n "$NODES" -topology line -v ${SIMFLAGS:-} >"$WORK/sim.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 100); do grep -q '^ready' "$WORK/sim.log" && break; sleep 0.1; done
grep -q '^ready' "$WORK/sim.log" || fail "simulator did not start"
PTS0=$(awk '/^node 0: \/dev/ {print $3}' "$WORK/sim.log")
PTS2=$(awk -v n="node $LAST:" '$1" "$2 == n && $3 ~ /^\/dev/ {print $3}' "$WORK/sim.log")
grep -m1 '^meshnode-sim' "$WORK/sim.log"
echo "node 0: $PTS0, node $LAST: $PTS2"

start_meshd() { # name pts
  mkdir -p "$WORK/$1"
  cat >"$WORK/$1/config.toml" <<CFG
name = "$1"
delegate = false
announce_minutes = 30
ui = ""
[gateway]
enabled = false
[lora]
enabled = true
port = "$2"
[lan]
enabled = false
CFG
  "$WORK/meshd" -v -config "$WORK/$1/config.toml" -data "$WORK/$1" >"$WORK/$1.log" 2>&1 &
  PIDS+=($!)
}
mesh() { local who=$1; shift; "$WORK/mesh" -data "$WORK/$who" "$@"; }

echo "== start meshd A on node 0, meshd B on node $LAST (LAN off)"
start_meshd alice "$PTS0"
start_meshd bob "$PTS2"
for who in alice bob; do
  for _ in $(seq 100); do mesh "$who" status >/dev/null 2>&1 && break; sleep 0.1; done
  mesh "$who" status >/dev/null 2>&1 || fail "meshd $who did not start"
done

echo "== exchange contacts (out of band, as with a QR code)"
mesh alice add "$(mesh bob id)" bob
mesh bob add "$(mesh alice id)" alice

TEXT="salam az LoRa $(date +%s)"
echo "== alice → bob: $TEXT"
mesh alice send bob "$TEXT"

deadline=$((SECONDS + TIMEOUT))
until mesh bob read alice | grep -qF "$TEXT"; do
  ((SECONDS < deadline)) || fail "bob did not receive the message within ${TIMEOUT}s"
  sleep 1
done
echo "bob received it after ${SECONDS}s"
until mesh alice read bob | grep -F "$TEXT" | grep -q '✓✓'; do
  ((SECONDS < deadline)) || fail "alice got no delivery receipt within ${TIMEOUT}s"
  sleep 1
done
echo "alice got the delivery receipt after ${SECONDS}s"

REPLY="javab $(date +%s)"
echo "== bob → alice: $REPLY"
mesh bob send alice "$REPLY"
until mesh alice read bob | grep -qF "$REPLY"; do
  ((SECONDS < deadline)) || fail "alice did not receive the reply within ${TIMEOUT}s"
  sleep 1
done
echo "alice received the reply after ${SECONDS}s"

# The end nodes cannot hear each other, so every repeater must have carried
# data and receipts.
for i in $(seq 1 $((LAST - 1))); do
  for t in DATA RECEIPT; do
    grep -q "^node $i tx $t" "$WORK/sim.log" || fail "repeater (node $i) never transmitted a $t packet"
  done
done
grep -E '^node [0-9]+ tx' "$WORK/sim.log" | sed 's/^/  sim: /'
echo "PASS: text crossed PC → node → $((NODES - 2)) repeater(s) → node → PC and back"

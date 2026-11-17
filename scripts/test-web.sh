#!/usr/bin/env bash
# Browser test of the web client (docs/web-access.md): builds it, starts
# web/cmd/devboard (board link stand-in + relay), and drives two headless
# Chromium windows with Playwright (web/test/browser.js). Optional argument:
# a directory for screenshots.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$tmp"' EXIT
OUT="$tmp/web" GZIP_FILES=0 scripts/build-web.sh > /dev/null
go build -o "$tmp/devboard" ./web/cmd/devboard  # a binary, so the trap really stops it
"$tmp/devboard" -dir "$tmp/web" -board 127.0.0.1:8080 -relay 127.0.0.1:8081 > "$tmp/devboard.log" 2>&1 &
for _ in $(seq 50); do curl -sf -o /dev/null http://127.0.0.1:8080/app/index.htm && break; sleep 0.2; done
NODE_PATH="${NODE_PATH:-$(npm root -g)}" node web/test/browser.js "${1:-}"

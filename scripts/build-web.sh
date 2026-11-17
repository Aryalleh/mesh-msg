#!/usr/bin/env bash
# Builds the browser client (docs/web-access.md) into firmware/data/web/,
# gzipped, for the Mava board's LittleFS: `cd firmware && pio run -t uploadfs`.
#   OUT=dir scripts/build-web.sh   to put the plain files somewhere else.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT="${OUT:-firmware/data/web}"
GZIP="${GZIP_FILES:-1}"
mkdir -p "$OUT"
rm -f "$OUT"/*
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$OUT/main.wasm" ./web/cmd/webclient
root="$(go env GOROOT)"
if [ -f "$root/lib/wasm/wasm_exec.js" ]; then cp "$root/lib/wasm/wasm_exec.js" "$OUT/"; else cp "$root/misc/wasm/wasm_exec.js" "$OUT/"; fi
cp web/static/index.htm web/static/app.js "$OUT/"
mkdir -p "$OUT/fonts" && cp web/static/fonts/*.woff2 "$OUT/fonts/"
if [ "$GZIP" = 1 ]; then
  for f in "$OUT"/*.*; do gzip -9 -n -f "$f"; done
fi
ls -la "$OUT"

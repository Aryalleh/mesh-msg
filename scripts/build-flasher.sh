#!/usr/bin/env bash
# Builds the Linux board flasher bundle (docs/flashing.md):
#   dist/mava-board-flasher-<version>-linux.tar.gz
# with the ESP32-S3 firmware + web client file system, the three keyboard-module
# images, Espressif's standalone esptool (x86-64) and tools/flasher/mava-flash.
#   VERSION=0.1.1-beta7 scripts/build-flasher.sh
# Needs Go, PlatformIO and network access to PlatformIO's registry and GitHub.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-dev}"
ESPTOOL_VERSION=4.8.1
name="mava-board-flasher-$VERSION-linux"
out="dist/$name"
rm -rf "$out" && mkdir -p "$out/images" "$out/tools"

scripts/build-web.sh > /dev/null
(cd firmware && pio run -e mava_handheld_s3 > /tmp/mava-pio-board.log 2>&1 || { tail -n 60 /tmp/mava-pio-board.log; exit 1; })
(cd firmware && pio run -e mava_handheld_s3 -t buildfs > /tmp/mava-pio-fs.log 2>&1 || { tail -n 60 /tmp/mava-pio-fs.log; exit 1; })
(cd firmware/keyboard && pio run > /tmp/mava-pio-kb.log 2>&1 || { tail -n 60 /tmp/mava-pio-kb.log; exit 1; })

b=firmware/.pio/build/mava_handheld_s3
cp "$b/bootloader.bin" "$b/partitions.bin" "$b/firmware.bin" "$b/littlefs.bin" "$out/images/"
cp "$HOME/.platformio/packages/framework-arduinoespressif32/tools/partitions/boot_app0.bin" "$out/images/"
for kb in matrix bb9900 bb9900tp; do
  cp "firmware/keyboard/.pio/build/kb_$kb/firmware.uf2" "$out/images/keyboard-$kb.uf2"
done

zip="$(mktemp -d)/esptool.zip"
curl -fsSL -o "$zip" "https://github.com/espressif/esptool/releases/download/v$ESPTOOL_VERSION/esptool-v$ESPTOOL_VERSION-linux-amd64.zip"
unzip -q -o "$zip" -d "$(dirname "$zip")"
cp "$(dirname "$zip")/esptool-linux-amd64/esptool" "$out/tools/esptool"
chmod +x "$out/tools/esptool"
cp "$(dirname "$zip")/esptool-linux-amd64/LICENSE" "$out/tools/esptool-LICENSE" 2>/dev/null || true

cp tools/flasher/mava-flash "$out/"
cp docs/flashing.md "$out/README.md"
echo "$VERSION" > "$out/VERSION"
(cd "$out/images" && sha256sum * > SHA256SUMS)
tar -C dist -czf "dist/$name.tar.gz" "$name"
ls -la "dist/$name.tar.gz"

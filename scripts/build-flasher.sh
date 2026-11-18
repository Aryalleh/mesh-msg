#!/usr/bin/env bash
# Builds the Linux board flasher bundle (docs/flashing.md):
#   dist/mava-board-flasher-<version>-linux.tar.gz
# with the ESP32-S3 firmware + web client file system, the three keyboard-module
# images, Espressif's standalone esptool (x86-64) and tools/flasher/mava-flash.
# Also dist/board-images/ (+ .tar): the image set the Android flasher bundles
# (android/flasher, -PboardImages=dist/board-images).
#   VERSION=0.1.1-beta7 scripts/build-flasher.sh
# Needs Go, PlatformIO and network access to PlatformIO's registry and GitHub.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-dev}"
ESPTOOL_VERSION=4.8.1
name="mava-board-flasher-$VERSION-linux"
out="dist/$name"
rm -rf "$out" && mkdir -p "$out/images" "$out/tools"

# Pre-releases (a "-" in the version: 0.1.1-beta31) and dev builds carry two
# firmwares and the flashers let the user choose (owner, 2026-11-18):
#   firmware.bin        with the hardware simulators (src/hwsim: Mava Screen's
#                       screen, keys and audio over USB), the default
#   firmware-plain.bin  without them (env mava_handheld_s3_v1): less work
#                       per loop, for a board with its own display and keys
# A stable version (1.0.0) is built without them only (firmware.bin).
case "$VERSION" in
  *-* | dev) env=mava_handheld_s3 plain=mava_handheld_s3_v1 ;;
  *) env=mava_handheld_s3_v1 plain="" ;;
esac
echo "board firmware: env $env${plain:+, and $plain as firmware-plain.bin}"

scripts/build-web.sh > /dev/null
(cd firmware && pio run -e "$env" > /tmp/mava-pio-board.log 2>&1 || { tail -n 60 /tmp/mava-pio-board.log; exit 1; })
(cd firmware && pio run -e "$env" -t buildfs > /tmp/mava-pio-fs.log 2>&1 || { tail -n 60 /tmp/mava-pio-fs.log; exit 1; })
if [ -n "$plain" ]; then
  (cd firmware && pio run -e "$plain" > /tmp/mava-pio-plain.log 2>&1 || { tail -n 60 /tmp/mava-pio-plain.log; exit 1; })
fi
(cd firmware/keyboard && pio run > /tmp/mava-pio-kb.log 2>&1 || { tail -n 60 /tmp/mava-pio-kb.log; exit 1; })

b="firmware/.pio/build/$env"
cp "$b/bootloader.bin" "$b/partitions.bin" "$b/firmware.bin" "$b/littlefs.bin" "$out/images/"
[ -n "$plain" ] && cp "firmware/.pio/build/$plain/firmware.bin" "$out/images/firmware-plain.bin"
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

# Android flasher images: the bootloader with the flash settings written into
# its header (as esptool write_flash --flash_mode dio --flash_freq 80m
# --flash_size 16MB does) merged with the partition table, so the phone writes
# exactly what the Linux flasher writes.
ai=dist/board-images
rm -rf "$ai" && mkdir -p "$ai"
"$out/tools/esptool" --chip esp32s3 merge_bin -o "$ai/boot.bin" --flash_mode dio --flash_freq 80m --flash_size 16MB \
  0x0 "$b/bootloader.bin" 0x8000 "$b/partitions.bin" > /dev/null
cp "$out/images/boot_app0.bin" "$b/firmware.bin" "$b/littlefs.bin" "$ai/"
printf '0x0 boot.bin boot\n0xe000 boot_app0.bin boot\n0x10000 firmware.bin app\n0x810000 littlefs.bin fs\n' > "$ai/images.txt"
# The second firmware: flashers that know variants.txt offer it in place of
# firmware.bin (older ones just write firmware.bin).
if [ -n "$plain" ]; then
  cp "$out/images/firmware-plain.bin" "$ai/"
  printf '# <app image> <name>: the firmware variants, the first is the default\nfirmware.bin simulators\nfirmware-plain.bin plain\n' > "$ai/variants.txt"
fi
echo "$VERSION" > "$ai/VERSION"
tar -C dist -cf dist/board-images.tar board-images

cp tools/flasher/mava-flash "$out/"
cp docs/flashing.md "$out/README.md"
echo "$VERSION" > "$out/VERSION"
(cd "$out/images" && sha256sum * > SHA256SUMS)
tar -C dist -czf "dist/$name.tar.gz" "$name"
ls -la "dist/$name.tar.gz"

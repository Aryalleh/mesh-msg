# Flashing the Mava board from Linux

The release has `mava-board-flasher-<version>-linux.tar.gz`: everything needed, no
internet and no PlatformIO required.

```
tar xzf mava-board-flasher-*-linux.tar.gz
cd mava-board-flasher-*-linux
./mava-flash board          # first time on a new board: ./mava-flash board --erase
./mava-flash log            # see it start: device password, BLE passkey, what it hears
```

| Command | What it writes |
|---|---|
| `./mava-flash board` | firmware and the web client (file system) |
| `./mava-flash board --erase` | erases everything first: **new identity and device password**. Needed once on boards flashed before 0.1.1-beta7 (new partition table). |
| `./mava-flash firmware` | firmware only (keeps identity, settings, stored messages) |
| `./mava-flash web` | web client file system only — it also holds identity and settings, so they are replaced |
| `./mava-flash keyboard matrix` / `bb9900` / `bb9900tp` | the keyboard module (Raspberry Pi Pico) of version A / B / C: hold **BOOTSEL** while plugging the Pico in |
| `./mava-flash log` | the board's USB log |
| `./mava-flash info` | chip and flash size (checks the connection) |

Options: `--port /dev/ttyACM0` (default: the first Espressif USB device), `--dry-run` (print
the commands only), `--baud 460800` (if writing fails on a long cable).

## Permissions

The serial port belongs to the `dialout` group:

```
sudo usermod -aG dialout $USER     # then log out and in once
```

## What is inside

```
mava-flash                  the script
images/bootloader.bin       0x0
images/partitions.bin       0x8000   (2 × 4 MB app slots, 7.9 MB LittleFS)
images/boot_app0.bin        0xe000
images/firmware.bin         0x10000
images/littlefs.bin         0x810000 (the browser client, docs/web-access.md)
images/keyboard-*.uf2       keyboard module, one per version
images/SHA256SUMS
tools/esptool               Espressif's standalone esptool (x86-64 Linux)
```

On a Raspberry Pi or another ARM Linux, install esptool once (`pip install esptool`); the
script uses it when the bundled one does not run.

If the board does not answer: hold **BOOT**, press and release **RESET**, release BOOT
(download mode), and run the command again.

## Building it yourself

`VERSION=dev scripts/build-flasher.sh` (Go, PlatformIO, network) → `dist/`.

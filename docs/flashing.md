# Flashing the Mava board (Linux, Android)

Two flashers write the same images: `mava-board-flasher-<version>-linux.tar.gz` on a
computer, and `mava-board-flasher-<version>-android.apk` on a phone ([below](#android)).

## Linux

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

### Permissions

The serial port belongs to the `dialout` group:

```
sudo usermod -aG dialout $USER     # then log out and in once
```

### What is inside

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

## Android

`mava-board-flasher-<version>-android.apk` (Android 8+) carries the board firmware and
writes it from the phone, no computer needed:

1. Install the APK (allow installs from your browser or file manager once).
2. Connect the board with a **USB-OTG adapter** and a **data cable**. Android asks whether
   to open *Mava Flasher*: yes (tick "always" to skip the question next time). Or open the
   app and tap **Allow USB access**.
3. **Install everything** the first time: firmware and web client; the board gets a new
   identity and device password and its settings start over (same as
   `./mava-flash board --erase`). **Update firmware only** later: identity, settings and
   stored messages stay (`./mava-flash firmware`).
4. **Board log** restarts the board and shows what it prints; the hotspot name and device
   password are picked out at the top.

5. **Board Wi-Fi** (firmware 0.1.1-beta9 on) sets the network the board joins over the same
   cable, and opens or closes its setup hotspot: no switch needed. It talks to the board's
   own USB socket (the firmware's host link is there, not on a USB-serial chip).

Keep the screen on and the cable still while it writes (about 2–4 minutes). Each region is
checked against the chip's MD5 of the flash before the board restarts.

It talks to the ESP32-S3's own USB (vendor 303a) and to boards with a CP210x or CH340
USB-serial chip (vendor 10c4 / 1a86; also CH343 and other CDC-ACM bridges). On a dev board
with two sockets, use the one marked **USB**: the firmware's log is only there. If it does
not connect: hold **BOOT**, press and release **RESET**, release BOOT, and try again.

The keyboard module (Raspberry Pi Pico) can't be flashed from the phone: use the Linux
flasher (`./mava-flash keyboard …`) or copy its `.uf2` onto the Pico's RPI-RP2 drive from
any computer.

How it works: `android/flasher` speaks Espressif's ROM serial bootloader protocol (the part
esptool uses without its RAM stub: sync, SPI attach, compressed flash writes, MD5, reset)
over Android's USB host API, with the same offsets and flash settings as `mava-flash`. Tests
run it against a simulated ESP32-S3 ROM (`./gradlew :flasher:testDebugUnitTest`).

## Building it yourself

`VERSION=dev scripts/build-flasher.sh` (Go, PlatformIO, network) → `dist/` (the Linux
bundle, and `dist/board-images/` for the Android flasher). Then
`cd android && ./gradlew :flasher:assembleDebug -PboardImages=../dist/board-images`.

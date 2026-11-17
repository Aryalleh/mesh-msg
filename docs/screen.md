# The handheld's screen on a phone (Mava Screen)

**Mava Screen** (`android/screen`, release asset `mava-screen-<version>-android.apk`) shows
the handheld's 240×320 screen on an Android phone over a USB-OTG cable, live, and works it:
touch the picture to tap, use the buttons for the handheld's keys (call, menu, arrows, OK,
back, end, backspace, enter, symbols), and type with the phone's keyboard (each character
goes to the board as a key press, any language). A Mava board without a screen fitted (a
bare ESP32-S3 dev board) runs the same UI, so the phone is its screen.

```
phone (Mava Screen) ── USB-OTG ── board's own USB socket (host link, port 0)
   SCREEN 1  ───────────────────►  start: info, then every tile, then what changes
   ◄─────────────── SCREEN events  16×8 tiles, run-length or raw RGB565
   INPUT touch / key ───────────►  fed to LVGL like the touch panel / keyboard module
```

## Landscape, speed, keyboards (0.1.1-beta17)

- The screen is **landscape, 320 × 240**, as on a BlackBerry: the ILI9341 is rotated in hardware
  (`LCD_ROTATION`, default 90; 270 if the panel is mounted the other way), so the copy sent to
  the phone is landscape too (20 × 30 tiles). The phone app sizes the picture from the info event.
- **Speed:** the board used to send at most 256 bytes to USB per main-loop pass (the ESP32-S3
  USB-Serial/JTAG transmit buffer defaults to 256 bytes, and one chunk was written per pass), so
  a full frame took seconds. Now the transmit buffer is 16 kB, every pass drains what is waiting,
  the node's per-port output is 8 kB and up to 64 tiles are queued per pass.
- **Keyboards:** one firmware for all of them: the Pico keyboard module (own matrix, BlackBerry
  9900 keyboard, 9900 keyboard + trackpad, detected), the touch keyboard, and the phone's
  keyboard through Mava Screen. Mava Screen's switch *Type with the phone's keyboard* (on by
  default) sends INPUT option `2 | 1 | 0` and the board hides its touch keyboard.

## How it works

- It is the same host link the apps use for LoRa over USB (docs/firmware.md): the phone
  sends `SCREEN 1` (op 0x0B) and `INPUT` (op 0x0C) commands, the board answers with
  `SCREEN` events (0x86). Trusted ports only (USB, BLE), never the Wi-Fi web port.
- **Board**: the display driver's flushes are copied into a frame in PSRAM
  (`firmware/lib/meshui/src/mirror.*`), marking 16×8 tiles dirty. Each pass of the main loop
  sends dirty tiles while the host's output buffer has room, so the screen never waits for
  USB and only what changed is sent; a flat area costs 11 bytes per tile, a full new frame
  of the violet UI a few tens of kB. A new subscriber gets the whole frame.
- **Phone**: `ScreenSession` decodes the tiles into a bitmap drawn without smoothing (sharp
  pixels), redrawn up to 30 times a second; touches are sent on press, every 30 ms while
  moving and on release.
- Formats (`mirror.h`): info `0 | w u16 | h u16 | tile w | tile h`; tile `1 | x u16 | y u16 |
  w | h | format | pixels` with format 0 raw RGB565 little-endian or 1 runs of
  `count u8 | colour u16`; input `0 | x u16 | y u16 | pressed` or `1 | action | code point u32`
  (`keys::Action`). Tests on both sides use the same bytes.

## Limits

- One app at a time owns the board's USB: close Mava's LoRa-over-USB and the flasher first.
- The board's own USB socket (vendor 303a), not a USB-serial chip: the firmware's host link
  is on the chip's USB. While a phone is attached the USB log is silent (as with meshd).
- Needs firmware 0.1.1-beta11 or later; older firmware answers "unknown command".
- Not tested on real hardware yet.

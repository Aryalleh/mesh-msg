# Mava handheld — prototype wiring

Three versions share everything except the input devices:

| Version | Keyboard | Pointing | Firmware env |
|---|---|---|---|
| **A** | our own 7 × 7 diode matrix (§4) | touch screen | `mava_handheld_s3` |
| **B** | original BlackBerry Bold 9900 keyboard (§7) | touch screen | `mava_handheld_bb` |
| **C** | original BlackBerry Bold 9900 keyboard (§7) | BlackBerry 9900 optical trackpad (§8), no touch | `mava_handheld_bbtp` |

§1–§6 describe version A; §7 and §8 list what B and C change. The handheld built from modules: the Mava board's ESP32-S3-N16R8 and SX1262 (board.md), a
2.4" ILI9341 touch display, a Waveshare WM8960 audio module and a 49-key diode matrix on 14
GPIOs. It does everything board.md describes (LoRa on the Meshtastic backbone, BLE, USB,
Wi-Fi relay, setup hotspot), plus a screen, a keyboard and voice calls. Firmware pins:
`firmware/platformio.ini`; key layout: `firmware/lib/meshkeys` (host-tested).

Status: **wiring designed, firmware reads the matrix (setup switch) and drives the radio;
display, touch, audio and the messenger UI are not written yet.** Nothing has been built.

## 1. Parts

| Part | What | Notes |
|---|---|---|
| MCU | ESP32-S3-WROOM-1-**N16R8** (or a DevKitC-1 N16R8) | GPIO 26–37 belong to flash and octal PSRAM; 19/20 are USB. 31 GPIOs remain — all are used below. |
| LoRa | SX1262 module (TCXO 1.8 V, DIO2 = RF switch), e.g. Ebyte E22-900M22S | as board.md |
| Display | Hailege 2.4" **ILI9341** 240×320 SPI TFT, **XPT2046** resistive touch, SD slot | 3.3 V logic. Short jumper **J1** on the module for 3.3 V supply. SD slot not used. |
| Audio | Waveshare **WM8960** Audio Board (SKU 15019) | I2C control (address 0x1A), I2S audio. **No crystal: MCLK comes from the ESP32-S3.** On-board mic, 3.5 mm headphone jack, two 1 W class-D speaker outputs (SPK: LP LN RP RN). |
| Loudspeaker | 8 Ω, 1 W, 20–28 mm, in a sealed box | WM8960 SPK left (LP/LN): ringing, hands-free |
| Earpiece | 32 Ω receiver | WM8960 SPK right (RP/RN): calls at the ear, at low gain |
| Keyboard | 49 tactile switches + 49 × 1N4148W (SOD-123) diodes | §4. A latching slide switch replaces one key (setup). |
| Fuel gauge | MAX17048 module | on the shared I2C bus (address 0x36): battery % without an ADC pin |
| Battery | 1S LiPo, 2000–3000 mAh, with protection | |
| Charger | TP4056 module with DW01 protection (USB-C in) | or any 1 A single-cell charger |
| 3.3 V | TPS63020 buck-boost module (2 A) | Wi-Fi + LoRa TX + speaker peaks reach ~0.8 A; an LDO drops out below ~3.5 V battery |
| Power switch | slide switch between battery and the buck-boost | |
| Passives | 100 k × 2 (straps), 4.7 k × 2 (I2C), 10 k × 2 (CS pull-ups), 100 nF / 10 µF per module | |

## 2. ESP32-S3 pin map

| GPIO | Signal | To | Notes |
|---|---|---|---|
| 0 | KB row R0 | key matrix | strap (BOOT): input with pull-up; nothing drives it low at reset (columns float). A BOOT button to GND may stay: it reads as row R0 held. |
| 1 | LoRa DIO1 | SX1262 DIO1 | RTC pin: a packet wakes the chip |
| 2 | LCD DC | ILI9341 DC | |
| 3 | KB row R1 | key matrix | strap (JTAG source, only with the eFuse set) |
| 4 | LoRa BUSY | SX1262 BUSY | |
| 5 | Touch CS | XPT2046 T_CS | 10 k pull-up to 3.3 V |
| 6 / 7 / 8 / 9 | KB rows R2 / R3 / R4 / R5 | key matrix | |
| 10 | LoRa CS | SX1262 NSS | |
| 11 | SPI MOSI | SX1262 MOSI, LCD SDI(MOSI), touch T_DIN | FSPI native pin |
| 12 | SPI SCK | SX1262 SCK, LCD SCK, touch T_CLK | FSPI native pin |
| 13 | SPI MISO | SX1262 MISO, LCD SDO(MISO), touch T_DO | FSPI native pin |
| 14 | KB row R6 | key matrix | |
| 15 | I2S MCLK | WM8960 MCLK | 256 × fs (4.096 MHz at 16 kHz) |
| 16 | I2S BCLK | WM8960 BCLK ("CLK") | |
| 17 | I2S LRCK | WM8960 LRCLK ("WS") | |
| 18 | I2S DIN | WM8960 **ADCDAT** (codec → ESP: microphone) | |
| 19 / 20 | USB D− / D+ | USB-C | host link to PCs and phones, flashing, log |
| 21 | LCD CS | ILI9341 CS | 10 k pull-up to 3.3 V |
| 38–42 | KB columns C0–C4 | key matrix | |
| 43 / 44 | KB columns C5 / C6 | key matrix | UART0 pins; the console is on USB |
| 45 | LCD backlight (PWM) | backlight switch (§3) | strap: **must be low at reset** — 100 k to GND |
| 46 | I2S DOUT | WM8960 **DACDAT** (ESP → codec: speaker / earpiece) | strap: low at reset — 100 k to GND |
| 47 | I2C SDA | WM8960 SDA, MAX17048 SDA | 4.7 k pull-up (skip if a module has one) |
| 48 | I2C SCL | WM8960 SCL, MAX17048 SCL | 4.7 k pull-up. On a DevKitC whose RGB LED is on GPIO 48, remove the LED. |
| EN | reset | SX1262 NRESET, ILI9341 RESET | the radio and the screen reset with the chip; firmware rewrites all their registers at start |

Why it fits: one SPI bus for radio, screen and touch (saves 3 pins); reset lines on EN (2);
battery level over I2C instead of an ADC (1); the setup switch inside the matrix (1); no LED,
no touch interrupt (polled), no SD card. **No GPIO is left**: a later PCB that needs one more
moves the keyboard to an I2C expander (TCA8418), which frees 13.

Shared SPI bus rules (firmware): every chip select idles high (set in `board::init` before
the radio starts); one task owns the bus; the radio runs at 8 MHz, the display at 40 MHz,
touch at 2 MHz, each in its own SPI transaction. A full-screen redraw is ~31 ms of bus time.

## 3. Modules, pin by pin

**SX1262** — 3.3 V, GND, NSS→10, SCK→12, MOSI→11, MISO→13, BUSY→4, DIO1→1, NRESET→EN,
TXEN/RXEN per the module (E22: DIO2 drives the switch, `RADIO_DIO2_RF_SWITCH=1`). Antenna
on the u.FL before ever transmitting.

**ILI9341 display (pin header J2)**

| Module pin | To |
|---|---|
| VCC | 3.3 V (J1 shorted) |
| GND | GND |
| CS | GPIO 21 |
| RESET | EN |
| DC | GPIO 2 |
| SDI (MOSI) | GPIO 11 |
| SCK | GPIO 12 |
| LED | backlight switch below |
| SDO (MISO) | GPIO 13 |
| T_CLK | GPIO 12 |
| T_CS | GPIO 5 |
| T_DIN | GPIO 11 |
| T_DO | GPIO 13 |
| T_IRQ | not connected (touch is polled every 30 ms while the screen is on) |
| SD_* (J4) | not connected |

Backlight: the LED pin feeds the LEDs (~40–60 mA at full brightness), more than a GPIO
should source. High-side switch: LED ← drain of an AO3401 P-FET, source at 3.3 V, gate
100 k to 3.3 V and pulled down by a 2N7002 whose gate is GPIO 45 (100 k to GND). Reset and
boot: backlight off; PWM at 5 kHz dims it.

**WM8960 Audio Board**

| Module pin | To |
|---|---|
| 3V3 / VCC | 3.3 V |
| GND | GND |
| SDA / SCL | GPIO 47 / 48 |
| CLK (BCLK) | GPIO 16 |
| WS (LRCLK) | GPIO 17 |
| DAC data in (Waveshare: "RXSDA"/"DACDAT") | GPIO 46 |
| ADC data out ("TXSDA"/"ADCDAT") | GPIO 18 |
| MCLK | GPIO 15 — set the module's MCLK selection pads so MCLK comes from the header (Waveshare's manual: short MCLK to the TX pad) |
| SPK LP / LN | 8 Ω loudspeaker |
| SPK RP / RN | 32 Ω earpiece |
| Headphone jack | headset (stereo out) |
| On-board MIC | call microphone (LINPUT1) |

Check every name above against the module's silkscreen before soldering: the header names
on Waveshare's audio boards differ between revisions (www.waveshare.com was not reachable
from the build environment to confirm them).

Audio modes, chosen by the firmware through WM8960 registers: **earpiece** (right speaker
driver on, low gain), **speaker** (left driver), **headset** (headphone outputs; the jack's
detect is not wired, so it is a menu choice). Echo cancellation: Espressif's AFE with the
decoded far-end audio as the software reference, which matters in speaker mode.

**MAX17048** — VCC/GND from the battery side (it measures the cell), SDA/SCL to 47/48.

**Power**

```
USB-C ──VBUS──► TP4056 (+DW01) ──► LiPo
                     │
                  BAT+ ──► slide switch ──► TPS63020 ──3.3 V──► ESP32-S3, SX1262, ILI9341, WM8960
                     └──► MAX17048 (cell sense)
USB-C D− / D+ ──► GPIO 19 / 20 (ESD diode array at the connector)
```

On a DevKitC, its own USB port's 5 V must not back-feed the battery path: power the
handheld from the charger path and use the DevKit's USB only for data, or remove its 5 V
diode link.

## 4. Keyboard matrix

7 rows × 7 columns = 49 positions on 14 GPIOs. Each key has a diode in series, **cathode to
the column**, so any number of keys can be held (Alt + Shift, Shift + letter, the latched
setup switch) without ghosting. Rows: inputs with pull-ups on RTC GPIOs (a key can wake the
chip). Columns: driven low one at a time for 10 µs, floating otherwise. Scan every 5 ms,
15 ms debounce.

| | C0 (38) | C1 (39) | C2 (40) | C3 (41) | C4 (42) | C5 (43) | C6 (44) |
|---|---|---|---|---|---|---|---|
| **R0 (0)** | ض q | ص w | ث e | ق r | ف t | غ y | ع u |
| **R1 (3)** | ه i | خ o | ح p | ج [ | چ ] | ش a | س s |
| **R2 (6)** | ی d | ب f | ل g | ا h | ت j | ن k | م l |
| **R3 (7)** | ک ; | گ ' | ظ z | ط x | ز c | ر v | ذ b |
| **R4 (8)** | د n | پ m | و , | ژ . | Space | ⌫ Backspace | ⏎ Enter |
| **R5 (9)** | Shift | Alt | ▲ Up | ▼ Down | ◀ Left | ▶ Right | OK |
| **R6 (14)** | 📞 Answer / Call | ⛔ Hang up | Menu (left soft key) | Back (right soft key) | Vol + | Vol − | **Setup switch** (latching) |

The letters sit on their **ISIRI 9147** (standard Persian keyboard) positions, so the Latin
layer is plain QWERTY and Persian typists keep their habits; ژ, normally Shift + C, has its
own key so all 32 letters are one press.

Physical layout (top to bottom):

```
 [Menu]  [📞]       [▲]       [⛔]  [Back]
              [◀]  [OK]  [▶]
                    [▼]
 ض q  ص w  ث e  ق r  ف t  غ y  ع u  ه i  خ o  ح p  ج [  چ ]
  ش a  س s  ی d  ب f  ل g  ا h  ت j  ن k  م l  ک ;  گ '
 [Shift]  ظ z  ط x  ز c  ر v  ذ b  د n  پ m  و ,  ژ .  [⌫]
 [Alt]          [        Space        ]           [⏎]
 side: [Vol+] [Vol−]  top edge: setup switch (OFF | ON)
```

Layers (`firmware/lib/meshkeys`):

| Input | Persian layer | Latin layer |
|---|---|---|
| key | the letter | the QWERTY character |
| Shift + key | آ on ا, ئ on ی, ؤ on و, ة on ه, ء on ژ | capitals |
| Shift + Space | ZWNJ (نیم‌فاصله) | space |
| Alt + top row (q … p) | ۱ ۲ ۳ ۴ ۵ ۶ ۷ ۸ ۹ ۰ | 1 2 3 4 5 6 7 8 9 0 |
| Alt + other keys | symbols: ( ) ! ؟ - + = / : " ' * # @ % & _ < > ؛ ، . | the same with ASCII ? ; , |
| **Alt + Shift** | switch to Latin | switch to Persian |

Phone keys: 📞 answers an incoming call or calls the open chat's contact; ⛔ rejects or hangs
up (and goes back to the chat list); OK and the arrows move through menus; Menu and Back are
the soft keys shown at the bottom of the screen.

## 5. Calls and video

- **Voice:** Wi-Fi only (LoRa can't carry voice). Opus 16 kHz, 20 ms frames on the S3 (a
  fraction of one core), WM8960 at 16 kHz, I2S full duplex, as docs/calls.md §7.
- **Video, best case: receive only.** The handheld offers a video stream with direction
  *receive*, codec H.264 and `max width 240, height 180, fps 10` (calls.md stream caps).
  H.264 is decoded in software on the S3 into PSRAM and drawn at 240×180 on the 240×320
  screen. Expected 5–10 fps; to be measured. It never sends video (no camera). The Android
  sender currently encodes about 640×480 and must honour the receiver's caps, which it
  does not do yet.

## 6. Bring-up order

1. Power path alone: 3.3 V under 1 A load, charger, fuel gauge reading over I2C.
2. ESP32-S3 + matrix: the USB log shows the setup switch; a key test screen comes later.
3. SX1262 on the shared bus: `pio run -t upload`, LoRa against a Meshtastic node.
4. ILI9341 + backlight, then touch, on the same bus while LoRa runs.
5. WM8960: I2C probe at 0x1A, then a 1 kHz tone on each speaker driver, then mic loopback.
6. Firmware for the UI, messaging and calls (not written yet).

## 7. Version B — BlackBerry 9900 keyboard

The original keypad of the BlackBerry Bold 9900 (QWERTY or QWERTZ legends, both work)
replaces our matrix. Everything else is as version A, touch screen included.

**Matrix.** 39 keys on **7 rows × 6 columns = 13 lines** of the keypad's flex, mapped by
ZitaoTech's RP2040 board for the same keypad (BB9900-USB-keyboard, QMK `info.json`). The
keypad has no diodes; its layout keeps normal combinations (Alt or Shift + a key) free of
ghosting. Three positions are unused on the flex; our board adds a switch at each, **with a
diode** (cathode to the column) so they cannot cause ghosts:

| | C0 (38) | C1 (39) | C2 (40) | C3 (41) | C4 (42) | C5 (43) |
|---|---|---|---|---|---|---|
| **R0 (0)** | *Setup switch* | W | G | S | L | H |
| **R1 (3)** | A | Q | R | E | O | U |
| **R2 (6)** | 📞 Call | 0 | F | ⇧ left | K | J |
| **R3 (7)** | ⛔ End | Space | C | Z (QWERTZ: Y) | M | N |
| **R4 (8)** | ⊞ BlackBerry (Menu) | Sym | T | D | I | Y (QWERTZ: Z) |
| **R5 (9)** | ↩ Back | Alt | V | X | $ | B |
| **R6 (14)** | *Vol + side key* | *Vol − side key* | ⇧ right | P | Del | ⏎ Enter |

Italic positions are on our board, not on the keypad. The flex lines' order is not
published: **wire them to the GPIOs, flash `mava_handheld_bb`, and press each key** — the
USB log prints `key R<r> C<c> down`. Swap wires (or the order in `KB_ROW_PINS` /
`KB_COL_PINS`) until every key prints the position in the table. The keypad's connector is
a board-to-board type whose part number the reference project does not give: measure the
flex (contact count, pitch) and pick the matching receptacle, or solder fine wires to the
flex pads for the prototype.

**Pins (changes from A):**

| GPIO | A | B |
|---|---|---|
| 38–43 | columns C0–C5 | columns C0–C5 (keypad flex) |
| 44 | column C6 | **keyboard backlight** PWM → gate of an AO3400 N-FET (100 k to GND); the FET switches the keypad's LED return; series resistor chosen after measuring the LEDs' forward voltage |
| 0, 3, 6, 7, 8, 9, 14 | rows R0–R6 | rows R0–R6 (keypad flex) |

**Persian on 26 letter keys.** The letters stay on their ISIRI 9147 positions (as in A);
the six letters ISIRI puts on punctuation keys go where a BlackBerry has room:

| Key | Persian | Shift (Persian) | Alt (Persian) | Latin / Alt (printed) |
|---|---|---|---|---|
| Q W E R T Y U I O P | ض ص ث ق ف غ ع ه خ ح | — / ة on ه | U: **چ**, P: **ج**; W E R: ۱ ۲ ۳ | QWERTY letters / # 1 2 3 ( ) _ - + @ |
| A S D F G H J K L | ش س ی ب ل ا ت ن م | ئ on ی, آ on ا | L: **گ**; S D F: ۴ ۵ ۶ | / * 4 5 6 / : ; ' " |
| Z X C V B N M | ظ ط ز ر ذ د پ | ء on ز | B: **ژ**; Z X C: ۷ ۸ ۹ | / 7 8 9 ? ! , . |
| $ | **و** | ؤ | — | $ |
| 0 | **ک** | — | ۰ | 0 |

- **Alt + Shift** (held together, or tapped one after the other) switches Persian ↔ Latin.
- Alt and Shift work held or **tapped once for the next key**, as on a BlackBerry.
- Shift + Space = ZWNJ (نیم‌فاصله).
- **Sym** tapped opens the on-screen symbol and digit picker (@ « » ! ؟ … everything the
  Persian Alt layer gave up for ج چ گ ژ). **Sym held**: E S D F are ▲ ◀ ▼ ▶, Enter is OK —
  the arrows version A has as keys. The BlackBerry key is Menu, ↩ is Back, 📞 / ⛔ answer and
  hang up.
- Stickers with the Persian letters go on the keycaps (the 9900 keypad only exists with
  Latin legends).

## 8. Version C — BlackBerry 9900 keyboard + trackpad

Version B plus the 9900's optical trackpad, which replaces the touch screen: the XPT2046
lines (T_CLK, T_CS, T_DIN, T_DO, T_IRQ) stay unconnected, and GPIO 5 is free for the setup
switch. The trackpad moves the selection (and the cursor in text); pressing it is OK.

**Sensor:** Avago **ADBS-A320** optical finger navigation, **I2C at 0x3B**, product ID 0x83
(register 0), motion register 0x02 (bit 7 = moved), ΔX 0x03, ΔY 0x04 (signed bytes).
Pinout from stolen/blackberry_trackpad (cable seen from the back, pads towards you):

| Trackpad pin | Function | To |
|---|---|---|
| 1 | dome switch A | row R0 (GPIO 0) |
| 2 | dome switch B | diode → column C0 (GPIO 38): the click is matrix position (0,0) |
| 3 | backlight LED − | 200 Ω → the keyboard-backlight FET's drain (dims with the keyboard) |
| 4 | backlight LED + | 3.3 V |
| 5 | DVDD | **1.8 V** from a small LDO (XC6206P182 or AP2112K-1.8): its own 1.8 V regulator is not used on this module |
| 6 | VDDA / VDDIO / VLED+ | 3.3 V |
| 7, 12 | GND | GND |
| 8 | NRST | EN (resets with the chip) |
| 9 | SDA | GPIO 47 (shared I2C) |
| 10 | MOTION_N | not connected: the firmware polls register 0x02 |
| 11 | SCL | GPIO 48 (shared I2C) |
| 13 | SHTDWN | GND (always on) |

Connector: the trackpad's 13-contact, 0.3 mm dual-row flex fits a **Hirose FH35C-17S**
(two positions unused on each side); the flex is thinner than the connector expects, so a
layer of Kapton tape behind it helps. With the trackpad on the bus, use **3.3 k** I2C
pull-ups (the sensor's notes use 3 k) instead of 4.7 k. Don't swap the 1.8 V and 3.3 V
pins: the sensor then answers at 0x33 and misbehaves.

**Pins (changes from B):**

| GPIO | B | C |
|---|---|---|
| 5 | touch CS | **setup switch** (latching, to GND; internal pull-up; RTC pin) |
| 47 / 48 | I2C (WM8960, MAX17048) | + trackpad at 0x3B |

Firmware: `trackpad.cpp` checks the product ID at start (the USB log says
`trackpad ok (ADBS-A320)`) and logs movement until the UI uses it.

Sources for §7–§8: ZitaoTech, *BB9900-USB-keyboard* (QMK keyboard for the 9900 keypad and
trackpad: matrix, trackpad registers); stolen, *blackberry_trackpad* (9900 trackpad pinout,
voltages, connector).


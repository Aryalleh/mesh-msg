# Model S power board

The power section of the Model S handheld (docs/handheld.md §1–2) as its own 2-layer board
inside the case, plugged into the 2-layer main board. Everything here is **generated**: change
the scripts, not the KiCad files.

| | |
|---|---|
| Size | 54 × 36 mm, 2 layers, 1.6 mm, all parts on top, 4 × M2 holes |
| Input | USB-C (5 V sink, CC 5.1 k, USBLC6 on D+/D−, SMF5.0A on VBUS) |
| Battery | 1-cell LiPo with 10 k NTC on JST-PH 3-pin (1 = BAT+, 2 = NTC, 3 = BAT−) |
| PMU | X-Powers AXP2101: linear charger ≤ 1 A, power path, fuel gauge, DCDC1 + 6 LDOs |
| RTC | NXP PCF85063A from the battery through a TI TPS7A02 (25 nA) |
| To main board | J3, 2 × 14 1.27 mm SMD header (mates with a 2 × 14 1.27 mm SMD socket) |

## Rails

| Rail | Source | Max | Model S load | J3 pins |
|---|---|---|---|---|
| VBAT | battery | – | NS4150B, motor, LTE board (2 A peaks) | 1–4 |
| VSYS | AXP2101 power path | 2 A total | spare for loads that must run from USB without a battery | 7–8 |
| +3V3 | DCDC1 | 2 A | ESP32-S3, flash, PSRAM | 11–13 |
| 3V3_LORA | ALDO2 | 300 mA | SX1262 module | 15 |
| 3V3_AUDIO | ALDO3 | 300 mA | ES8388 | 16 |
| 3V3_SD | ALDO4 | 300 mA | microSD | 17 |
| 3V3_LCD | BLDO1 | 300 mA | screen VCI, touch | 18 |
| 3V3_GPS | BLDO2 | 300 mA | GPS / LTE option (J6 VCC, LTE load-switch enable) | 19 |
| VSPARE | ALDO1 | 300 mA | spare, off by default | 20 |
| VRTC | TPS7A02 from VBAT | 200 mA | PCF85063A only | – |

GND on J3 pins 5, 6, 9, 10, 14, 21. Header pins carry 1 A each.

## Signals to the ESP32-S3

| J3 | Net | ESP32-S3 | |
|---|---|---|---|
| 23 / 24 | I2C_SDA / I2C_SCL | GPIO18 / GPIO8 | AXP2101 at 0x34, PCF85063A at 0x51; 2.2 k pull-ups on this board |
| 25 | PMU_IRQ | GPIO2 | open drain, 10 k pull-up here; AXP2101 IRQ and RTC INT wired together |
| 26 | PMU_PWROK | (EN, optional) | high once the rails are up |
| 22 | PWR_KEY | – | side power key on the main board, to GND (parallel to SW1 here) |
| 27 / 28 | USB_D_P / USB_D_N | GPIO20 / GPIO19 | native USB, ESD-protected at the connector |

**Charge status and battery capacity** reach the ESP32-S3 over I2C, nothing else is needed:
the AXP2101's fuel gauge (E-Gauge) gives the state of charge in % (REG A4h), the battery
voltage and charge current from its ADC, and the charger state (charging / full / discharging,
VBUS present) in its status registers. PMU_IRQ goes low on every change (USB in/out, charge
start/done, battery low, power key), so the ESP32-S3 sleeps until something happens.
For an accurate percentage the firmware loads the battery's 128-byte model into the gauge at
start-up (X-Powers design guide §6); without it the gauge uses its generic curve.
XPowersLib (already used in `firmware/`) has the register map.

## Firmware duties

- Set each rail's voltage and enable it before use; rails start from the AXP2101's eFuse
  defaults.
- Charge current ≤ 1 A for the 2500 mAh pack (REG 62h), target 4.2 V (REG 64h).
- TS pin: the pack's 10 k NTC; for a pack without NTC fit R10 (10 k, DNP) or turn TS off.
- PCF85063A: CAP_SEL = 12.5 pF, CLKOUT off (COF = 111).

## Files

| File | |
|---|---|
| `generate_schematic.py` | the schematic (spec + drawing; `verify()` proves they match) |
| `place_board.py` | placement |
| `route_board.py` | routing: power paths scripted, the rest Freerouting, stored in `routing/` |
| `make_rails.py` | power symbols for this board's rails (`lib/rails.kicad_sym`) |
| `lib/` | AXP2101, TPS7A02, PCF85063ATT, inductor: symbols/footprints from LCSC (easyeda2kicad) |
| `kicad-env.sh` | KiCad 10 from the AppImage (`kicad-cli`, `kpy` = KiCad's Python) |
| `schlib.py`, `pcblib.py`, `routelib.py`, `subcircuits.py`, `jlc_cpl.py` | the libraries the scripts use |

Regenerate:

```
. ./kicad-env.sh
python3 make_rails.py
python3 generate_schematic.py --force --render
kpy place_board.py --force
kpy route_board.py
```

# Model S power board: ordering, assembly and bring-up

Board: 54 × 36 mm, 2 layers, 1.6 mm FR-4, 1 oz copper, parts on the top side only.
Schematic: `power-s.kicad_sch` (PDF in `jlc/power-s.pdf`). Board: `power-s.kicad_pcb`.
What the board does and what the firmware must do: [README.md](README.md).

## 1. Build the files

```
cd hardware/power-s
. ./kicad-env.sh
python3 build_package.py
```

`jlc/` then holds:
- `power-s-gerbers.zip`: Gerbers and drill files.
- `power-s_bom.csv`: the BOM.
- `power-s_cpl.csv`: the pick-and-place file.
- `power-s.pdf`: the schematic.

The script refuses to build when DRC shows an error or an unrouted connection.

## 2. Order from JLCPCB (PCB + assembly)

1. jlcpcb.com → **Order now** → upload `power-s-gerbers.zip`. Settings:

   | Setting | Value |
   |---|---|
   | Layers | 2 |
   | Thickness | 1.6 mm |
   | Copper | 1 oz |
   | Surface finish | HASL lead-free or ENIG |
   | Remove order number | "Specify a location" or "Yes" |

   ENIG is better for the 0.4 mm-pitch AXP2101.
2. Turn on **PCB Assembly**: top side, **Standard** PCBA. The AXP2101 QFN-40 at 0.4 mm
   pitch and the SMD header need Standard; Economic may refuse them.
3. Upload `power-s_bom.csv` and `power-s_cpl.csv`.
4. In the **parts** step, check that every line matched and is in stock. If a part is out of
   stock, pick the same value, size and rating. Don't substitute these:

   | Part | Why |
   |---|---|
   | U1 AXP2101 | – |
   | U3 TPS7A0233 | 25 nA quiescent current |
   | L1 | ≤ 30 mΩ, ≥ 2.6 A saturation |
   | R1 | 10 mΩ, 1206 |
   | J3 | 2 × 14, 1.27 mm SMD |

5. In the **placement preview**, check the orientation of every polarised part before you
   pay. Pin 1 / cathode must match the silkscreen dot or bar:

   | Ref | Part | Check |
   |---|---|---|
   | U1 | AXP2101 | pin-1 dot |
   | U2 | USBLC6-2SC6 | pin-1 dot |
   | U3 | TPS7A0233 | pin-1 dot |
   | U4 | PCF85063ATT | pin-1 dot |
   | D1 | green LED | cathode toward U1 |
   | D2 | SMF5.0A | cathode bar toward VBUS |
   | J1 | USB-C | mouth over the bottom edge |
   | J2 | JST-PH | opening toward the left edge |
   | J3 | 2 × 14 header | pin 1 = VBAT, the header's left end |

   Rotate any part that is off in the preview tool. That's normal for KiCad stock
   footprints. Write the correction into `jlc_rotations.json` so the next order is right.
6. **Not fitted:** R10 (DNP) and the test pads TP1–TP5.

## 3. Hand assembly (if you solder it yourself)

Tools:
- hot-air station and/or a hotplate;
- fine-tip iron;
- leaded paste or flux;
- 0.4 mm-pitch stencil (order the stencil with the PCB);
- magnifier;
- multimeter;
- bench supply with a current limit.

Solder in this order, small and flat parts first:

1. **U1 AXP2101**: paste through the stencil, reflow with hot air or the hotplate. The
   exposed pad has 9 vias and must be soldered: it is the IC's ground and its heat path.
   Afterwards check for bridges between neighbouring pins under the magnifier. Then measure
   the resistance from VSYS, VBUS, VBAT and +3V3 to GND: none of them may read near 0 Ω.
2. **U4, U3, U2, Y1**.
3. **The passives**: all 0402s, then 0603/0805, R1 (1206) and L1.
4. **D1**: cathode toward U1. **D2**: bar toward VBUS (the C23 side).
5. **J1 USB-C**: solder the four shell tabs too, because they carry the plug's force.
6. **J2 battery connector, SW1, J3 header**.

Leave R10 empty (see §5).

## 4. Bring-up (before the main board and before a battery)

Do these steps in order. Stop at the first one that fails.

1. **No power, multimeter on ohms.** Measure each test pad to TP5 (GND):

   | Pad | Rail | Expected |
   |---|---|---|
   | TP1 | VBUS | > 100 kΩ |
   | TP2 | VSYS | > 1 kΩ |
   | TP3 | VBAT | > 100 kΩ |
   | TP4 | +3V3 | > 1 kΩ |

   A reading near 0 Ω is a solder bridge.
2. **USB from a bench supply.** Set it to 5.0 V with a 200 mA limit and feed it through a
   USB-C breakout, or use a USB meter. With no battery and no main board it should draw
   under 30 mA. Measure:

   | Pad | Expected |
   |---|---|
   | TP1 | 5.0 V |
   | TP2 (VSYS) | 4.4–5.0 V |
   | TP4 (+3V3, DCDC1) | 3.3 V ± 3 % |

   The AXP2101's own eFuse defaults turn DCDC1 on. The green LED may blink: it is the
   charge LED, and with no battery the charger says "no battery". If +3V3 is missing,
   press SW1 for 1 s (that is PWRON).
3. **Battery.** Fit a 1-cell LiPo with a 10 k NTC on its 3-pin JST-PH plug. Pin 1 is
   BAT+, pin 2 NTC and pin 3 BAT−. **Check the plug's polarity with a meter before you plug
   it in**: LiPo packs are wired both ways, and a reversed pack kills U1.
   - With no NTC in the pack, fit R10 (10 kΩ, 0402). Otherwise the charger sees a
     "battery too cold" fault and won't charge.
   - Firmware can also turn the TS check off.

   With USB still connected, the LED shows charging, and the supply current rises toward
   the firmware's charge current. The eFuse default charge current is low until firmware
   sets it.
4. **Unplug USB.** The board now runs from the battery: TP4 stays at 3.3 V and TP2 follows
   TP3 (VBAT). Press SW1 for 6 s and everything turns off. Press it for 1 s and it turns
   on again.
5. **RTC.** Even with the power off, VRTC (at C26/C27) reads 3.3 V while a battery is
   connected.

## 5. Connect to the main board

- J3 mates with a **2 × 14, 1.27 mm SMD female socket** on the main board: LCSC
  C22436134 or equivalent, 1.27 mm pitch.
- Stack the boards with M2 standoffs in the four corner holes, 2.2 mm at 3 mm from each edge.
- J3 pin 1 (VBAT) is the header's left end when the board is seen from the top, with
  USB-C at the bottom. The main board's socket must use the same pinout, mirrored for its
  mounting side. The full pin list is in [README.md](README.md#rails).
- On the main board **do not** fit pull-ups on I2C_SDA / I2C_SCL / PMU_IRQ: they are on
  this board (R4–R7).
- The main board's side power key goes to J3 pin 22 (PWR_KEY), switching to GND. It is
  in parallel with SW1.

## 6. Firmware check (with the main board)

1. An I2C scan on GPIO18 (SDA) / GPIO8 (SCL) shows **0x34** (AXP2101) and **0x51**
   (PCF85063A).
2. Read the battery percentage (REG A4h), the battery voltage and the charge state.
   Unplug USB: PMU_IRQ (GPIO2) goes low, and the firmware sees "VBUS removed".
3. Before the LoRa, audio, SD, LCD and GPS parts are used, the firmware sets their LDOs
   to 3.3 V and turns them on:

   | Rail | LDO |
   |---|---|
   | 3V3_LORA | ALDO2 |
   | 3V3_AUDIO | ALDO3 |
   | 3V3_SD | ALDO4 |
   | 3V3_LCD | BLDO1 |
   | 3V3_GPS | BLDO2 |

   Before that, those parts have no power.
4. Load the battery's fuel-gauge model once at start-up (X-Powers guide §6) for an
   accurate percentage.

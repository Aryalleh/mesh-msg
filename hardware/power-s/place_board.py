"""Model S power board -- placement.

    . ./kicad-env.sh && kpy place_board.py [--force]

Board mm, origin top-left, Y down. Re-running resets every part listed here
to its table position (parts locked in KiCad are not touched by this script
only if you remove them from the table first).

Mechanics (README.md): 54 x 36 mm, 2 layers, all parts on top. Top edge faces
the main board (2x14 1.27 mm header J3, mezzanine), USB-C on the bottom edge,
battery JST-PH on the left edge, 4x M2 holes in the corners.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import pcblib                                     # noqa: E402
from pcblib import Board                          # noqa: E402

NAME = "power-s"
W, H = 54.0, 36.0          # case inside is ~58 mm wide (docs/handheld.md: 62 mm outside)

# Pad orientation used below: a two-pad part at rot 0 has pad 1 on the left,
# rot 90 pad 1 at the bottom, rot 180 on the right, rot 270 at the top.
# Every capacitor's pad 1 is its rail, pad 2 ground, so the rot puts the rail
# pad toward the pin it serves.
#
# AXP2101 rotated 180 deg (pad positions at (27, 20)):
#   top    y 17.5  LDO outputs and their inputs, pins 19..12 at x 25.6..28.4
#   left   x 24.5  FB1 18.2, LX1 18.6, VIN1/2 19.0/19.4, VRTC 21.0, PWROK 21.4, PWRON 21.8
#   bottom y 22.5  TS 25.2, BAT 26.0, VSYS 26.4, VMID 27.2, VBUS 27.6, IRQ/SDA/SCL 28.0..28.8
#   right  x 29.5  VIN3/4 19.4/19.8, GND 21.0, VREF 21.4, CHGLED 21.8
U1 = (27.0, 20.0)

PLACEMENT = {
    # ref: (x, y, rot)
    "U1": (U1[0], U1[1], 180),
    "J3": (27.0, 5.2, 90),            # to the main board, along the top edge
    # DCDC1: LX1 -> L1 -> +3V3, output caps beside L1, VIN1/2 caps under it
    "L1": (19.4, 16.6, 0),            # pad 1 (LX) on the right, facing pin 22
    "C12": (15.3, 15.2, 90),
    "C13": (15.3, 18.8, 90),
    "C7": (22.5, 20.2, 180),          # VIN1/2 2.2 uF at the pins
    "C3": (19.4, 20.9, 180),          # VIN1/2 22 uF
    "C21": (22.2, 21.4, 180),         # VRTC (pin 28)
    "C22": (21.4, 23.3, 180),           # PWRON 1 nF
    "R3": (21.4, 24.5, 180),            # PWRON 510 R to the key
    # top row: LDO outputs and ALDOIN/BLDOIN, in pin order, rail pad down, 1.3 mm pitch:
    # route_board.py fans the 0.4 mm pins out to them in a staircase
    "C14": (22.95, 13.8, 90),          # 3V3_LORA   pin 19
    "C19": (24.25, 13.8, 90),          # VSPARE     pin 18
    "C5": (25.55, 13.8, 90),           # ALDOIN     pin 17
    "C15": (26.8, 13.8, 90),          # 3V3_AUDIO  pin 16
    "C16": (28.15, 13.8, 90),          # 3V3_SD     pin 15
    "C18": (29.45, 13.8, 90),          # 3V3_GPS    pin 14
    "C10": (30.75, 13.335, 90),          # BLDOIN 4.7 uF pin 13
    "C17": (32.05, 13.8, 90),         # 3V3_LCD    pin 12
    # right: VIN3/4, VREF, charge LED
    "C9": (32.0, 18.45, 0),
    "C8": (32.0, 19.65, 0),
    "C20": (32.0, 21.4, 0),          # VREF 1 uF at the pin
    "D1": (33.0, 22.6, 0),            # CHGLED sinks: cathode (pad 1) toward U1
    "R2": (33.0, 24.3, 0),
    # bottom: BAT, VSYS, VMID, VBUS, the 10 mOhm VMID->VSYS link, VSYS bulk
    "C11": (23.8, 25.4, 270),         # BAT 2.2 uF
    "C6": (24.9, 25.4, 270),          # VSYS 2.2 uF pin 34
    "C2": (26.6, 26.25, 270),         # VMID 10 uF
    "C1": (28.9, 26.25, 270),         # VBUS 10 uF (I2C leaves above it)
    "R1": (26.0, 29.6, 0),
    "C4": (21.0, 27.5, 90),           # VSYS 22 uF bulk
    # I2C / IRQ / PWROK pull-ups to +3V3, under the header's signal pins
    "R4": (32.6, 11.0, 90),
    "R5": (33.75, 11.0, 90),
    "R6": (34.9, 11.0, 90),
    "R7": (36.05, 11.0, 90),
    # USB-C on the bottom edge, right: D+/D- run up the free right side to
    # J3 27/28; ESD flow-through right above the receptacle
    "J1": (40.0, H - 4.17, 0),
    "U2": (40.0, 23.6, 180),
    "R8": (35.6, 24.0, 90),
    "R9": (44.4, 24.0, 90),
    "D2": (47.6, 25.5, 90),
    "C23": (47.6, 29.8, 90),
    # battery on the left edge, cable leaves to the left; VBAT is one short
    # run to J3 pins 1-4 (the header's left end) and to BAT (pin 33)
    "J2": (5.2, 22.0, 270),
    "C24": (11.6, 21.0, 270),         # VBAT pad up, in line with J2 pin 1
    "R10": (11.6, 24.5, 90),
    # RTC, top right
    "U3": (42.0, 12.0, 0),
    "C25": (45.1, 12.0, 90),
    "C26": (39.2, 13.5, 90),
    "U4": (48.0, 12.5, 180),
    "Y1": (48.3, 8.0, 0),
    "C27": (45.6, 15.0, 90),
    # power key (bench; the case key is on the main board, PWR_KEY on J3)
    "SW1": (10.5, 31.4, 0),
    # test pads along the bottom edge
    "TP1": (17.0, 33.6, 0),
    "TP2": (19.5, 33.6, 0),
    "TP3": (22.0, 33.6, 0),
    "TP4": (24.5, 33.6, 0),
    "TP5": (27.0, 33.6, 0),
}
HOLES = [(3.0, 3.0), (W - 3.0, 3.0), (3.0, H - 3.0), (W - 3.0, H - 3.0)]


def main():
    pcblib.create_board(__file__, NAME, layers=2)
    b = Board(__file__, NAME, size=(W, H))
    b.sync_footprints(prune=True)
    b.refresh_footprints()                          # pick up lib/ fixes (fix_footprints.py)
    b.outline(radius=1.0)
    b.place_all(PLACEMENT)
    b.link_schematic()
    # match the symbols: test pads on the BOM (no LCSC, so not on the JLC BOM), R10 not fitted
    for ref, fp in b.fps.items():
        if ref.startswith("TP"):
            fp.SetExcludedFromBOM(False)    # symbols are on the BOM; jlc_cpl skips parts without LCSC
    b.fps["R10"].SetDNP(True)
    for x, y in HOLES:
        b.hole(x, y, 2.2)
    b.tidy_references()
    try:
        b.check()
    except SystemExit:
        if "--preview" not in sys.argv:
            raise
        print("  (--preview: saving and rendering despite check errors)")
    b.save(force="--force" in sys.argv or "--preview" in sys.argv)
    b.drc()
    b.render(os.path.join(HERE, "out", "pcb"), views=("top",))   # angled 3D takes >10 min here


if __name__ == "__main__":
    main()

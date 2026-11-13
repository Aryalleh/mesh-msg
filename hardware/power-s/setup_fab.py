"""Model S power board -- fab rules, net classes, stackup (JLCPCB, 2 layers).

    . ./kicad-env.sh && kpy setup_fab.py

Idempotent; moves nothing. Run after place_board.py and before route_board.py.

Stackup JLC0216A: 1.6 mm, 1 oz outer copper, FR-4.

Rules are JLCPCB's no-surcharge tier with margin (jlcpcb.com/capabilities/pcb-capabilities):
0.15 mm track/space (their limit 0.10), 0.3 mm via drill / 0.6 mm via, 0.3 mm copper to
the routed edge.

Widths. The AXP2101 is a 0.4 mm pitch QFN with 0.2 mm pads: a track entering a pad can
be at most 0.3 mm wide or it is within 0.15 mm of the next pin. So the classes that the
autorouter uses stay at 0.3 mm (about 1 A at a 10 C rise on 1 oz outer copper, IPC-2152),
and the paths that carry more than that -- battery to header, USB VBUS to the PMU, the
DCDC1 switch node and output, VSYS to the header -- are drawn wide by route_board.py and
backed by power pours on the top layer.

USB D+/D- are full-speed (12 Mbit/s, ESP32-S3 native USB): no impedance control needed
at a 30 mm run; they are kept together and away from the switch node.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from pcblib import Board, say           # noqa: E402

NAME = "power-s"

CLASSES = {
    "POWER": {"track_width": 0.3, "clearance": 0.15, "via_diameter": 0.6, "via_drill": 0.3},
    "RAIL": {"track_width": 0.3, "clearance": 0.15, "via_diameter": 0.6, "via_drill": 0.3},
    "USB": {"track_width": 0.25, "clearance": 0.2, "via_diameter": 0.6, "via_drill": 0.3},
}
PATTERNS = [
    ("POWER", "VBUS"), ("POWER", "VSYS"), ("POWER", "VBAT"), ("POWER", "+3V3"),
    ("POWER", "*VMID"), ("POWER", "*LX1*"),
    ("RAIL", "3V3_*"), ("RAIL", "VSPARE"), ("RAIL", "VRTC"), ("RAIL", "*AXP_VRTC"),
    ("USB", "*USB_D_?"),
]
DEFAULT_CLASS = {"track_width": 0.2, "clearance": 0.15, "via_diameter": 0.6, "via_drill": 0.3}

RULES = {
    "min_clearance": 0.127,
    "min_track_width": 0.127,
    "min_connection": 0.127,
    "min_via_diameter": 0.5,
    "min_via_annular_width": 0.1,
    "min_through_hole_diameter": 0.3,
    "min_hole_to_hole": 0.25,
    "min_hole_clearance": 0.25,
    "min_copper_edge_clearance": 0.3,
    "min_silk_clearance": 0.0,
    "min_text_height": 0.8,
    "min_text_thickness": 0.15,
    "min_resolved_spokes": 1,
    "solder_mask_to_copper_clearance": 0.0,
    "max_error": 0.005,
    "use_height_for_length_calcs": True,
}

DRU = """(version 1)

(rule "JLC: NPTH to copper"
\t(condition "A.Pad_Type == 'NPTH, mechanical'")
\t(constraint hole_clearance (min 0.2mm)))

(rule "JLC: SMD pad to pad, different nets"
\t(condition "A.Type == 'Pad' && B.Type == 'Pad' && A.Pad_Type == 'SMD' && B.Pad_Type == 'SMD' && A.Net != B.Net")
\t(constraint clearance (min 0.15mm)))

(rule "AXP2101 0.4 mm pitch: pours keep 0.15 mm from its pins"
\t(condition "A.Type == 'Zone' && B.Parent.Reference == 'U1'")
\t(constraint clearance (min 0.15mm)))
"""


def main():
    b = Board(__file__, NAME)
    b._stale_holes.clear()
    b._stale_zones.clear()
    b.set_netclasses(CLASSES, PATTERNS, RULES)
    pro_path = os.path.join(HERE, NAME + ".kicad_pro")
    pro = json.load(open(pro_path, encoding="utf-8"))
    for c in pro["net_settings"]["classes"]:
        if c["name"] == "Default":
            c.update(DEFAULT_CLASS)
    with open(pro_path, "w", encoding="utf-8", newline="\n") as f:
        json.dump(pro, f, indent=2)
        f.write("\n")
    b.check_netclasses()
    b.set_stackup("JLC0216A")
    open(os.path.join(HERE, NAME + ".kicad_dru"), "w", encoding="utf-8", newline="\n").write(DRU)
    say("wrote %s.kicad_dru: %d rules" % (NAME, DRU.count("(rule ")))


if __name__ == "__main__":
    main()

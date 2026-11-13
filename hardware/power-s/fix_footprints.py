"""Repairs the footprints easyeda2kicad fetched into lib/power_s.pretty.

    . ./kicad-env.sh && kpy fix_footprints.py

EasyEDA draws the courtyard round the package body, not round the pads, so
the AXP2101's courtyard is 5.08 mm square while its pads reach 5.8 mm and the
TSSOP's misses its leads by 1.35 mm. Placement and DRC keep parts apart by
courtyard, and with these a capacitor lands touching the IC's pads.

Each courtyard is replaced by a rectangle round the union of body and pads,
plus 0.25 mm (IPC-7351 nominal), on the 0.01 mm grid, and the file is saved
in KiCad 10's format. Run again after re-fetching a part.
"""
import glob
import os

import pcbnew

MARGIN = 0.25
_KEEP = []
LIB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib", "power_s.pretty")


def fix(path):
    name = os.path.splitext(os.path.basename(path))[0]
    fp = pcbnew.FootprintLoad(LIB, name)
    xs, ys = [], []
    for p in fp.Pads():
        bb = p.GetBoundingBox()
        xs += [bb.GetLeft(), bb.GetRight()]
        ys += [bb.GetTop(), bb.GetBottom()]
    for item in fp.GraphicalItems():
        if item.GetLayer() in (pcbnew.F_Fab, pcbnew.F_SilkS) and item.GetClass() != "PCB_TEXT":
            bb = item.GetBoundingBox()
            xs += [bb.GetLeft(), bb.GetRight()]
            ys += [bb.GetTop(), bb.GetBottom()]
    for item in list(fp.GraphicalItems()):
        if item.GetLayer() == pcbnew.F_CrtYd:
            fp.Remove(item)
            item.thisown = False           # see pcblib.Board._remove
            _KEEP.append(item)
    m = pcbnew.FromMM(MARGIN)
    g = pcbnew.FromMM(0.01)
    x0 = (min(xs) - m) // g * g
    y0 = (min(ys) - m) // g * g
    x1 = -((-(max(xs) + m)) // g) * g
    y1 = -((-(max(ys) + m)) // g) * g
    rect = pcbnew.PCB_SHAPE(fp, pcbnew.SHAPE_T_RECT)
    rect.SetStart(pcbnew.VECTOR2I(int(x0), int(y0)))
    rect.SetEnd(pcbnew.VECTOR2I(int(x1), int(y1)))
    rect.SetLayer(pcbnew.F_CrtYd)
    rect.SetWidth(pcbnew.FromMM(0.05))
    fp.Add(rect)
    io = pcbnew.PCB_IO_MGR.FindPlugin(pcbnew.PCB_IO_MGR.KICAD_SEXP)
    io.FootprintSave(LIB, fp)
    print(f"{name}: courtyard {pcbnew.ToMM(x1 - x0):.2f} x {pcbnew.ToMM(y1 - y0):.2f} mm")


if __name__ == "__main__":
    for path in sorted(glob.glob(os.path.join(LIB, "*.kicad_mod"))):
        fix(path)

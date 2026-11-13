"""Model S power board -- routing.

    . ./kicad-env.sh && kpy route_board.py              # redraw: scripted copper + stored autoroute
    . ./kicad-env.sh && kpy route_board.py --autoroute  # re-run Freerouting (Java, ~/tools/freerouting)

Run after place_board.py and setup_fab.py. Idempotent: every run removes all copper and
draws it again, so the board is the same every time.

Scripted here (the geometry is the design):
  * DCDC1 switch node: AXP2101 LX1 (pin 22) to L1, 0.8 mm, top only, shortest path.
  * +3V3: L1 to its two 22 uF caps, then a 0.8 mm trunk to J3 pins 11-13.
  * VBAT: J2 pin 1 to J3 pins 1-4 (1.0 mm) and to its 10 uF; the battery feeds the
    main board's speaker amp, motor and LTE straight from here.
  * VBUS: each pair of USB-C VBUS pins to its side's caps (C1 left, TVS + C23 right).
  * Ground: 3 x 3 vias in the AXP2101 exposed pad (its heat path, X-Powers guide 5.1),
    one via beside every other ground pad, stitching vias in open board, and a ground
    pour on both layers. The bottom layer is the ground plane.
Everything else is Freerouting's, stored in routing/autoroute.json and redrawn by replay().
"""
import math
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import pcbnew                                   # noqa: E402
from routelib import Router, F, B, say          # noqa: E402

NAME = "power-s"
W, H = 54.0, 36.0
LX = "Net-(U1-LX1)"

VIA, DRILL = 0.6, 0.3


def scripted_copper(r):
    # ---- DCDC1 switch node ------------------------------------------------
    lx = r.pad("U1", 22, expect=(24.5, 18.6), net=LX)
    l1 = r.pad("L1", 1, expect=(22.23, 16.6), net=LX)
    r.track(LX, [lx, (23.4, 18.6)], 0.25)
    r.track(LX, [(23.4, 18.6), (22.6, 17.8), l1], 0.8)

    # ---- +3V3: L1 -> C12, C13 -> J3 11/12/13 ------------------------------
    l2 = r.pad("L1", 2, expect=(18.17, 16.6), net="+3V3")
    c12 = r.pad("C12", 1, expect=(15.9, 16.15), net="+3V3")
    c13 = r.pad("C13", 1, expect=(15.9, 19.75), net="+3V3")
    j11 = r.pad("J3", 11, expect=(25.09, 7.15), net="+3V3")
    j12 = r.pad("J3", 12, expect=(25.09, 3.25), net="+3V3")
    j13 = r.pad("J3", 13, expect=(26.36, 7.15), net="+3V3")
    r.track("+3V3", [l2, (c12[0], l2[1])], 0.8)
    r.track("+3V3", [(17.3, l2[1]), (17.3, c13[1]), c13], 0.8)
    r.track("+3V3", [l2, (l2[0], 11.0), (24.2, 11.0), (j11[0], 10.1), j11], 0.8)
    r.track("+3V3", [j11, j12], 0.6)
    r.track("+3V3", [(j11[0], 9.2), (j13[0], 9.2), j13], 0.6)

    # ---- VBAT: J2.1 -> J3 1-4, and C24 -----------------------------------
    b1 = r.pad("J2", 1, expect=(8.05, 20.0), net="VBAT")
    c24 = r.pad("C24", 1, expect=(11.6, 20.22), net="VBAT")
    j1 = r.pad("J3", 1, expect=(18.75, 7.15), net="VBAT")
    j2 = r.pad("J3", 2, expect=(18.75, 3.25), net="VBAT")
    j3 = r.pad("J3", 3, expect=(20.02, 7.15), net="VBAT")
    j4 = r.pad("J3", 4, expect=(20.02, 3.25), net="VBAT")
    r.track("VBAT", [b1, (9.3, b1[1]), (9.3, 9.6), (j1[0], 9.6), j1], 1.0)
    r.track("VBAT", [(j1[0], 9.6), (j3[0], 9.6), j3], 0.7)
    r.track("VBAT", [j1, j2], 0.7)
    r.track("VBAT", [j3, j4], 0.7)
    r.track("VBAT", [(9.3, b1[1]), (c24[0], b1[1]), c24], 0.8)

    # ---- VBUS: USB-C left pair -> C1, right pair -> D2 + C23 ----------------
    a4 = r.pad("J1", "A4", expect=(37.55, 27.78), net="VBUS")
    a9 = r.pad("J1", "A9", expect=(42.45, 27.78), net="VBUS")
    c1 = r.pad("C1", 1, expect=(28.65, 25.05), net="VBUS")
    d2 = r.pad("D2", 1, expect=(47.6, 26.9), net="VBUS")
    c23 = r.pad("C23", 1, expect=(47.6, 30.75), net="VBUS")
    r.track("VBUS", [a4, (a4[0], 26.2), (29.9, 26.2), (29.9, c1[1]), c1], 0.6)
    r.track("VBUS", [a9, (a9[0], 26.2), (45.9, 26.2), (45.9, c23[1]), c23], 0.6)
    r.track("VBUS", [(45.9, d2[1]), d2], 0.6)


def ground_vias(r):
    """EP array, one via per ground pad, then stitching in open board."""
    ep = r.pad("U1", 41, expect=(27.0, 20.0), net="GND")
    vias = []
    for dx in (-1.0, 0.0, 1.0):
        for dy in (-1.0, 0.0, 1.0):
            vias.append(r.via("GND", (ep[0] + dx, ep[1] + dy), VIA, DRILL))
    r.zone_connection("U1", 41, pcbnew.ZONE_CONNECTION_FULL)
    g3 = r.pad("U1", 3, expect=(29.5, 21.0), net="GND")       # the GND pin, onto the EP
    r.track("GND", [g3, (ep[0] + 1.4, g3[1])], 0.2)

    # every copper object a new via must clear: pads of other nets, scripted tracks
    pads = []
    for ref, fp in r.fps.items():
        for p in fp.Pads():
            bb = p.GetBoundingBox()
            pads.append((ref, p.GetNumber(), p.GetNetname(),
                         pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetTop()) - r.oy,
                         pcbnew.ToMM(bb.GetRight()) - r.ox, pcbnew.ToMM(bb.GetBottom()) - r.oy))
    tracks = []
    for t in r.board.GetTracks():
        if t.GetClass() == "PCB_VIA":
            continue
        s, e = t.GetStart(), t.GetEnd()
        tracks.append((pcbnew.ToMM(s.x) - r.ox, pcbnew.ToMM(s.y) - r.oy,
                       pcbnew.ToMM(e.x) - r.ox, pcbnew.ToMM(e.y) - r.oy, pcbnew.ToMM(t.GetWidth()) / 2))
    holes = [(3.0, 3.0), (W - 3.0, 3.0), (3.0, H - 3.0), (W - 3.0, H - 3.0)]
    bodies = []                         # no vias under parts that sit flat on the board
    for ref in ("U1", "U2", "U3", "U4", "L1", "J1", "SW1", "Y1"):
        cy = r.fps[ref].GetCourtyard(pcbnew.F_CrtYd).BBox()
        bodies.append((ref, pcbnew.ToMM(cy.GetLeft()) - r.ox, pcbnew.ToMM(cy.GetTop()) - r.oy,
                       pcbnew.ToMM(cy.GetRight()) - r.ox, pcbnew.ToMM(cy.GetBottom()) - r.oy))

    def clear(x, y, own=None, gap=0.2):
        rad = VIA / 2
        if not (0.3 + rad + 0.2 <= x <= W - 0.3 - rad - 0.2 and 0.3 + rad + 0.2 <= y <= H - 0.3 - rad - 0.2):
            return False
        for hx, hy in holes:
            if math.hypot(x - hx, y - hy) < 1.1 + rad + 0.3:
                return False
        for ref, x0, y0, x1, y1 in bodies:
            if ref != own and x0 - rad < x < x1 + rad and y0 - rad < y < y1 + rad:
                return False
        for ref, num, net, x0, y0, x1, y1 in pads:
            dx = max(x0 - x, 0, x - x1)
            dy = max(y0 - y, 0, y - y1)
            if math.hypot(dx, dy) < rad + (gap if net != "GND" else 0.15):
                return False
        for x0, y0, x1, y1, hw in tracks:
            vx, vy = x1 - x0, y1 - y0
            L2 = vx * vx + vy * vy
            u = 0 if L2 == 0 else max(0, min(1, ((x - x0) * vx + (y - y0) * vy) / L2))
            if math.hypot(x - (x0 + u * vx), y - (y0 + u * vy)) < rad + hw + gap:
                return False
        for vx_, vy_ in vias:
            if math.hypot(x - vx_, y - vy_) < VIA + 0.25:
                return False
        return True

    placed = 0
    for ref, num, net, x0, y0, x1, y1 in list(pads):
        if net != "GND" or ref == "U1" or ref.startswith("TP"):
            continue
        cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
        fp = r.fps[ref]
        fx = pcbnew.ToMM(fp.GetPosition().x) - r.ox
        fy = pcbnew.ToMM(fp.GetPosition().y) - r.oy
        away = math.atan2(cy - fy, cx - fx) if (abs(cy - fy) + abs(cx - fx)) > 0.05 else -math.pi / 2
        reach = max(x1 - x0, y1 - y0) / 2 + VIA / 2 + 0.2
        done = False
        for extra in (0.0, 0.3, 0.6, 1.0):
            for k in (0, 1, -1, 2, -2, 3, -3, 4):
                a = away + k * math.pi / 4
                x = round(cx + (reach + extra) * math.cos(a), 2)
                y = round(cy + (reach + extra) * math.sin(a), 2)
                if clear(x, y, own=ref):
                    vias.append(r.via("GND", (x, y), VIA, DRILL))
                    placed += 1
                    done = True
                    break
            if done:
                break
        if not done:
            say("  no room for a ground via at %s.%s" % (ref, num))
    # stitching: a 4 mm grid wherever it is clear by a margin (room left for routing)
    stitch = 0
    for gx in range(2, int(W), 4):
        for gy in range(2, int(H), 4):
            x, y = gx + 0.0, gy + 0.0
            if clear(x, y, gap=0.8):
                vias.append(r.via("GND", (x, y), VIA, DRILL))
                stitch += 1
    say("ground vias: 9 in the exposed pad, %d at pads, %d stitching" % (placed, stitch))


def main():
    r = Router(__file__, NAME, size=(W, H))
    r.clear()
    scripted_copper(r)
    r.check_moved()
    ground_vias(r)
    r.zone("GND", F, "GND top", clearance=0.2)
    r.zone("GND", B, "GND bottom", clearance=0.2)
    r.normalise()
    if "--autoroute" in sys.argv:
        r.autoroute(scripted_nets=["GND", LX], passes=40, tries=4, best_of=3)
    n = r.replay()
    say("replayed %d autorouted items" % n)
    r.fill()
    r.save()
    r.drc_report()
    r.report()


if __name__ == "__main__":
    main()

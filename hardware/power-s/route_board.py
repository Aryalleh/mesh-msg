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
import re
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
    l1 = r.pad("L1", 1, expect=(21.43, 16.6), net=LX)
    r.track(LX, [lx, (23.4, 18.6)], 0.25)
    r.track(LX, [(23.4, 18.6), (22.6, 17.8), l1], 0.8)

    # ---- +3V3: L1 -> C12, C13 -> J3 11/12/13 ------------------------------
    l2 = r.pad("L1", 2, expect=(17.37, 16.6), net="+3V3")
    c12 = r.pad("C12", 1, expect=(15.3, 16.15), net="+3V3")
    c13 = r.pad("C13", 1, expect=(15.3, 19.75), net="+3V3")
    j11 = r.pad("J3", 11, expect=(25.09, 7.15), net="+3V3")
    j12 = r.pad("J3", 12, expect=(25.09, 3.25), net="+3V3")
    j13 = r.pad("J3", 13, expect=(26.36, 7.15), net="+3V3")
    r.track("+3V3", [l2, (c12[0], l2[1])], 0.8)
    r.track("+3V3", [(16.7, l2[1]), (16.7, c13[1]), c13], 0.8)
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
    c1 = r.pad("C1", 1, expect=(28.9, 25.3), net="VBUS")
    d2 = r.pad("D2", 1, expect=(47.6, 26.9), net="VBUS")
    c23 = r.pad("C23", 1, expect=(47.6, 30.75), net="VBUS")
    r.track("VBUS", [a4, (a4[0], 26.2), (29.9, 26.2), (29.9, c1[1]), c1], 0.6)
    r.track("VBUS", [a9, (a9[0], 26.2), (45.9, 26.2), (45.9, c23[1]), c23], 0.6)
    r.track("VBUS", [(45.9, d2[1]), d2], 0.6)
    # USB-C ground pins straight onto the shell tabs (the top pour cannot reach between them)
    for g, sh in (("A1", (35.68, 28.70)), ("A12", (44.32, 28.70))):
        gp = r.pad("J1", g, net="GND")
        r.track("GND", [gp, (gp[0], 28.3), (sh[0] + (0.3 if sh[0] < gp[0] else -0.3), 28.7)], 0.3)


def exposed_pad(r):
    """3 x 3 vias in the AXP2101 exposed pad, and its GND pin onto the pad."""
    ep = r.pad("U1", 41, expect=(27.0, 20.0), net="GND")
    for dx in (-1.0, 0.0, 1.0):
        for dy in (-1.0, 0.0, 1.0):
            r.via("GND", (ep[0] + dx, ep[1] + dy), VIA, DRILL)
    r.zone_connection("U1", 41, pcbnew.ZONE_CONNECTION_FULL)
    g3 = r.pad("U1", 3, expect=(29.5, 21.0), net="GND")
    r.track("GND", [g3, (ep[0] + 1.4, g3[1])], 0.2)


def fanout(r):
    """The AXP2101's 0.4 mm-pitch pins out to their capacitors and to escape points.
    Freerouting cannot do this part (it draws a net at one width and boxes pins in), so
    every U1 pin except the exposed pad is drawn here and the autorouter starts outside.

    Top: the eight LDO/LDO-input pins step out in a staircase -- each turns at its own
    level, 0.4 mm apart, the outermost nearest the package -- to a capacitor row at 1.3 mm
    pitch. Bottom: VBAT/VSYS left to their caps, VMID/VBUS straight down, I2C/IRQ out to
    the right, the NTC line through a via. Sides: short stubs; FB1 and PWROK through vias.
    """
    W2 = 0.2

    def pin(n, net, xy):
        return r.pad("U1", n, expect=xy, net=net)

    def cap(ref, net, xy):
        return r.pad(ref, 1, expect=xy, net=net)

    # ---- top, pins at y 17.5, tips at 17.1 ---------------------------------
    stairs = [  # pin, net, staircase level, capacitor
        (19, "3V3_LORA", 16.85, ("C14", (22.95, 14.28))),
        (18, "VSPARE", 16.45, ("C19", (24.25, 14.28))),
        (17, "VSYS", 16.05, ("C5", (25.55, 14.28))),
        (16, "3V3_AUDIO", None, ("C15", (26.8, 14.28))),
        (15, "3V3_SD", 15.65, ("C16", (28.15, 14.28))),
        (14, "3V3_GPS", 16.05, ("C18", (29.45, 14.28))),
        (13, "VSYS", 16.45, ("C10", (30.75, 14.11))),
        (12, "3V3_LCD", 16.85, ("C17", (32.05, 14.28))),
    ]
    xs = {19: 25.6, 18: 26.0, 17: 26.4, 16: 26.8, 15: 27.2, 14: 27.6, 13: 28.0, 12: 28.4}
    for n, net, lvl, (cref, cxy) in stairs:
        p = pin(n, net, (xs[n], 17.5))
        c = cap(cref, net, cxy)
        if lvl is None:
            r.track(net, [p, c], W2)
        else:
            r.track(net, [p, (p[0], lvl), (c[0], lvl), c], W2)

    # ---- bottom, pins at y 22.5, tips at 22.9 -------------------------------
    p = pin(33, "VBAT", (26.0, 22.5))
    c = cap("C11", "VBAT", (23.8, 24.92))
    r.track("VBAT", [p, (p[0], 23.95), (c[0], 23.95), c], W2)
    p = pin(34, "VSYS", (26.4, 22.5))
    c = cap("C6", "VSYS", (24.9, 24.92))
    r.track("VSYS", [p, (p[0], 24.35), (c[0], 24.35), c], W2)
    p = pin(36, "/VMID", (27.2, 22.5))
    c = cap("C2", "/VMID", (26.6, 25.3))
    r.track("/VMID", [p, (p[0], 25.3)], W2)
    p = pin(37, "VBUS", (27.6, 22.5))
    c = cap("C1", "VBUS", (28.9, 25.3))
    r.track("VBUS", [p, (p[0], 24.6), (c[0], 24.6), c], W2)
    # I2C and IRQ: a staircase to the right, each ending in its own via (staggered so
    # each has a way out; the autorouter boxed a shared exit in)
    for n, net, lvl, tail in ((40, "/I2C_SCL", 23.15, [(30.3, 23.15), (30.9, 22.75)]),
                              (39, "/I2C_SDA", 23.55, [(31.2, 23.55)]),
                              (38, "/PMU_IRQ", 23.95, [(30.3, 23.95), (30.6, 24.25), (30.6, 24.6)])):
        p = pin(n, net, ({40: 28.8, 39: 28.4, 38: 28.0}[n], 22.5))
        r.track(net, [p, (p[0], lvl)] + tail, W2)
        r.via(net, tail[-1], VIA, DRILL)
    p = pin(31, "/BAT_NTC", (25.2, 22.5))
    r.track("/BAT_NTC", [p, (p[0], 23.35)], W2)
    r.via("/BAT_NTC", (p[0], 23.35), VIA, DRILL)

    # ---- left, pins at x 24.5, tips at 24.1 ---------------------------------
    p = pin(21, "+3V3", (24.5, 18.2))                       # FB1: senses +3V3, via to the bottom
    r.track("+3V3", [p, (24.05, p[1]), (23.9, 17.6)], W2)
    r.via("+3V3", (23.9, 17.6), VIA, DRILL)
    a, b = pin(23, "VSYS", (24.5, 19.0)), pin(24, "VSYS", (24.5, 19.4))
    c = r.pad("C7", 1, expect=(22.98, 20.2), net="VSYS")
    r.track("VSYS", [a, (24.0, a[1]), (24.0, b[1]), (23.3, 20.1), c], W2)
    r.track("VSYS", [b, (24.0, b[1])], W2)
    p = pin(28, "/AXP_VRTC", (24.5, 21.0))
    c = r.pad("C21", 1, expect=(22.68, 21.4), net="/AXP_VRTC")
    r.track("/AXP_VRTC", [p, (23.2, p[1]), (22.8, 21.4), c], W2)
    p = pin(29, "/PMU_PWROK", (24.5, 21.4))
    r.track("/PMU_PWROK", [p, (23.9, p[1]), (23.4, 21.9), (23.4, 22.1)], W2)
    r.via("/PMU_PWROK", (23.4, 22.1), VIA, DRILL)
    p = pin(30, "/PMU_ONOFF", (24.5, 21.8))
    c = r.pad("C22", 1, expect=(21.88, 23.3), net="/PMU_ONOFF")
    r3 = r.pad("R3", 1, expect=(21.91, 24.5), net="/PMU_ONOFF")
    r.track("/PMU_ONOFF", [p, (24.0, p[1]), (24.0, 22.4), (23.4, 23.0), (c[0], 23.0), c, r3], W2)

    # ---- right, pins at x 29.5, tips at 29.9 --------------------------------
    a, b = pin(7, "VSYS", (29.5, 19.4)), pin(6, "VSYS", (29.5, 19.8))
    c8 = r.pad("C8", 1, expect=(31.52, 19.65), net="VSYS")
    c9 = r.pad("C9", 1, expect=(31.52, 18.45), net="VSYS")
    r.track("VSYS", [a, (30.0, a[1]), (30.0, b[1]), b], W2)
    r.track("VSYS", [(30.0, 19.65), c8], 0.3)
    r.track("VSYS", [(30.6, 19.65), (30.6, c9[1]), c9], 0.3)
    p = pin(2, "/VREF", (29.5, 21.4))
    c = r.pad("C20", 1, expect=(31.52, 21.4), net="/VREF")
    r.track("/VREF", [p, c], W2)
    p = pin(1, "/CHGLED", (29.5, 21.8))
    d = r.pad("D1", 1, expect=(32.21, 22.6), net="/CHGLED")
    r.track("/CHGLED", [p, (30.4, p[1]), (30.75, 22.15), (31.6, 22.15), d], W2)


def ground_vias(r, pads_=True, stitching=True):
    """After routing: one via beside every ground pad, then stitching in open board.
    Done last so the autorouter has every channel; each via is checked against all
    copper already on the board."""
    vias = []

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
            q = t.GetPosition()
            if t.GetNetname() == "GND":
                vias.append((pcbnew.ToMM(q.x) - r.ox, pcbnew.ToMM(q.y) - r.oy))
            else:                            # another net's via: a zero-length "track"
                x, y = pcbnew.ToMM(q.x) - r.ox, pcbnew.ToMM(q.y) - r.oy
                tracks.append((x, y, x, y, pcbnew.ToMM(t.GetWidth()) / 2))
            continue
        if t.GetNetname() == "GND":
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
    for ref, num, net, x0, y0, x1, y1 in (list(pads) if pads_ else []):
        if net != "GND" or ref == "U1" or ref.startswith("TP"):
            continue
        cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
        fp = r.fps[ref]
        fx = pcbnew.ToMM(fp.GetPosition().x) - r.ox
        fy = pcbnew.ToMM(fp.GetPosition().y) - r.oy
        away = math.atan2(cy - fy, cx - fx) if (abs(cy - fy) + abs(cx - fx)) > 0.05 else -math.pi / 2
        reach = max(x1 - x0, y1 - y0) / 2 + VIA / 2 + 0.2
        done = False
        for extra in (0.0, 0.15, 0.3, 0.45, 0.6, 0.8, 1.0, 1.3):
            for k in (0, 0.5, -0.5, 1, -1, 1.5, -1.5, 2, -2, 2.5, -2.5, 3, -3, 4):
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
    for gx in (range(2, int(W), 4) if stitching else []):
        for gy in range(2, int(H), 4):
            x, y = gx + 0.0, gy + 0.0
            if clear(x, y, gap=0.8):
                vias.append(r.via("GND", (x, y), VIA, DRILL))
                stitch += 1
    say("ground vias: %d at pads, %d stitching" % (placed, stitch))


def neck_down(r, ref="U1", reach=2.5):
    """Freerouting draws a whole net at its class width, and a 0.3 mm track cannot enter a
    0.2 mm pad at 0.4 mm pitch: it stops short. Close each such gap with a 0.2 mm neck:
    straight out of the pin past its tip, then to the nearest end of that net's track."""
    fp = r.fps[ref]
    c = fp.GetPosition()
    cx, cy = pcbnew.ToMM(c.x) - r.ox, pcbnew.ToMM(c.y) - r.oy
    ends = {}
    for t in r.board.GetTracks():
        if t.GetClass() == "PCB_VIA" or t.GetLayer() != F:
            continue
        for q in (t.GetStart(), t.GetEnd()):
            ends.setdefault(t.GetNetname(), []).append((pcbnew.ToMM(q.x) - r.ox, pcbnew.ToMM(q.y) - r.oy))
    for t in r.board.GetTracks():
        if t.GetClass() == "PCB_VIA":
            q = t.GetPosition()
            ends.setdefault(t.GetNetname(), []).append((pcbnew.ToMM(q.x) - r.ox, pcbnew.ToMM(q.y) - r.oy))
    added = []
    for p in fp.Pads():
        net = p.GetNetname()
        if not net or net == "GND" or net.startswith("unconnected") or p.GetNumber() == "41":
            continue
        bb = p.GetBoundingBox()
        x0, y0 = pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetTop()) - r.oy
        x1, y1 = pcbnew.ToMM(bb.GetRight()) - r.ox, pcbnew.ToMM(bb.GetBottom()) - r.oy
        if any(x0 - 0.01 <= x <= x1 + 0.01 and y0 - 0.01 <= y <= y1 + 0.01 for x, y in ends.get(net, [])):
            continue                                   # already reached
        px, py = (x0 + x1) / 2, (y0 + y1) / 2
        # outward direction: the side of the package this pin is on
        if x1 - x0 > y1 - y0:                          # left/right pin
            dx, dy = (1 if px > cx else -1), 0
            tip = (x1 if dx > 0 else x0) + 0.25 * dx, py
        else:
            dx, dy = 0, (1 if py > cy else -1)
            tip = px, (y1 if dy > 0 else y0) + 0.25 * dy
        cand = [e for e in ends.get(net, []) if math.hypot(e[0] - tip[0], e[1] - tip[1]) < reach]
        if not cand:
            say("  neck_down: %s.%s (%s) has no track end within %.1f mm" % (ref, p.GetNumber(), net, reach))
            continue
        e = min(cand, key=lambda e: math.hypot(e[0] - tip[0], e[1] - tip[1]))
        r.track(net, [(px, py), tip, e], 0.2)
        added.append("%s.%s" % (ref, p.GetNumber()))
    say("neck-downs at %s" % (", ".join(added) or "none"))


POWER_NETS = {"VBUS", "VSYS", "VBAT", "+3V3", "/VMID", "3V3_LORA", "3V3_AUDIO", "3V3_SD",
              "3V3_LCD", "3V3_GPS", "VSPARE", "VRTC", "/AXP_VRTC"}


def _seg_dist(a, b, c, d):
    """Shortest distance between segments ab and cd (2D)."""
    def pt_seg(p, q, r_):
        vx, vy = r_[0] - q[0], r_[1] - q[1]
        L2 = vx * vx + vy * vy
        u = 0 if L2 == 0 else max(0, min(1, ((p[0] - q[0]) * vx + (p[1] - q[1]) * vy) / L2))
        return math.hypot(p[0] - (q[0] + u * vx), p[1] - (q[1] + u * vy))

    def cross(o, p, q):
        return (p[0] - o[0]) * (q[1] - o[1]) - (p[1] - o[1]) * (q[0] - o[0])
    if (cross(a, b, c) * cross(a, b, d) < 0) and (cross(c, d, a) * cross(c, d, b) < 0):
        return 0.0
    return min(pt_seg(a, c, d), pt_seg(b, c, d), pt_seg(c, a, b), pt_seg(d, a, b))


def widen(r, width=0.5, gap=0.2):
    """Widen every power/rail track drawn narrower than `width` wherever it keeps `gap`
    (a little over the 0.15 mm rule) from other nets' copper on its layer."""
    def mmxy(v):
        return (pcbnew.ToMM(v.x) - r.ox, pcbnew.ToMM(v.y) - r.oy)
    copper = []                                  # (layer set, net, kind, geometry, half-width)
    for t in r.board.GetTracks():
        if t.GetClass() == "PCB_VIA":
            q = mmxy(t.GetPosition())
            copper.append([{F, B}, t.GetNetname(), "seg", (q, q), 0.3, t])
        else:
            copper.append([{t.GetLayer()}, t.GetNetname(), "seg",
                           (mmxy(t.GetStart()), mmxy(t.GetEnd())), pcbnew.ToMM(t.GetWidth()) / 2, t])
    for fp in r.fps.values():
        for p in fp.Pads():
            bb = p.GetBoundingBox()
            x0, y0 = pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetTop()) - r.oy
            x1, y1 = pcbnew.ToMM(bb.GetRight()) - r.ox, pcbnew.ToMM(bb.GetBottom()) - r.oy
            layers = {F, B} if p.GetAttribute() == pcbnew.PAD_ATTRIB_PTH else {F}
            copper.append([layers, p.GetNetname(), "box", (x0, y0, x1, y1), 0.0, None])

    def box_dist(seg, box):
        (a, b), (x0, y0, x1, y1) = seg, box
        if x0 <= a[0] <= x1 and y0 <= a[1] <= y1:
            return 0.0
        edges = [((x0, y0), (x1, y0)), ((x1, y0), (x1, y1)), ((x1, y1), (x0, y1)), ((x0, y1), (x0, y0))]
        return min(_seg_dist(a, b, c, d) for c, d in edges)

    n = 0
    for entry in copper:
        layers, net, kind, geo, hw, t = entry
        if t is None or t.GetClass() == "PCB_VIA" or net not in POWER_NETS or hw * 2 >= width - 1e-6:
            continue
        lay = t.GetLayer()
        ok = True
        for l2, n2, k2, g2, h2, t2 in copper:
            if n2 == net or lay not in l2 or t2 is t:
                continue
            d = _seg_dist(geo[0], geo[1], *g2) if k2 == "seg" else box_dist(geo, g2)
            if d - h2 < width / 2 + gap:
                ok = False
                break
        if ok:
            t.SetWidth(pcbnew.FromMM(width))
            entry[4] = width / 2                 # later checks see the new width
            n += 1
    say("widened %d power/rail track segments to %.1f mm" % (n, width))


def _obstacles(r, net):
    obs = {F: [], B: []}
    for t in r.board.GetTracks():
        if t.GetNetname() == net:
            continue
        if t.GetClass() == "PCB_VIA":
            q = t.GetPosition()
            x, y = pcbnew.ToMM(q.x) - r.ox, pcbnew.ToMM(q.y) - r.oy
            for L in (F, B):
                obs[L].append(("seg", (x, y), (x, y), 0.3))
        else:
            a_, b_ = t.GetStart(), t.GetEnd()
            obs[t.GetLayer()].append(("seg", (pcbnew.ToMM(a_.x) - r.ox, pcbnew.ToMM(a_.y) - r.oy),
                                      (pcbnew.ToMM(b_.x) - r.ox, pcbnew.ToMM(b_.y) - r.oy),
                                      pcbnew.ToMM(t.GetWidth()) / 2))
    for fp in r.fps.values():
        for p in fp.Pads():
            if p.GetNetname() == net and net:
                continue
            bb = p.GetBoundingBox()
            box = ("box", (pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetTop()) - r.oy),
                   (pcbnew.ToMM(bb.GetRight()) - r.ox, pcbnew.ToMM(bb.GetBottom()) - r.oy), 0.0)
            for L in (F, B):
                if p.IsOnLayer(L) or p.GetAttribute() in (pcbnew.PAD_ATTRIB_PTH, pcbnew.PAD_ATTRIB_NPTH):
                    obs[L].append(box)
    for hx, hy in ((3.0, 3.0), (W - 3.0, 3.0), (3.0, H - 3.0), (W - 3.0, H - 3.0)):
        for L in (F, B):
            obs[L].append(("seg", (hx, hy), (hx, hy), 1.1))
    return obs


def _odist(x, y, o):
    kind, a_, b_, hw = o
    if kind == "box":
        return math.hypot(max(a_[0] - x, 0, x - b_[0]), max(a_[1] - y, 0, y - b_[1]))
    vx, vy = b_[0] - a_[0], b_[1] - a_[1]
    L2 = vx * vx + vy * vy
    u = 0 if L2 == 0 else max(0, min(1, ((x - a_[0]) * vx + (y - a_[1]) * vy) / L2))
    return math.hypot(x - (a_[0] + u * vx), y - (a_[1] + u * vy)) - hw


def find_path(r, net, starts, goals, width=0.2, clearance=0.16, step=0.05, margin=5.0, via_cost=20,
              free_ends=True):
    """A* over both layers from any of `starts` to any of `goals` (each (x, y, layer)):
    8 directions on a `step` grid, a via where cheaper than going round, keeping
    `clearance` from every other net. Draws the result and returns True, or False."""
    import heapq
    allobs = _obstacles(r, net)
    pts = starts + goals
    x0, y0 = min(p[0] for p in pts) - margin, min(p[1] for p in pts) - margin
    x1, y1 = max(p[0] for p in pts) + margin, max(p[1] for p in pts) + margin
    win = lambda o: (min(o[1][0], o[2][0]) - 1.5 < x1 and max(o[1][0], o[2][0]) + 1.5 > x0
                     and min(o[1][1], o[2][1]) - 1.5 < y1 and max(o[1][1], o[2][1]) + 1.5 > y0)
    obs = {L: [o for o in allobs[L] if win(o)] for L in (F, B)}
    g = lambda p: (round((p[0] - x0) / step), round((p[1] - y0) / step))
    w = lambda c: (x0 + c[0] * step, y0 + c[1] * step)
    nx, ny = g((x1, y1))
    S = [g(p[:2]) + (p[2],) for p in starts]
    G = {g(p[:2]) + (p[2],): p for p in goals}
    gl = list(G)
    cache = {}

    def free(c, L, need):
        k = (c, L, need)
        if k not in cache:
            x, y = w(c)
            cache[k] = (0.5 < x < W - 0.5 and 0.5 < y < H - 0.5
                        and all(_odist(x, y, o) >= need for o in obs[L]))
        return cache[k]
    tneed, vneed = width / 2 + clearance, 0.3 + clearance
    ends = S + gl
    near = lambda c: free_ends and any(c[2] == t[2] and abs(c[0] - t[0]) + abs(c[1] - t[1]) <= 4 for t in ends)
    h = lambda c: min(math.hypot(c[0] - t[0], c[1] - t[1]) for t in gl)
    openq = [(h(c), c) for c in S]
    heapq.heapify(openq)
    came = {c: None for c in S}
    cost = {c: 0 for c in S}
    hit = None
    while openq:
        _, c = heapq.heappop(openq)
        if c in G:
            hit = c
            break
        steps = [((c[0] + dx, c[1] + dy, c[2]), 1.414 if dx and dy else 1.0)
                 for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1), (1, 1), (1, -1), (-1, 1), (-1, -1))]
        steps.append(((c[0], c[1], B if c[2] == F else F), via_cost))
        for n2, base in steps:
            if not (0 <= n2[0] <= nx and 0 <= n2[1] <= ny):
                continue
            if n2[2] != c[2]:
                if not (free(n2[:2], F, vneed) and free(n2[:2], B, vneed)):
                    continue
            elif not (near(n2) or free(n2[:2], n2[2], tneed)):
                continue
            prev = came[c]
            turn = 0 if prev is None or prev[2] != c[2] or n2[2] != c[2] or \
                (c[0] - prev[0], c[1] - prev[1]) == (n2[0] - c[0], n2[1] - c[1]) else 0.6
            nc = cost[c] + base + turn + (0.3 if n2[2] == B else 0)
            if nc < cost.get(n2, 1e18):
                cost[n2] = nc
                came[n2] = c
                heapq.heappush(openq, (nc + h(n2), n2))
    if hit is None:
        return False
    cells = []
    c = hit
    while c is not None:
        cells.append(c)
        c = came[c]
    cells.reverse()
    start_pt = next(p for p in starts if g(p[:2]) + (p[2],) == cells[0])
    goal_pt = G[hit]
    if net == "GND":
        say("    bridge %s -> %s, %d cells" % (start_pt, goal_pt, len(cells)))
    runs, cur = [], [cells[0]]
    for c in cells[1:]:
        if c[2] != cur[-1][2]:
            runs.append(cur)
            r.via(net, tuple(round(v, 3) for v in w(c[:2])), VIA, DRILL)
            cur = [c]
        else:
            cur.append(c)
    runs.append(cur)
    for k, run in enumerate(runs):
        pts_ = [start_pt[:2] if k == 0 else tuple(round(v, 3) for v in w(run[0][:2]))]
        for i in range(1, len(run) - 1):
            d1 = (run[i][0] - run[i - 1][0], run[i][1] - run[i - 1][1])
            d2 = (run[i + 1][0] - run[i][0], run[i + 1][1] - run[i][1])
            if d1 != d2:
                pts_.append(tuple(round(v, 3) for v in w(run[i][:2])))
        pts_.append(goal_pt[:2] if k == len(runs) - 1 else tuple(round(v, 3) for v in w(run[-1][:2])))
        r.track(net, pts_, width, run[0][2])
    return True


def close_opens(r):
    """Connect what is still open after the autorouter: KiCad's DRC names each open pair
    and a point on each side; find_path() joins them. GND is left to stitch_islands()."""
    import json
    import subprocess
    import tempfile
    tmp = tempfile.mkdtemp()
    path = os.path.join(tmp, NAME + ".kicad_pcb")
    for ext in (".kicad_pro", ".kicad_dru"):
        src = os.path.join(HERE, NAME + ext)
        if os.path.exists(src):
            open(os.path.join(tmp, NAME + ext), "wb").write(open(src, "rb").read())
    pcbnew.SaveBoard(path, r.board)
    rep = os.path.join(tmp, "drc.json")
    subprocess.run([r.cli, "pcb", "drc", "--format", "json", "--units", "mm", "-o", rep, path],
                   capture_output=True)
    lay = lambda d: B if "B.Cu" in d and "F.Cu" not in d else F
    fan_ends = {"/I2C_SCL": (30.9, 22.75), "/I2C_SDA": (31.2, 23.55), "/PMU_IRQ": (30.6, 24.6)}
    u1 = r.fps["U1"].GetPosition()
    u1 = (pcbnew.ToMM(u1.x) - r.ox, pcbnew.ToMM(u1.y) - r.oy)
    done, total = [], 0
    for u in json.load(open(rep)).get("unconnected_items", []):
        it = u["items"]
        d0, d1 = it[0]["description"], it[1]["description"]
        net = re.findall(r"\[([^\]]+)\]", d0)
        if not net or net[0] == "GND" or "Zone" in d0 + d1:
            continue
        net = net[0]
        ends = []
        for i, d in ((it[0], d0), (it[1], d1)):
            p = (i["pos"]["x"] - r.ox, i["pos"]["y"] - r.oy, lay(d))
            if net in fan_ends and math.hypot(p[0] - u1[0], p[1] - u1[1]) < 3.5:
                p = fan_ends[net] + (F,)            # reach a U1 pin through its fan-out
            ends.append(p)
        total += 1
        if find_path(r, net, [ends[0]], [ends[1]]):
            done.append(net)
        else:
            say("  close_opens: no path for %s %s -> %s" % (net, ends[0], ends[1]))
    say("close_opens: %d of %d connected (%s)" % (len(done), total, ", ".join(done)))


def ground_stubs(r, reach=1.3, width=0.3, gap=0.17):
    """Tie each ground pad to its nearest ground via with a straight stub where that
    stub clears every other net: a thermal-relief spoke can fail to land between
    tracks, a drawn track cannot. C7's ground pad, boxed in by the VIN tracks, goes to
    C21's instead."""
    def mm(v):
        return (pcbnew.ToMM(v.x) - r.ox, pcbnew.ToMM(v.y) - r.oy)
    gvias = [mm(t.GetPosition()) for t in r.board.GetTracks()
             if t.GetClass() == "PCB_VIA" and t.GetNetname() == "GND"]
    other = []
    for t in r.board.GetTracks():
        if t.GetNetname() == "GND":
            continue
        if t.GetClass() == "PCB_VIA":
            q = mm(t.GetPosition())
            other.append(("seg", (q, q), 0.3))
        elif t.GetLayer() == F:
            other.append(("seg", (mm(t.GetStart()), mm(t.GetEnd())), pcbnew.ToMM(t.GetWidth()) / 2))
    for fp in r.fps.values():
        for p in fp.Pads():
            if p.GetNetname() != "GND" and p.IsOnLayer(F):
                bb = p.GetBoundingBox()
                other.append(("box", (pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetTop()) - r.oy,
                                      pcbnew.ToMM(bb.GetRight()) - r.ox, pcbnew.ToMM(bb.GetBottom()) - r.oy), 0))

    def clear(a, b):
        for kind, g, hw in other:
            if kind == "seg":
                d = _seg_dist(a, b, g[0], g[1]) - hw
            else:
                x0, y0, x1, y1 = g
                if x0 <= a[0] <= x1 and y0 <= a[1] <= y1:
                    return False
                d = min(_seg_dist(a, b, c, e) for c, e in (((x0, y0), (x1, y0)), ((x1, y0), (x1, y1)),
                                                             ((x1, y1), (x0, y1)), ((x0, y1), (x0, y0))))
            if d < width / 2 + gap:
                return False
        return True
    n = 0
    for ref, fp in r.fps.items():
        if ref == "U1" or ref.startswith("TP"):
            continue
        for p in fp.Pads():
            if p.GetNetname() != "GND" or not p.IsOnLayer(F) or p.GetAttribute() != pcbnew.PAD_ATTRIB_SMD:
                continue
            c = mm(p.GetPosition())
            for v in sorted(gvias, key=lambda v: math.hypot(v[0] - c[0], v[1] - c[1])):
                if math.hypot(v[0] - c[0], v[1] - c[1]) > reach:
                    break
                if clear(c, v):
                    r.track("GND", [c, v], width)
                    n += 1
                    break
    a = r.pad("C7", 2, expect=(22.02, 20.2), net="GND")
    b = r.pad("C21", 2, expect=(21.72, 21.4), net="GND")
    r.track("GND", [a, b], 0.25)
    say("ground stubs: %d pad-to-via, plus C7-C21" % n)


def stitch_islands(r, margin=0.3, grid=0.1, rounds=3):
    """After the fill: the bottom plane is cut by bottom-layer tracks, so a group of pour
    pieces can end up joined to each other but not to the rest of ground. Find each such
    group and drop a via where one of its pieces overlaps a piece of the main group on
    the other layer, well inside both fills (so it clears every other net), then refill."""
    keep = []            # the polygon sets must outlive the outlines taken from them

    def pieces():
        out = []
        for z in r.board.Zones():
            if z.GetNetname() != "GND":
                continue
            L = z.GetLayer()
            ps = pcbnew.SHAPE_POLY_SET(z.GetFilledPolysList(L))
            keep.append(ps)
            for k in range(ps.OutlineCount()):
                out.append((L, ps.Outline(k)))
        return out
    added = 0
    bridged = set()
    for _ in range(rounds):
        pcs = pieces()
        vias = [t.GetPosition() for t in r.board.GetTracks()
                if t.GetClass() == "PCB_VIA" and t.GetNetname() == "GND"]
        parent = list(range(len(pcs)))

        def find(i):
            while parent[i] != i:
                parent[i] = parent[parent[i]]
                i = parent[i]
            return i
        for v in vias:
            hit = [i for i, (L, o) in enumerate(pcs) if o.PointInside(v)]
            for i in hit[1:]:
                parent[find(i)] = find(hit[0])
        # a ground track joins every piece its ends sit in, on its layer
        for t in r.board.GetTracks():
            if t.GetClass() == "PCB_VIA" or t.GetNetname() != "GND":
                continue
            ends_ = (t.GetStart(), t.GetEnd())
            hit = [i for i, (L, o) in enumerate(pcs) if L == t.GetLayer()
                   and (o.PointInside(ends_[0]) or o.PointInside(ends_[1]))]
            # through a via at either end, onto whatever that via sits in
            for v in vias:
                if any((v - e_).EuclideanNorm() < pcbnew.FromMM(0.05) for e_ in ends_):
                    hit += [i for i, (L, o) in enumerate(pcs) if o.PointInside(v)]
            for i in hit[1:]:
                parent[find(i)] = find(hit[0])
        groups = {}
        for i in range(len(pcs)):
            groups.setdefault(find(i), []).append(i)
        main_g = max(groups.values(), key=lambda g: sum(abs(pcs[i][1].Area()) for i in g))
        others = [g for g in groups.values() if g is not main_g]
        if not others:
            break
        for g in others:
            say("  ground group cut off: " + "; ".join(
                "%s %.1f mm2 at (%.1f, %.1f)" % ("top" if pcs[i][0] == F else "bot", abs(pcs[i][1].Area()) / 1e12,
                                                 pcbnew.ToMM(pcs[i][1].BBox().Centre().x) - r.ox,
                                                 pcbnew.ToMM(pcs[i][1].BBox().Centre().y) - r.oy) for i in g))
        progress = False
        m = pcbnew.FromMM(margin)
        for g in others:
            done = False
            for i in g:
                L, o = pcs[i]
                bb = o.BBox()
                step = pcbnew.FromMM(grid)
                x = bb.GetLeft()
                while x <= bb.GetRight() and not done:
                    y = bb.GetTop()
                    while y <= bb.GetBottom() and not done:
                        pt = pcbnew.VECTOR2I(int(x), int(y))
                        if o.PointInside(pt) and o.SquaredDistance(pt) >= m * m:
                            for j in main_g:
                                L2, o2 = pcs[j]
                                if L2 != L and o2.PointInside(pt) and o2.SquaredDistance(pt) >= m * m \
                                        and all((pt - v).EuclideanNorm() >= pcbnew.FromMM(0.9) for v in vias):
                                    r.via("GND", (pcbnew.ToMM(pt.x) - r.ox, pcbnew.ToMM(pt.y) - r.oy), VIA, DRILL)
                                    added += 1
                                    done = progress = True
                                    break
                        y += step
                    x += step
                if done:
                    break
        if not progress:
            # no overlap to drop a via in: route a ground bridge from a via inside the
            # group to the nearest main-ground via, around the other nets
            main_pts = [v for v in vias if any(pcs[j][1].PointInside(v) for j in main_g)]
            for g in others:
                own = [v for v in vias if any(pcs[i][1].PointInside(v) for i in g)]
                key = frozenset((v.x, v.y) for v in own)
                if not own or key in bridged:
                    continue
                bridged.add(key)
                st = [(pcbnew.ToMM(v.x) - r.ox, pcbnew.ToMM(v.y) - r.oy, F) for v in own]
                cx = sum(p[0] for p in st) / len(st)
                cy = sum(p[1] for p in st) / len(st)
                gl = [(pcbnew.ToMM(v.x) - r.ox, pcbnew.ToMM(v.y) - r.oy) for v in main_pts]
                gl = sorted(gl, key=lambda p: math.hypot(p[0] - cx, p[1] - cy))[:12]
                gl = [p + (L,) for p in gl for L in (F, B)]
                # and any point well inside the main pour within 7 mm, on its own layer
                deep = pcbnew.FromMM(0.45)
                for j in main_g:
                    L2, o2 = pcs[j]
                    for gx in range(int((cx - 7) * 2), int((cx + 7) * 2) + 1):
                        for gy in range(int((cy - 7) * 2), int((cy + 7) * 2) + 1):
                            x_, y_ = gx / 2.0, gy / 2.0
                            pt = pcbnew.VECTOR2I(pcbnew.FromMM(x_ + r.ox), pcbnew.FromMM(y_ + r.oy))
                            if o2.PointInside(pt) and o2.SquaredDistance(pt) >= deep * deep:
                                gl.append((x_, y_, L2))
                st = st + [(p[0], p[1], B) for p in st]
                if find_path(r, "GND", st, gl, width=0.3, step=0.1, margin=2.0, via_cost=15, free_ends=False):
                    added += 1
                    progress = True
        r.fill()
        if not progress:
            for g in others:
                desc = []
                for i in g:
                    L, o = pcs[i]
                    bb = o.BBox()
                    desc.append("%s %.0f mm2 at x %.1f..%.1f y %.1f..%.1f" % (
                        "top" if L == F else "bot", abs(o.Area()) / 1e12,
                        pcbnew.ToMM(bb.GetLeft()) - r.ox, pcbnew.ToMM(bb.GetRight()) - r.ox,
                        pcbnew.ToMM(bb.GetTop()) - r.oy, pcbnew.ToMM(bb.GetBottom()) - r.oy))
                say("  cut-off ground group: " + "; ".join(desc))
            break
    say("stitch_islands: %d via(s) joining cut-off ground" % added)


def power_key(r):
    """PWR_KEY is a push-button line: no hurry, but the autorouter ran it up the middle of
    the bottom plane and fenced in a piece of ground. It goes round the board edge on the
    bottom layer instead, from J3 pin 22 to SW1; close_opens() joins R3 to it."""
    for t in list(r.board.GetTracks()):
        if t.GetNetname() == "/PWR_KEY":
            r.board.Remove(t)
            r._removed.append(t)
    r.pad("J3", 22, expect=(31.44, 3.25), net="/PWR_KEY")
    sw = r.pad("SW1", 1, net="/PWR_KEY")
    sw = min((p for p in r.fps["SW1"].Pads() if p.GetNetname() == "/PWR_KEY"),
             key=lambda p: p.GetPosition().x)
    q = sw.GetPosition()
    sw = (round(pcbnew.ToMM(q.x) - r.ox, 3), round(pcbnew.ToMM(q.y) - r.oy, 3))
    # from x 17 along the edge; close_opens() makes the last link to J3 pin 22 between
    # whatever the autorouter put along the header
    r.track("/PWR_KEY", [(17.0, 0.75), (1.75, 0.75), (1.25, 1.25),
                         (1.25, sw[1] - 0.5), (1.75, sw[1]), (sw[0] - 1.2, sw[1])], 0.25, B)
    r.via("/PWR_KEY", (sw[0] - 1.2, sw[1]), VIA, DRILL)
    r.track("/PWR_KEY", [(sw[0] - 1.2, sw[1]), sw], 0.25)


def main():
    r = Router(__file__, NAME, size=(W, H))
    r.clear()
    scripted_copper(r)
    fanout(r)
    r.check_moved()
    exposed_pad(r)
    ground_vias(r, stitching=False)               # before routing: the router keeps them
    r.zone("GND", F, "GND top", clearance=0.2)
    r.zone("GND", B, "GND bottom", clearance=0.2)
    r.normalise()
    if "--autoroute" in sys.argv:
        r.autoroute(scripted_nets=["GND", LX], passes=100, tries=1, timeout=5400)
    n = r.replay()
    say("replayed %d autorouted items" % n)
    power_key(r)
    if n:
        close_opens(r)
    widen(r)
    ground_vias(r, pads_=False)                   # stitching, into whatever space is left
    ground_stubs(r)
    r.fill()
    stitch_islands(r)
    r.save()
    r.drc_report()
    r.report()


if __name__ == "__main__":
    main()

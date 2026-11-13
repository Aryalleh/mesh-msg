"""Model S power board -- schematic generator.

    . ./kicad-env.sh && python3 make_rails.py && python3 generate_schematic.py

The .kicad_sch is generated: change this script, not the sheet. Spec first
(PARTS / NETS / NO_CONNECT, built by the blocks below), then the drawing;
verify() proves with KiCad's own netlist that the drawing is the spec.

What the board is (docs/handheld.md §1-2, decisions in README.md):
  USB-C 5 V in -> AXP2101 PMU (linear charger, power path, fuel gauge, DCDC1
  + 6 LDOs for the Model S rails) -> 2x14 1.27 mm board-to-board header to the
  main board. RTC (PCF85063A) on the battery through a 25 nA LDO (TPS7A02).
  Charge state reaches the ESP32-S3 over I2C (AXP2101 status/fuel-gauge
  registers) and PMU_IRQ (GPIO2), which fires on every charger event.

AXP2101 circuit = X-Powers "AXP2101 design guide V1.0" §3.2 (linear-charge
mode) and datasheet V1.4 table 4-1; pin numbers checked against both.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

from schlib import Design, snap                               # noqa: E402
from subcircuits import Block, Refs, Spec, UsbCSink            # noqa: E402

# --------------------------------------------------------------------------
# Parts: (lib_id, value, footprint, LCSC, manufacturer, MPN). Every LCSC
# number was checked on LCSC/JLCPCB (value, package, stock) on 2026-11-13.
# --------------------------------------------------------------------------
R0402, C0402 = "Resistor_SMD:R_0402_1005Metric", "Capacitor_SMD:C_0402_1005Metric"
C0603, C0805 = "Capacitor_SMD:C_0603_1608Metric", "Capacitor_SMD:C_0805_2012Metric"
UR = "UNI-ROYAL"
CAP = {
    "100nF":     ("Device:C", "100nF", C0402, "C1525", "Samsung", "CL05B104KO5NNNC"),
    "1nF":       ("Device:C", "1nF", C0402, "C1523", "FH", "0402B102K500NT"),
    "1uF":       ("Device:C", "1uF", C0402, "C52923", "Samsung", "CL05A105KA5NQNC"),
    "2.2uF":     ("Device:C", "2.2uF", C0402, "C12530", "Samsung", "CL05A225MQ5NSNC"),
    "4.7uF":     ("Device:C", "4.7uF", C0603, "C19666", "Samsung", "CL10A475KO8NNNC"),
    "10uF":      ("Device:C", "10uF", C0603, "C19702", "Samsung", "CL10A106KP8NNNC"),
    "10uF/25V":  ("Device:C", "10uF/25V", C0805, "C15850", "Samsung", "CL21A106KAYNNNE"),
    "22uF":      ("Device:C", "22uF", C0805, "C45783", "Samsung", "CL21A226MAQNNNE"),
}
RES = {
    "510":  ("Device:R", "510", R0402, "C25123", UR, "0402WGF5100TCE"),
    "1k":   ("Device:R", "1k", R0402, "C11702", UR, "0402WGF1001TCE"),
    "2.2k": ("Device:R", "2.2k", R0402, "C25879", UR, "0402WGF2201TCE"),
    "10k":  ("Device:R", "10k", R0402, "C25744", UR, "0402WGF1002TCE"),
    "10m":  ("Device:R", "10m", "Resistor_SMD:R_1206_3216Metric", "C105362", "PSA",
             "FMF06FTHR010-LH"),
}


def put_upright(blk, s, ref, x, y):
    """Place a 2-pin part vertically with pin 1 on top (the rail side), trying
    the rotations rather than trusting a table: diodes and capacitors keep
    their pins on different local axes."""
    if not any(sy["ref"] == ref for sy in s.symbols):
        blk.put(s, ref, x, y)
    sym = s._sym(ref)[0]
    sym["x"], sym["y"] = snap(x), snap(y)
    for rot in (0, 90, 180, 270):
        sym["rot"] = rot
        s._pin_pts = s._fields = s._items = None
        a, b = s.pin(ref, 1), s.pin(ref, 2)
        if abs(a[0] - b[0]) < 0.01 and a[1] < b[1]:
            return ref
    raise ValueError(f"{ref}: no rotation puts pin 1 above pin 2")


# ==========================================================================
# Blocks
# ==========================================================================
class UsbInput(UsbCSink):
    """USB-C sink (CC 5.1k, flip pairs joined, USBLC6 on D+/D-) plus a 5 V TVS
    and the input capacitor on VBUS. The USBLC6's own VBUS pin is left open:
    on 2 layers it sits between D+ and D- and the trace to it would cut the
    ground under the pair (subcircuits.UsbCSink docstring); VBUS gets the
    SMF5.0A instead."""

    title = "USB-C INPUT"

    def __init__(self, refs):
        super().__init__(refs, vbus="VBUS", dp="USB_D_P", dm="USB_D_N", esd_vbus=False)
        self.tvs = self.part("D", ("Diode:SMF5V0A", "SMF5.0A", "Diode_SMD:D_SOD-123F",
                                   "C193402", "MDD", "SMF5.0A"))
        self.cin = self.part("C", CAP["10uF/25V"])
        self.conn("VBUS", (self.tvs, 1), (self.cin, 1))
        self.conn("GND", (self.tvs, 2), (self.cin, 2))
        self.notes += ["D+/D- go to the main board (ESP32-S3 GPIO19/20, native USB).",
                       "VBUS: SMF5.0A clamps at 9.2 V (AXP2101 VBUS abs. max 12 V)."]

    def _draw(self, s, x, y):
        super()._draw(s, x, y)
        vb = s.pin(self.j, "A4")
        bx = snap(vb[0] + 40.64)
        s.rail_bank([self.tvs, self.cin], bx, snap(y - 20.32), "VBUS",
                    place=lambda r, xx, yy: put_upright(self, s, r, xx, yy), flag=False)


class Battery(Block):
    """1-cell LiPo pouch with 10k NTC on a JST-PH 3-pin (SMD, right angle)."""

    title = "BATTERY"

    def __init__(self, refs):
        super().__init__(refs)
        self.j = self.part("J", ("Connector_Generic:Conn_01x03", "BATTERY",
                                 "Connector_JST:JST_PH_S3B-PH-SM4-TB_1x03-1MP_P2.00mm_Horizontal",
                                 "C265101", "JST", "S3B-PH-SM4-TB(LF)(SN)"))
        self.c = self.part("C", CAP["10uF"])
        self.rts = self.part("R", RES["10k"])
        self.conn("VBAT", (self.j, 1), (self.c, 1))
        self.conn("BAT_NTC", (self.j, 2), (self.rts, 1))
        self.conn("GND", (self.j, 3), (self.c, 2), (self.rts, 2))
        self.notes = ["J2: 1 = BAT+, 2 = NTC (10k B3435), 3 = BAT-. Check the pack's",
                      "wire order before plugging in -- PH packs are not standard.",
                      "R_TS DNP: fit 10k only for a pack without NTC.",
                      "2500 mAh pouch; charge current set by firmware (<= 1 A)."]

    def _draw(self, s, x, y):
        self.put(s, self.j, x, y, mirror="y")
        p1, p2, p3 = (s.pin(self.j, n) for n in ("1", "2", "3"))
        s.stub(self.j, "1", 5.08, power="rails:VBAT")
        s.stub(self.j, "2", 12.7, label="BAT_NTC")
        g = s.stub(self.j, "3", 5.08, power="GND")
        # PWR_FLAG: VBAT and the battery's GND enter the board here
        top = (p1[0] + 5.08, p1[1])
        s.wire(top, (top[0], top[1] - 5.08))
        s.power("PWR_FLAG", top[0], top[1] - 5.08)
        bx = snap(x + 30.48)
        s.rail_bank([self.c], bx, snap(y - 2.54), "rails:VBAT",
                    place=lambda r, xx, yy: self.put(s, r, xx, yy))
        self.put(s, self.rts, snap(x + 22.86), snap(y + 10.16))
        s._sym(self.rts)[0]["dnp"] = True
        s.stub(self.rts, 1, 2.54, label="BAT_NTC")
        s.stub(self.rts, 2, 2.54, power="GND")


class Pmu(Block):
    """AXP2101 with everything the design guide puts around it."""

    title = "PMU  AXP2101"

    # AXP2101 pin -> net. Unused DCDC2/3/4 (LX/FB), DCDC5 (SW, GPIO1/FB5),
    # CPUSLDO, DLDO1/2 and VBACKUP stay open (guide §4.2 items 2 and 7, §4.5).
    PINS = {
        "1": "CHGLED", "2": "VREF", "3": "GND", "6": "VSYS", "7": "VSYS",
        "12": "3V3_LCD", "13": "VSYS", "14": "3V3_GPS", "15": "3V3_SD",
        "16": "3V3_AUDIO", "17": "VSYS", "18": "VSPARE", "19": "3V3_LORA",
        "21": "+3V3", "22": "LX1", "23": "VSYS", "24": "VSYS", "28": "AXP_VRTC",
        "29": "PMU_PWROK", "30": "PMU_ONOFF", "31": "BAT_NTC", "33": "VBAT", "34": "VSYS",
        "36": "VMID", "37": "VBUS", "38": "PMU_IRQ", "39": "I2C_SDA", "40": "I2C_SCL",
        "41": "GND",
    }
    OPEN = ["4", "5", "8", "9", "10", "11", "20", "25", "26", "27", "32", "35"]

    def __init__(self, refs):
        super().__init__(refs)
        P = self.part
        self.u = P("U", ("power_s:AXP2101", "AXP2101",
                         "power_s:QFN-40_L5.0-W5.0-P0.40-TL-EP3.4", "C3036461",
                         "X-Powers", "AXP2101"))
        self.l1 = P("L", ("power_s:MWSA0402S-1R0MT", "1uH",
                          "power_s:IND-SMD_L4.4-W4.2", "C408332", "Sunlord", "MWSA0402S-1R0MT"))
        self.rmid = P("R", RES["10m"])
        for pin, net in self.PINS.items():
            self.conn(net, (self.u, pin))
        self.nc(self.u, *self.OPEN)
        self.conn("LX1", (self.l1, 1))
        self.conn("+3V3", (self.l1, 2))
        self.conn("VMID", (self.rmid, 1))
        self.conn("VSYS", (self.rmid, 2))

        # capacitor banks: rail -> [(value, note)]
        self.banks = []

        def bank(rail, values, kind="power"):
            refs_ = []
            for v in values:
                c = P("C", CAP[v])
                self.conn(rail if kind != "label" else rail, (c, 1))
                self.conn("GND", (c, 2))
                refs_.append(c)
            self.banks.append((rail, refs_, kind))

        bank("VBUS", ["10uF/25V"])
        bank("VMID", ["10uF/25V"], "label")
        bank("VSYS", ["22uF", "22uF", "2.2uF", "2.2uF", "2.2uF", "2.2uF", "2.2uF", "4.7uF"])
        bank("VBAT", ["2.2uF"])
        bank("+3V3", ["22uF", "22uF"])
        for rail in ("3V3_LORA", "3V3_AUDIO", "3V3_SD", "3V3_LCD", "3V3_GPS", "VSPARE"):
            bank(rail, ["2.2uF"])
        bank("VREF", ["1uF"], "label")
        bank("AXP_VRTC", ["2.2uF"], "label")

        # charge LED: VSYS -> 1k -> LED -> CHGLED (guide §4.3 item 2)
        self.rled = P("R", RES["1k"])
        self.led = P("D", ("Device:LED", "GREEN", "LED_SMD:LED_0603_1608Metric",
                           "C2986059", "Everlight", "19-217/GHC-YR1S2/6T"))
        self.conn("VSYS", (self.rled, 1))
        self.conn("LED_A", (self.rled, 2), (self.led, 2))
        self.conn("CHGLED", (self.led, 1))

        # power key: PWRON -- 1nF to GND, 510R to the key (guide fig. KP3)
        self.rkey = P("R", RES["510"])
        self.ckey = P("C", CAP["1nF"])
        self.sw = P("SW", ("Switch:SW_Push", "POWER", "Button_Switch_SMD:SW_Push_1P1T_XKB_TS-1187A",
                           "C318884", "XKB Connection", "TS-1187A-B-A-B"))
        self.conn("PMU_ONOFF", (self.ckey, 1), (self.rkey, 1))
        self.conn("GND", (self.ckey, 2), (self.sw, 2))
        self.conn("PWR_KEY", (self.rkey, 2), (self.sw, 1))

        # pull-ups to +3V3: IRQ 10k (guide §4.4), PWROK 10k, I2C 2.2k (datasheet)
        self.pull = []
        for net, v in (("PMU_IRQ", "10k"), ("PMU_PWROK", "10k"),
                       ("I2C_SDA", "2.2k"), ("I2C_SCL", "2.2k")):
            r = P("R", RES[v])
            self.conn("+3V3", (r, 1))
            self.conn(net, (r, 2))
            self.pull.append(r)

        self.notes = [
            "Linear-charge mode: VMID-VSYS through 10 mOhm, DCDC5 unused (SW, GPIO1 open).",
            "One 2.2 uF at each of VIN1-4 and ALDOIN, 4.7 uF at BLDOIN, 2x 22 uF at VSYS.",
            "DCDC1 3.3 V <= 2 A: 1 uH, <= 30 mOhm (MWSA0402S: 27 mOhm, Isat 7 A).",
            "LDOs 300 mA each. Rails at power-up are set by the PMU's eFuse defaults;",
            "firmware sets the voltages/enables (XPowersLib) before using a rail.",
            "Wide copper (> 150 mil where it fits) on VBUS, VMID, VBAT, VSYS, VIN, LX.",
            "VREF cap at the pin, its ground away from the DCDC1 loop.",
            "Pull-ups live here; the main board must not add its own on I2C/IRQ.",
        ]

    def _draw(self, s, x, y):
        u = self.u
        self.put(s, u, x, y)
        for pin, net in self.PINS.items():
            if net == "GND":
                s.stub(u, pin, 2.54, power="GND")
            elif net == "LX1":
                continue
            else:
                s.stub(u, pin, 5.08, label=net)
        s.nc(u, *self.OPEN)

        # DCDC1: LX1 -> L1 -> +3V3 (FB1 is labelled +3V3 above)
        lx = s.pin(u, "22")
        self.put_pin(s, self.l1, "1", (snap(lx[0] + 7.62), lx[1]))
        s.wire(lx, s.pin(self.l1, "1"))
        s.stub(self.l1, "2", 5.08, label="+3V3")
        # VMID -> 10 mOhm -> VSYS, beside the right flank
        rx, ry = snap(x + 58.42), snap(y - 10.16)
        self.put(s, self.rmid, rx, ry)
        s.stub(self.rmid, 1, 2.54, label="VMID")
        s.stub(self.rmid, 2, 2.54, label="VSYS")

        # capacitor banks in rows below the IC
        bx0, by = snap(x - 45.72), snap(y + 45.72)
        bx = bx0
        for rail, refs_, kind in self.banks:
            port = rail if rail in ("VBUS", "+3V3") else f"rails:{rail}"
            if bx + 7.62 * len(refs_) + 12.7 > x + 76.2:
                bx, by = bx0, snap(by + 25.4)
            place = lambda r, xx, yy: self.put(s, r, xx, yy)       # noqa: E731
            if kind == "label":
                bb = s.rail_bank(refs_, bx, by, rail, rail_kind="label", place=place)
            else:
                bb = s.rail_bank(refs_, bx, by, port, place=place,
                                 flag=rail not in ("VBUS", "VBAT"))
            # a flagged single-part bank sends its PWR_FLAG 7.62 mm to the right
            bx = snap(bb[2] + (20.32 if len(refs_) == 1 else 15.24))

        # pull-ups: a bank hanging from +3V3, each to its label
        py = snap(by + 30.48)
        s.rail_bank(self.pull, bx0, py, "+3V3", gnd=None,
                    place=lambda r, xx, yy: self.put(s, r, xx, yy))
        for r, net in zip(self.pull, ("PMU_IRQ", "PMU_PWROK", "I2C_SDA", "I2C_SCL")):
            s.stub(r, 2, 5.08, label=net)

        # charge LED chain, vertical: VSYS port, 1k, LED (anode up), CHGLED
        lx0 = snap(bx0 + 60.96)
        self.put(s, self.rled, lx0, py)
        s.stub(self.rled, 1, 2.54, power="rails:VSYS")
        self.put(s, self.led, lx0, snap(py + 10.16), rot=90)
        s.wire(s.pin(self.rled, 2), s.pin(self.led, 2))
        s.stub(self.led, 1, 5.08, label="CHGLED")

        # power key: PWRON label, 1nF down to GND, 510R across to PWR_KEY, key to GND
        kx = snap(bx0 + 81.28)
        self.put(s, self.ckey, kx, snap(py + 7.62))
        s.stub(self.ckey, 2, 2.54, power="GND")
        top = s.pin(self.ckey, 1)
        n = (top[0], snap(top[1] - 5.08))                  # PMU_ONOFF node
        s.wire(top, n, (n[0] - 5.08, n[1]))
        s.label("PMU_ONOFF", n[0] - 5.08, n[1], angle=180)
        # 510R horizontal, pin 1 on the left (rot 90 turns pin 1 to the left)
        self.put_pin(s, self.rkey, 1, (snap(n[0] + 5.08), n[1]), rot=90)
        s.wire(n, s.pin(self.rkey, 1))
        k = (snap(s.pin(self.rkey, 2)[0] + 5.08), n[1])    # PWR_KEY node
        s.wire(s.pin(self.rkey, 2), k)
        s.wire(k, (k[0], snap(k[1] - 5.08)))
        s.label("PWR_KEY", k[0], snap(k[1] - 5.08), angle=90)
        self.put_pin(s, self.sw, 1, (snap(k[0] + 5.08), k[1]))
        s.wire(k, s.pin(self.sw, 1))
        s.stub(self.sw, 2, 2.54, power="GND")


class Rtc(Block):
    """PCF85063A on the battery through a TPS7A02 (25 nA), so time survives
    'off' (docs/handheld.md §1, RTC row)."""

    title = "RTC"

    def __init__(self, refs):
        super().__init__(refs)
        P = self.part
        self.ldo = P("U", ("power_s:TPS7A0233PDBVR", "TPS7A0233",
                           "power_s:SOT-23-5_L2.9-W1.6-P0.95-LS2.8-BR", "C2887324", "TI",
                           "TPS7A0233PDBVR"))
        self.rtc = P("U", ("power_s:PCF85063ATT_AJ", "PCF85063ATT",
                           "power_s:TSSOP-8_L3.0-W3.0-P0.65-LS4.9-BL", "C165608", "NXP",
                           "PCF85063ATT/AJ"))
        self.y = P("Y", ("Device:Crystal", "32.768kHz", "Crystal:Crystal_SMD_3215-2Pin_3.2x1.5mm",
                         "C97606", "Seiko", "SC-32S 32.768kHz 20PPM 12.5pF"))
        self.cin = P("C", CAP["1uF"])
        self.cout = P("C", CAP["1uF"])
        self.cdec = P("C", CAP["100nF"])
        l, r = self.ldo, self.rtc
        self.conn("VBAT", (l, 1), (l, 3), (self.cin, 1))
        self.conn("GND", (l, 2), (self.cin, 2), (self.cout, 2), (self.cdec, 2), (r, 4))
        self.conn("VRTC", (l, 5), (self.cout, 1), (self.cdec, 1), (r, 8))
        self.nc(l, 4)
        # a 32 kHz tuning-fork crystal is not polarised; pin 2 faces OSCI in the drawing
        self.conn("RTC_OSCI", (r, 1), (self.y, 2))
        self.conn("RTC_OSCO", (r, 2), (self.y, 1))
        self.conn("PMU_IRQ", (r, 3))
        self.conn("I2C_SDA", (r, 5))
        self.conn("I2C_SCL", (r, 6))
        self.nc(r, 7)
        self.notes = ["INT (open drain) shares PMU_IRQ: one wake line for charger and alarm;",
                      "an alarm also wakes the PMU from sleep (REG26H[4]).",
                      "No crystal caps: set CAP_SEL = 12.5 pF (register 00h bit 0).",
                      "CLKOUT runs at 32 kHz after power-on: firmware turns it off (COF = 111).",
                      "I2C address 0x51. Keep the crystal next to pins 1-2, no traces under it."]

    def _draw(self, s, x, y):
        l, r = self.ldo, self.rtc
        self.put(s, l, x, y)
        vin = s.stub(l, "1", 5.08, power="rails:VBAT")
        s.stub(l, "3", 5.08, power="rails:VBAT")            # EN tied high
        s.stub(l, "2", 2.54, power="GND")
        s.nc(l, "4")
        s.stub(l, "5", 5.08, power="rails:VRTC")
        s.rail_bank([self.cin], snap(x - 25.4), snap(y + 15.24), "rails:VBAT",
                    place=lambda rr, xx, yy: self.put(s, rr, xx, yy))
        s.rail_bank([self.cout, self.cdec], snap(x + 15.24), snap(y + 15.24), "rails:VRTC",
                    place=lambda rr, xx, yy: self.put(s, rr, xx, yy), flag=True)
        rx = snap(x + 25.4)
        self.put(s, r, snap(x + 76.2), y)
        s.stub(r, "8", 5.08, power="rails:VRTC")
        s.stub(r, "4", 2.54, power="GND")
        s.stub(r, "3", 5.08, label="PMU_IRQ")
        s.stub(r, "5", 5.08, label="I2C_SDA")
        s.stub(r, "6", 5.08, label="I2C_SCL")
        s.nc(r, "7")
        # crystal above-left of the RTC: OSCI and OSCO each one clean L, no crossing
        o1, o2 = s.pin(r, "1"), s.pin(r, "2")
        x1 = snap(o1[0] - 5.08)                  # OSCI turns up here
        ytop = snap(o1[1] - 7.62)
        self.put_pin(s, self.y, 2, (x1, ytop))   # crystal horizontal, pin 2 on the right
        x2 = s.pin(self.y, 1)[0]
        s.wire(o1, (x1, o1[1]), (x1, ytop))
        s.wire(o2, (x2, o2[1]), (x2, ytop))


class MainLink(Block):
    """2x14 1.27 mm board-to-board header to the Model S main board."""

    title = "TO MAIN BOARD"

    MAP = {  # pin -> net; power pins paired, GND between groups
        1: "VBAT", 2: "VBAT", 3: "VBAT", 4: "VBAT", 5: "GND", 6: "GND",
        7: "VSYS", 8: "VSYS", 9: "GND", 10: "GND", 11: "+3V3", 12: "+3V3",
        13: "+3V3", 14: "GND", 15: "3V3_LORA", 16: "3V3_AUDIO", 17: "3V3_SD",
        18: "3V3_LCD", 19: "3V3_GPS", 20: "VSPARE", 21: "GND", 22: "PWR_KEY",
        23: "I2C_SDA", 24: "I2C_SCL", 25: "PMU_IRQ", 26: "PMU_PWROK",
        27: "USB_D_P", 28: "USB_D_N",
    }

    def __init__(self, refs):
        super().__init__(refs)
        self.j = self.part("J", ("Connector_Generic:Conn_02x14_Odd_Even", "MAIN",
                                 "Connector_PinHeader_1.27mm:PinHeader_2x14_P1.27mm_Vertical_SMD",
                                 "C54950782", "LCKELEC", "LCK-PH19-12728PC40D55-G1WT0"))
        for pin, net in self.MAP.items():
            self.conn(net, (self.j, pin))
        self.tps = []
        for net in ("VBUS", "VSYS", "VBAT", "+3V3", "GND"):
            tp = self.part("TP", ("Connector:TestPoint", net, "TestPoint:TestPoint_Pad_D1.0mm",
                                  "", "", ""))
            self.conn(net, (tp, 1))
            self.tps.append((tp, net))
        self.notes = ["1 A per pin: VBAT x4 (LTE board peaks 2 A), VSYS x2, +3V3 x3, GND x6.",
                      "Mates with a 2x14 1.27 mm SMD socket on the main board (e.g. C22436134).",
                      "PWR_KEY: the side power key on the main board, to GND (parallel to SW1).",
                      "PMU_PWROK: high when all rails are up; may hold the ESP32-S3 EN low.",
                      "TP: bring-up test pads, not fitted parts."]

    def _draw(self, s, x, y):
        j = self.j
        self.put(s, j, x, y)
        for pin, net in self.MAP.items():
            if net == "GND":
                s.stub(j, str(pin), 2.54, power="GND")
            else:
                s.stub(j, str(pin), 5.08 if pin % 2 else 5.08, label=net)
        tx = snap(x + 40.64)
        for i, (tp, net) in enumerate(self.tps):
            self.put(s, tp, snap(tx + i * 10.16), snap(y - 10.16))
            port = net if net in ("VBUS", "+3V3", "GND") else f"rails:{net}"
            if net == "GND":
                s.stub(tp, 1, 2.54, power="GND")
            else:
                s.stub(tp, 1, 2.54, power=port)


# ==========================================================================
def build():
    refs, spec = Refs(), Spec()
    pmu = Pmu(refs)                      # first, so the AXP2101 is U1
    usb = spec.add(UsbInput(refs))
    bat = spec.add(Battery(refs))
    spec.add(pmu)
    rtc = spec.add(Rtc(refs))
    link = spec.add(MainLink(refs))
    return spec, (usb, bat, pmu, rtc, link)


def main():
    spec, blocks = build()
    d = Design(HERE, "power-s", title="Mesh handheld Model S - power board", rev="A",
               company="mesh-msg")
    spec.lint(lib=d.lib)
    s = d.sheet("power-s.kicad_sch", "Model S power board", paper="auto",
                comment="AXP2101 PMU, USB-C charging, LiPo, RTC, link to main board")
    for b in blocks:
        b.draw(s)
    s.arrange()
    d.write(force="--force" in sys.argv)
    d.verify(dict(spec.NETS), spec.NO_CONNECT)
    d.erc()
    if "--render" in sys.argv:
        d.check_text()
        d.render(os.path.join(HERE, "out", "sch"))


if __name__ == "__main__":
    main()

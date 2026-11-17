#!/usr/bin/env python3
"""Mava handheld schematic (docs/handheld-proto.md) → handheld-schematic.pdf.

Every module is drawn with its pins; wires between pages connect by NET NAME
(a short stub ending in a label), as in a hierarchical schematic. Pages:

  1  overview: blocks, buses, versions, net list
  2  ESP32-S3-WROOM-1-N16R8: every pin, straps, reset, boot, decoupling
  3  LoRa (SX1262 module) and display (ILI9341 + XPT2046 + microSD), backlight switch
  4  audio (WM8960 board, speaker, earpiece), fuel gauge, vibration motor
  5  power: USB-C, ESD, charger, LiPo, switch, 3.3 V buck-boost
  6  keyboard module (Raspberry Pi Pico) and version A's 7 × 7 diode matrix
  7  versions B and C: BlackBerry 9900 keypad, side keys, 9900 trackpad + 1.8 V LDO

Run: python3 hardware/handheld/schematic.py   (needs schemdraw, matplotlib, pypdf)
"""
import os
import sys
import tempfile

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
import schemdraw  # noqa: E402
import schemdraw.elements as elm  # noqa: E402
from pypdf import PdfWriter  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "handheld-schematic.pdf")
A3 = (16.54, 11.69)
REV = "rev 0.1 — 2026-11-16 — not yet built"
schemdraw.config(fontsize=9, lw=1.0)

PAGES = []


def page(title, number):
    fig = plt.figure(figsize=A3)
    # frame and title block
    fig.patches.append(plt.Rectangle((0.01, 0.01), 0.98, 0.98, fill=False, lw=1.2, transform=fig.transFigure))
    fig.patches.append(plt.Rectangle((0.70, 0.01), 0.29, 0.07, fill=False, lw=1.0, transform=fig.transFigure))
    fig.text(0.705, 0.065, "Mava handheld", fontsize=13, weight="bold")
    fig.text(0.705, 0.042, f"{number}/7  {title}", fontsize=10)
    fig.text(0.705, 0.02, REV, fontsize=8, color="#555")
    fig.text(0.02, 0.965, title, fontsize=16, weight="bold")
    PAGES.append(fig)
    return fig


SCALE = 0.6  # inches per drawing unit: the same on every page


class canvas:
    """A drawing area on the page at one fixed scale (schemdraw would stretch a
    small circuit to fill its area). anchor: where the drawing's bounding box
    sits in the area — "nw" (top left) or "c" (centre)."""

    def __init__(self, fig, rect, anchor="nw"):
        self.fig, self.rect, self.anchor = fig, rect, anchor
        self.ax = fig.add_axes(rect)
        self.ax.set_axis_off()
        self.d = schemdraw.Drawing(canvas=self.ax, show=False)

    def __enter__(self):
        return self.d

    def __exit__(self, *exc):
        if exc[0]:
            return False
        self.d.draw(show=False)
        bb = self.d.get_bbox()
        w_in = self.rect[2] * A3[0]
        h_in = self.rect[3] * A3[1]
        w, h = w_in / SCALE, h_in / SCALE
        if self.anchor == "c":
            cx, cy = (bb.xmin + bb.xmax) / 2, (bb.ymin + bb.ymax) / 2
            self.ax.set_xlim(cx - w / 2, cx + w / 2)
            self.ax.set_ylim(cy - h / 2, cy + h / 2)
        else:
            self.ax.set_xlim(bb.xmin - 0.3, bb.xmin - 0.3 + w)
            self.ax.set_ylim(bb.ymax + 0.3 - h, bb.ymax + 0.3)
        return False


def ic(d, title, left=(), right=(), top=(), bottom=(), spacing=0.55, w=None, at=(0, 0), label_loc="top"):
    """An IC/module: pins are (pin name, net or None). Returns the element;
    pin anchors are named by pin name."""
    pins = []
    for side, lst in (("left", left), ("right", right), ("top", top), ("bottom", bottom)):
        # schemdraw stacks left / right pins bottom-up: reverse so the list reads top-down
        order = list(reversed(lst)) if side in ("left", "right") else list(lst)
        for name, _net in order:
            pins.append(elm.IcPin(name=name, side=side, anchorname=anchor(side, name)))
    n = max(len(left), len(right), 1)
    size = (w or 4.0, max(n * spacing + 0.6, 1.5))
    e = elm.Ic(pins=pins, size=size, pinspacing=spacing, edgepadH=0.3, lofst=0.12, fontsize=8).at(at)
    e.label(title, loc=label_loc, fontsize=11)
    d += e
    for side, lst in (("left", left), ("right", right), ("top", top), ("bottom", bottom)):
        for name, net in lst:
            if net:
                stub(d, getattr(e, anchor(side, name)), side, net)
    return e


def anchor(side, name):
    """A unique anchor per pin: '+' and '−' must not collapse to the same name."""
    table = {"+": "P", "-": "M", "−": "M", "/": "_", "(": "_", ")": "_", " ": "_", "'": "Q", ".": "_", "→": "_"}
    return "p_" + side[0] + "_" + "".join(c if c.isalnum() else table.get(c, "x") for c in name)


def stub(d, pt, side, net, length=1.1):
    """A net label: a short wire off the pin with the net name at its end."""
    direction = {"left": "left", "right": "right", "top": "up", "bottom": "down"}[side]
    line = elm.Line().at(pt)
    getattr(line, direction)(length)
    color = "#1f6feb" if net not in ("GND", "NC") else ("#444" if net == "GND" else "#999")
    loc = {"left": "left", "right": "right", "top": "top", "bottom": "bottom"}[side]
    d += line.label(net, loc=loc, color=color, fontsize=8)


def pullup(d, x, y, rname, net, rail="3V3", down=None):
    """rail ─R─ net label (a pull-up), or net ─R─ GND (down=True)."""
    if down:
        d += elm.Line().at((x, y)).up(0.6).label(net, loc="top", color="#1f6feb")
        d += elm.Resistor().at((x, y)).down().label(rname, loc="right")
        d += elm.Ground()
    else:
        d += elm.Vdd().at((x, y)).label(rail)
        d += elm.Resistor().at((x, y)).down().label(rname, loc="right")
        d += elm.Line().down(0.6).label(net, loc="bottom", color="#1f6feb")


def netlabel(d, at, net, direction="left", length=1.0):
    line = elm.Line().at(at)
    getattr(line, direction)(length)
    loc = {"left": "left", "right": "right", "up": "top", "down": "bottom"}[direction]
    d += line.label(net, loc=loc, color="#1f6feb")


def note(fig, x, y, text, size=9):
    fig.text(x, y, text, fontsize=size, va="top", family="DejaVu Sans", linespacing=1.35)


# ---------------------------------------------------------------- page 1
def page_overview():
    fig = page("Overview — blocks, buses and versions", 1)
    ax = fig.add_axes([0.02, 0.12, 0.62, 0.82])
    ax.set_xlim(0, 100)
    ax.set_ylim(0, 100)
    ax.set_axis_off()

    def box(x, y, w, h, title, sub="", color="#e8f0fe"):
        ax.add_patch(plt.Rectangle((x, y), w, h, fc=color, ec="#1f3b73", lw=1.2))
        ax.text(x + w / 2, y + h * 0.62, title, ha="center", va="center", fontsize=10, weight="bold")
        if sub:
            ax.text(x + w / 2, y + h * 0.3, sub, ha="center", va="center", fontsize=7.5)

    def wire(x1, y1, x2, y2, text="", color="#1f6feb"):
        ax.annotate("", xy=(x2, y2), xytext=(x1, y1), arrowprops=dict(arrowstyle="<->", color=color, lw=1.3))
        if text:
            ax.text((x1 + x2) / 2, (y1 + y2) / 2 + 1.2, text, ha="center", fontsize=7.5, color=color)

    box(38, 42, 24, 18, "ESP32-S3-WROOM-1", "N16R8 · Wi-Fi · BLE · USB")
    box(4, 76, 22, 12, "SX1262 LoRa", "E22-900M22S · own SPI bus")
    box(38, 80, 24, 12, "ILI9341 2.4\" + XPT2046", "+ microSD · display SPI bus")
    box(74, 76, 22, 12, "WM8960 audio", "I2S + I2C 0x1A")
    box(74, 56, 22, 10, "MAX17048", "fuel gauge I2C 0x36")
    box(74, 36, 22, 14, "Keyboard module", "Raspberry Pi Pico\nI2C target 0x1F + INT")
    box(74, 8, 22, 18, "Keyboard (one of)", "A: 7×7 diode matrix\nB: BlackBerry 9900 keypad\nC: 9900 keypad + trackpad", "#fff4e5")
    box(4, 40, 22, 14, "USB-C", "host link to PC / phone\ncharging · flashing")
    box(4, 8, 22, 22, "Power", "TP4056 + DW01 → LiPo\n→ switch → TPS63020 3.3 V\n→ Pico VSYS", "#eaf7ea")
    box(40, 8, 26, 14, "User I/O", "setup switch · vibration\nspeaker 8 Ω · earpiece 32 Ω")
    wire(26, 82, 38, 52, "LORA SPI 10–13 · DIO1 1 · BUSY 4 · RST 8")
    wire(50, 80, 50, 60, "LCD SPI 39/40/41 · CS 21/5/7 · DC 2 · RST 42 · BL 45 · T_IRQ 6")
    wire(62, 56, 74, 80, "I2S 15–18, 46")
    wire(62, 52, 74, 61, "I2C 47/48")
    wire(62, 48, 74, 43, "I2C 47/48 + INT 9")
    wire(85, 36, 85, 26, "rows GP3–9 · cols GP10–16 · TP I2C GP18/19")
    wire(26, 47, 38, 47, "USB D−/D+ 19/20")
    wire(15, 30, 15, 40, "VBUS", "#2e7d32")
    wire(53, 22, 53, 42, "SETUP 14 · VIBRA 38")
    ax.text(50, 3, "Wires connect by net name across pages (e.g. LORA_SCK, I2C_SDA, KB_R3).", ha="center", fontsize=8, color="#555")

    note(fig, 0.66, 0.93, """Versions (docs/handheld-proto.md)
  A  7×7 diode matrix + touch          Pico env kb_matrix
  B  BlackBerry 9900 keypad + touch    Pico env kb_bb9900
  C  9900 keypad + 9900 trackpad       Pico env kb_bb9900tp
  ESP32 firmware: one build (mava_handheld_s3) for all three.

Buses
  LoRa SPI   IO12 SCK · IO11 MOSI · IO13 MISO · IO10 NSS
  LCD SPI    IO39 SCK · IO40 MOSI · IO41 MISO
             CS: LCD IO21 · touch IO5 · SD IO7
  I2S        IO15 MCLK · IO16 BCLK · IO17 LRCK
             IO18 ← ADCDAT (mic) · IO46 → DACDAT
  I2C        IO47 SDA · IO48 SCL, 3.3 kΩ pull-ups
             WM8960 0x1A · MAX17048 0x36 · Pico 0x1F
  USB        IO19 D− · IO20 D+ (ESD at the connector)

Straps and resets
  IO0 BOOT button · IO45, IO46 100 kΩ to GND (low at reset)
  EN: 10 kΩ to 3V3, 1 µF to GND, reset button
  SX1262 NRESET IO8 · ILI9341 RESET IO42

Free: IO3, IO43, IO44 (UART0 debug)

Rails
  VBUS 5 V (USB) → TP4056 → VBAT (LiPo 1S)
  VBAT → power switch → VBAT_SW
  VBAT_SW → TPS63020 → 3V3 (2 A) · VBAT_SW → Pico VSYS
  1V8 (version C trackpad core) from XC6206P182

Not verified on hardware. Check module pinouts
against their datasheets / silkscreens before
soldering (WM8960 header names in particular).""", size=9.5)


# ---------------------------------------------------------------- page 2
ESP_LEFT = [("3V3", "3V3"), ("EN", "EN"), ("IO0", "BOOT"), ("IO1", "LORA_DIO1"), ("IO2", "LCD_DC"),
            ("IO3", "NC"), ("IO4", "LORA_BUSY"), ("IO5", "TOUCH_CS"), ("IO6", "TOUCH_IRQ"),
            ("IO7", "SD_CS"), ("IO8", "LORA_RST"), ("IO9", "KB_INT"), ("IO10", "LORA_NSS"),
            ("IO11", "LORA_MOSI"), ("IO12", "LORA_SCK"), ("IO13", "LORA_MISO"), ("IO14", "SETUP_SW"),
            ("IO15", "I2S_MCLK"), ("GND", "GND")]
ESP_RIGHT = [("IO16", "I2S_BCLK"), ("IO17", "I2S_LRCK"), ("IO18", "I2S_DIN"), ("IO19", "USB_DN"),
             ("IO20", "USB_DP"), ("IO21", "LCD_CS"), ("IO38", "VIBRA"), ("IO39", "LCD_SCK"),
             ("IO40", "LCD_MOSI"), ("IO41", "LCD_MISO"), ("IO42", "LCD_RST"), ("IO43 TXD0", "NC"),
             ("IO44 RXD0", "NC"), ("IO45", "LCD_BL_PWM"), ("IO46", "I2S_DOUT"), ("IO47", "I2C_SDA"),
             ("IO48", "I2C_SCL"), ("GND", "GND")]


def page_esp32():
    fig = page("ESP32-S3-WROOM-1-N16R8", 2)
    with canvas(fig, [0.06, 0.1, 0.46, 0.83]) as d:
        ic(d, "U1  ESP32-S3-WROOM-1-N16R8", ESP_LEFT, ESP_RIGHT, w=5.5, spacing=0.62)
    with canvas(fig, [0.58, 0.48, 0.4, 0.44]) as d:
        # EN: R1 to 3V3, C1 to GND, reset button to GND
        d += elm.Vdd().at((0, 0)).label("3V3")
        d += elm.Resistor().down().label("R1 10k")
        d += elm.Dot()
        en = d.here
        d += elm.Capacitor().down().label("C1 1µF")
        d += elm.Ground()
        d += elm.Line().at(en).left(2.5)
        d += elm.Button().down().label("SW1\nRESET")
        d += elm.Ground()
        netlabel(d, en, "EN", "right", 1.2)
        # BOOT: R2 to 3V3, button to GND
        d += elm.Vdd().at((6, 0)).label("3V3")
        d += elm.Resistor().down().label("R2 10k")
        d += elm.Dot()
        boot = d.here
        d += elm.Button().down().label("SW2 BOOT")
        d += elm.Ground()
        netlabel(d, boot, "BOOT (IO0)", "left", 1.0)
    with canvas(fig, [0.58, 0.12, 0.4, 0.3]) as d:
        d += elm.Vdd().at((0, 0)).label("3V3")
        d += elm.Line().right(6.5)
        for i, (name, val) in enumerate([("C2", "10µF"), ("C3", "100nF"), ("C4", "100nF")]):
            d += elm.Capacitor().at((1 + i * 2.5, 0)).down().label(f"{name} {val}", loc="right")
            d += elm.Ground()
    with canvas(fig, [0.06, 0.06, 0.46, 0.2]) as d:
        pullup(d, 0, 0, "R3 100k", "LCD_BL_PWM (IO45)", down=True)
        pullup(d, 4.5, 0, "R4 100k", "I2S_DOUT (IO46)", down=True)
        pullup(d, 9, 1.5, "R5 10k", "KB_INT (IO9)")
    note(fig, 0.58, 0.95, "Reset, boot, strapping pins, decoupling and the keyboard INT pull-up.\n"
                          "IO45 / IO46 must be low at reset (VDD_SPI / boot mode).", 9)


# ---------------------------------------------------------------- page 3
def page_radio_display():
    fig = page("LoRa radio and display", 3)
    with canvas(fig, [0.06, 0.5, 0.4, 0.43]) as d:
        ic(d, "U2  SX1262 module (Ebyte E22-900M22S)",
           [("VCC", "3V3"), ("GND", "GND"), ("NSS", "LORA_NSS"), ("SCK", "LORA_SCK"), ("MOSI", "LORA_MOSI"),
            ("MISO", "LORA_MISO")],
           [("NRST", "LORA_RST"), ("BUSY", "LORA_BUSY"), ("DIO1", "LORA_DIO1"), ("TXEN", "DIO2 (internal)"),
            ("RXEN", "NC"), ("ANT", "u.FL → SMA")], w=4.5)
    note(fig, 0.06, 0.48, "E22: DIO2 drives the RF switch (firmware RADIO_DIO2_RF_SWITCH=1), TCXO 1.8 V.\n"
                          "Never transmit without the antenna. 100 nF + 10 µF at VCC.", 9)
    with canvas(fig, [0.52, 0.42, 0.46, 0.51]) as d:
        ic(d, "U3  ILI9341 2.4\" TFT + XPT2046 (J2), microSD (J4)",
           [("VCC", "3V3"), ("GND", "GND"), ("CS", "LCD_CS"), ("RESET", "LCD_RST"), ("DC", "LCD_DC"),
            ("SDI(MOSI)", "LCD_MOSI"), ("SCK", "LCD_SCK"), ("LED", "BL_LED"), ("SDO(MISO)", "LCD_MISO")],
           [("T_CLK", "LCD_SCK"), ("T_CS", "TOUCH_CS"), ("T_DIN", "LCD_MOSI"), ("T_DO", "LCD_MISO"),
            ("T_IRQ", "TOUCH_IRQ"), ("SD_CS", "SD_CS"), ("SD_MOSI", "LCD_MOSI"), ("SD_MISO", "LCD_MISO"),
            ("SD_SCK", "LCD_SCK")], w=5.0)
    note(fig, 0.53, 0.45, "Short J1 on the module for 3.3 V supply.", 9)
    # backlight: high-side P-FET Q1; its gate BL_GATE is pulled up by R6 and
    # pulled down by Q2 when IO45 is high.
    with canvas(fig, [0.06, 0.04, 0.6, 0.34]) as d:
        ic(d, "Q1 AO3401 (P-FET)", [("G", "BL_GATE")], [("S", "3V3"), ("D", "BL_LED")], w=2.4, at=(0, 0))
        ic(d, "Q2 2N7002 (N-FET)", [("G", "LCD_BL_PWM")], [("D", "BL_GATE"), ("S", "GND")], w=2.4, at=(8.5, 0))
        netlabel(d, (0, -3.6), "3V3", "left", 0.6)
        d += elm.Resistor().at((0, -3.6)).right().label("R6 100k")
        netlabel(d, d.here, "BL_GATE", "right", 0.6)
        netlabel(d, (8.5, -3.6), "LCD_BL_PWM", "left", 0.6)
        d += elm.Resistor().at((8.5, -3.6)).right().label("R7 100k")
        d += elm.Ground()
    with canvas(fig, [0.68, 0.06, 0.3, 0.3]) as d:
        for i, (r, net) in enumerate([("R14 10k", "LCD_CS"), ("R15 10k", "TOUCH_CS"), ("R16 10k", "SD_CS")]):
            pullup(d, i * 2.8, 0, r, net)
    note(fig, 0.06, 0.44, "Backlight high-side switch (~40–60 mA): off at reset, PWM 5 kHz from IO45 dims it.", 9)
    note(fig, 0.68, 0.37, "Chip-select pull-ups (display bus):", 9)


# ---------------------------------------------------------------- page 4
def page_audio():
    fig = page("Audio, fuel gauge, vibration", 4)
    with canvas(fig, [0.06, 0.42, 0.5, 0.51]) as d:
        ic(d, "U4  Waveshare WM8960 Audio Board (SKU 15019)",
           [("3V3/VCC", "3V3"), ("GND", "GND"), ("SDA", "I2C_SDA"), ("SCL", "I2C_SCL"), ("CLK (BCLK)", "I2S_BCLK"),
            ("WS (LRCLK)", "I2S_LRCK"), ("DAC data in", "I2S_DOUT"), ("ADC data out", "I2S_DIN"),
            ("MCLK (pad)", "I2S_MCLK")],
           [("SPK LP", "LS1 8Ω +"), ("SPK LN", "LS1 8Ω −"), ("SPK RP", "LS2 32Ω +"), ("SPK RN", "LS2 32Ω −"),
            ("HP jack", "headset"), ("MIC (on board)", None)], w=5.0)
    note(fig, 0.06, 0.41, "No crystal on the module: MCLK (256 × fs) comes from IO15 — set the MCLK selection pads so the\n"
                          "header pin carries it (Waveshare manual: short MCLK to TX). Check header names on the silkscreen.\n"
                          "LS1: 8 Ω 1 W loudspeaker (ring, hands-free). LS2: 32 Ω earpiece.", 9)
    with canvas(fig, [0.62, 0.66, 0.36, 0.27]) as d:
        ic(d, "U5  MAX17048 fuel gauge",
           [("CELL", "VBAT"), ("VDD", "VBAT"), ("GND", "GND")],
           [("SDA", "I2C_SDA"), ("SCL", "I2C_SCL"), ("ALRT", "NC")], w=3.5)
    # vibration motor: Q3 pulls VIB− low; D1 (flyback) across the motor
    with canvas(fig, [0.45, 0.06, 0.53, 0.42]) as d:
        d += elm.Vdd().at((0, 0)).label("3V3")
        d += elm.Motor().at((0, 0)).down().label("M1")
        netlabel(d, d.here, "VIB−", "down", 0.5)
        d += elm.Vdd().at((3.5, 0)).label("3V3")
        d += elm.Diode().at((3.5, 0)).down().reverse().label("D1 1N4148")
        netlabel(d, d.here, "VIB−", "down", 0.5)
        ic(d, "Q3 AO3400 (N-FET)", [("G", "VIB_G")], [("D", "VIB−"), ("S", "GND")], w=2.4, at=(9, -1.5))
        netlabel(d, (2.0, -6.3), "VIBRA (IO38)", "left", 0.6)
        d += elm.Resistor().at((2.0, -6.3)).right().label("R8 100Ω")
        netlabel(d, d.here, "VIB_G", "right", 0.6)
        netlabel(d, (9.0, -6.3), "VIB_G", "left", 0.6)
        d += elm.Resistor().at((9.0, -6.3)).right().label("R9 100k")
        d += elm.Ground()
    with canvas(fig, [0.06, 0.04, 0.3, 0.28]) as d:
        pullup(d, 0, 0, "R10 3.3k", "I2C_SDA")
        pullup(d, 3.5, 0, "R11 3.3k", "I2C_SCL")
    note(fig, 0.06, 0.36, "I2C pull-ups (one pair on the bus; remove module\npull-ups if together they go below 2 kΩ):", 9)


# ---------------------------------------------------------------- page 5
def page_power():
    fig = page("Power and USB", 5)
    with canvas(fig, [0.06, 0.52, 0.4, 0.41]) as d:
        ic(d, "J1  USB-C receptacle (USB 2.0)",
           [("VBUS", "VBUS"), ("CC1", "R 5.1k → GND"), ("CC2", "R 5.1k → GND"), ("GND", "GND")],
           [("D+", "USB_DP"), ("D−", "USB_DN"), ("SHIELD", "GND")], w=4.0)
    with canvas(fig, [0.55, 0.52, 0.43, 0.41]) as d:
        ic(d, "U6  USBLC6-2SC6 ESD (at J1)",
           [("I/O1", "USB_DN"), ("I/O2", "USB_DP"), ("GND", "GND")],
           [("VBUS", "VBUS"), ("I/O1 out", "USB_DN"), ("I/O2 out", "USB_DP")], w=3.5)
    with canvas(fig, [0.06, 0.1, 0.3, 0.38]) as d:
        ic(d, "U7  TP4056 + DW01 module",
           [("IN+", "VBUS"), ("IN−", "GND")],
           [("B+", "BT1 +"), ("B−", "BT1 −"), ("OUT+", "VBAT"), ("OUT−", "GND")], w=3.5)
    with canvas(fig, [0.4, 0.1, 0.26, 0.38]) as d:
        netlabel(d, (0, 0), "VBAT", "left", 0.8)
        d += elm.Switch().at((0, 0)).right().label("SW3 power", loc="top")
        d += elm.Dot()
        netlabel(d, d.here, "VBAT_SW", "right", 0.8)
        netlabel(d, (0, -3), "BT1 +", "left", 0.8)
        d += elm.BatteryCell().at((0, -3)).right().label("BT1 LiPo 1S\n2000–3000 mAh", loc="bottom")
        netlabel(d, d.here, "BT1 −", "right", 0.8)
    with canvas(fig, [0.62, 0.1, 0.34, 0.38]) as d:
        ic(d, "U8  TPS63020 module 3.3 V 2 A",
           [("VIN", "VBAT_SW"), ("EN", "VBAT_SW"), ("GND", "GND")],
           [("VOUT", "3V3"), ("PG", "NC")], w=3.5)
    note(fig, 0.4, 0.2, "VBAT_SW also feeds the keyboard module's\nVSYS (page 6). On a DevKitC: don't let its\nUSB 5 V back-feed this path.", 9)


# ---------------------------------------------------------------- page 6
def page_keyboard_module():
    fig = page("Keyboard module (RP2040) and version A matrix", 6)
    left = [("VSYS", "VBAT_SW"), ("GND", "GND"), ("GP0 SDA0", "I2C_SDA"), ("GP1 SCL0", "I2C_SCL"),
            ("GP2", "KB_INT"), ("GP3", "KB_R0"), ("GP4", "KB_R1"), ("GP5", "KB_R2"), ("GP6", "KB_R3"),
            ("GP7", "KB_R4"), ("GP8", "KB_R5"), ("GP9", "KB_R6")]
    right = [("GP10", "KB_C0"), ("GP11", "KB_C1"), ("GP12", "KB_C2"), ("GP13", "KB_C3"), ("GP14", "KB_C4"),
             ("GP15", "KB_C5"), ("GP16", "KB_C6"), ("GP17", "KB_BL_PWM"), ("GP18 SDA1", "TP_SDA"),
             ("GP19 SCL1", "TP_SCL"), ("GP21", "TP_NRST"), ("GP22", "TP_SHTDWN")]
    with canvas(fig, [0.06, 0.35, 0.4, 0.58]) as d:
        ic(d, "U9  Raspberry Pi Pico", left, right, w=4.2, spacing=0.6)
    note(fig, 0.03, 0.33, "I2C target 0x1F on the shared bus; INT open drain (pull-up on page 2).\n"
                          "Rows: inputs with pull-ups. Columns: driven low one at a time.\n"
                          "GP17 → gate of Q4 AO3400 (100 kΩ to GND) switching the keyboard / trackpad\n"
                          "LED return (KB_BL−). Firmware: firmware/keyboard (env per version).", 9)
    # version A matrix
    ax = fig.add_axes([0.48, 0.12, 0.5, 0.8])
    ax.set_axis_off()
    keys = [["ض q", "ص w", "ث e", "ق r", "ف t", "غ y", "ع u"],
            ["ه i", "خ o", "ح p", "ج [", "چ ]", "ش a", "س s"],
            ["ی d", "ب f", "ل g", "ا h", "ت j", "ن k", "م l"],
            ["ک ;", "گ '", "ظ z", "ط x", "ز c", "ر v", "ذ b"],
            ["د n", "پ m", "و ,", "ژ .", "Space", "Bksp", "Enter"],
            ["Shift", "Alt", "Up", "Down", "Left", "Right", "OK"],
            ["Call", "End", "Menu", "Back", "Vol+", "Vol−", "Sym"]]
    with schemdraw.Drawing(canvas=ax, show=False) as d:
        schemdraw.config(fontsize=7)
        sx, sy = 2.6, 2.1
        for r in range(7):
            y = -r * sy
            d += elm.Line().at((-0.8, y)).tox(7 * sx - 0.6).color("#1f6feb").linewidth(0.8)
            d += elm.Label().at((-1.0, y)).label(f"KB_R{r}", loc="left", color="#1f6feb")
        for c in range(7):
            x = c * sx + 1.6
            d += elm.Line().at((x, 0.8)).toy(-7 * sy + 0.2).color("#c2410c").linewidth(0.8)
            d += elm.Label().at((x, 1.1)).label(f"KB_C{c}", loc="top", color="#c2410c")
        for r in range(7):
            for c in range(7):
                x, y = c * sx, -r * sy
                d += elm.Dot(radius=0.06).at((x, y))
                sw = elm.Switch().at((x, y)).down().length(0.7).linewidth(0.8)
                d += sw
                dio = elm.Diode().at(sw.end).right().length(1.6).linewidth(0.8)  # anode at the switch, cathode → column
                d += dio
                d += elm.Dot(radius=0.06).at(dio.end)
                d += elm.Label().at((x + 0.3, y - 1.25)).label(keys[r][c], fontsize=6.5, loc="bottom")
    note(fig, 0.49, 0.11, "Version A: 49 tactile switches, each in series with a 1N4148W (cathode to the column).\n"
                          "Letters on their ISIRI 9147 positions (QWERTY Latin layer).", 9)


# ---------------------------------------------------------------- page 7
def page_blackberry():
    fig = page("Versions B and C — BlackBerry 9900 keypad and trackpad", 7)
    with canvas(fig, [0.06, 0.45, 0.4, 0.48]) as d:
        ic(d, "J2  BlackBerry 9900 keypad flex",
           [("R0", "KB_R0"), ("R1", "KB_R1"), ("R2", "KB_R2"), ("R3", "KB_R3"), ("R4", "KB_R4"),
            ("R5", "KB_R5"), ("R6", "KB_R6")],
           [("C0", "KB_C0"), ("C1", "KB_C1"), ("C2", "KB_C2"), ("C3", "KB_C3"), ("C4", "KB_C4"),
            ("C5", "KB_C5"), ("LED+", "3V3 via R17"), ("LED−", "KB_BL−")], w=4.2)
    note(fig, 0.06, 0.44, "Flex line order is not published: wire to the module, press each key, read\n"
                          "'key R<r> C<c>' on the module's USB serial and match docs/handheld-proto.md §6.\n"
                          "Connector: measure the flex (count, pitch). R17: choose after measuring the LEDs.", 9)
    with canvas(fig, [0.06, 0.1, 0.4, 0.22]) as d:
        for i, (name, r, c) in enumerate([("SW4 Vol+", 6, 0), ("SW5 Vol−", 6, 1)]):
            y = -i * 2.2
            netlabel(d, (0, y), f"KB_R{r}", "left", 0.8)
            d += elm.Switch().at((0, y)).right().label(name, loc="top")
            d += elm.Diode().right().label(f"D{i + 2} 1N4148W", loc="bottom")
            netlabel(d, d.here, f"KB_C{c}", "right", 0.8)
    note(fig, 0.06, 0.34, "Side keys at the keypad's free matrix positions (6,0) and (6,1), with diodes:", 9)
    with canvas(fig, [0.52, 0.45, 0.46, 0.48]) as d:
        ic(d, "J3  9900 trackpad ADBS-A320 (Hirose FH35C-17S) — C",
           [("1 DOME A", "KB_R0"), ("2 DOME B", "D4 → KB_C0"), ("3 LED−", "R18 200Ω → KB_BL−"), ("4 LED+", "3V3"),
            ("5 DVDD", "1V8"), ("6 VDDA/IO", "3V3"), ("7 GND", "GND")],
           [("8 NRST", "TP_NRST"), ("9 SDA", "TP_SDA"), ("10 MOTION_N", "NC"), ("11 SCL", "TP_SCL"),
            ("12 GND", "GND"), ("13 SHTDWN", "TP_SHTDWN")], w=4.6)
    with canvas(fig, [0.52, 0.08, 0.46, 0.28]) as d:
        ic(d, "U10  XC6206P182 1.8 V LDO", [("VIN", "3V3"), ("GND", "GND")], [("VOUT", "1V8")], w=3.0)
        pullup(d, 7.5, 0.6, "R12 3.3k", "TP_SDA")
        pullup(d, 10.0, 0.6, "R13 3.3k", "TP_SCL")
    note(fig, 0.53, 0.44, "The dome switch is matrix position (0,0) through D4 (1N4148W). I2C 0x3B on the module's\n"
                          "own bus (GP18/19). Do not swap 1V8 and 3V3 (the sensor then answers at 0x33).", 9)


def main():
    page_overview()
    page_esp32()
    page_radio_display()
    page_audio()
    page_power()
    page_keyboard_module()
    page_blackberry()
    with tempfile.TemporaryDirectory() as tmp:
        paths = []
        for i, fig in enumerate(PAGES):
            p = os.path.join(tmp, f"{i}.pdf")
            fig.savefig(p)
            plt.close(fig)
            paths.append(p)
        w = PdfWriter()
        for p in paths:
            w.append(p)
        w.add_metadata({"/Title": "Mava handheld schematic", "/Subject": REV})
        with open(OUT, "wb") as f:
            w.write(f)
    print(OUT)


if __name__ == "__main__":
    sys.exit(main())

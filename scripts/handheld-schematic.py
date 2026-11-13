#!/usr/bin/env python3
"""Draws the schematic overviews of docs/handheld.md.

    python3 scripts/handheld-schematic.py s docs/images/handheld-s-schematic.svg
    python3 scripts/handheld-schematic.py p docs/images/handheld-p-schematic.svg

Model S: ESP32-S3 + 3.5" SPI touchscreen. Model P: ESP32-P4 + ESP32-C6 + 5" MIPI screen.
Both share the LoRa, audio, power, clock and option blocks. Nets with the same label are
connected; decoupling, ESD and footprints are not drawn.
"""
import html
import sys

P = 24
BLUE, RED, INK, BOX, BOXS, NOTE = '#1d4ed8', '#b91c1c', '#111827', '#fff8db', '#7c2d12', '#57534e'
POWER = {'VBAT', 'VBUS', 'VBUS_C', 'VRTC', 'VDD_CORE'}
out = []


# ------------------------------------------------------------------ drawing helpers
def t(x, y, s, size=11, anchor='start', weight='normal', fill=INK, italic=False):
    st = ' font-style="italic"' if italic else ''
    out.append(f'<text x="{x}" y="{y}" font-size="{size}" text-anchor="{anchor}" font-weight="{weight}" fill="{fill}"{st}>{html.escape(s)}</text>')


def note(x, y, s, anchor='start'):
    t(x, y, s, 9.5, anchor, fill=NOTE)


def line(pts, w=1.3, col=INK):
    out.append('<polyline points="%s" fill="none" stroke="%s" stroke-width="%s"/>' % (' '.join(f'{x},{y}' for x, y in pts), col, w))


def rect(x, y, w, h, fill=BOX, stroke=BOXS, sw=1.6, rx=2):
    out.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" stroke="{stroke}" stroke-width="{sw}"/>')


def circle(x, y, r, fill='#fff'):
    out.append(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}" stroke="{INK}" stroke-width="1.3"/>')


def dot(x, y):
    out.append(f'<circle cx="{x}" cy="{y}" r="2.6" fill="{INK}"/>')


def is_power(n):
    return n in POWER or n[0].isdigit()


def label(x, y, s, side, power=None):
    col = RED if (is_power(s) if power is None else power) else BLUE
    w = 7 * len(s) + 12
    x0 = x if side == 'r' else x - w
    out.append(f'<rect x="{x0}" y="{y-8}" width="{w}" height="16" rx="8" fill="#fff" stroke="{col}" stroke-width="1.1"/>')
    t(x0 + w / 2, y + 4, s, 10.5, 'middle', 'bold', col)


def gnd(x, y):
    line([(x, y), (x, y + 8)])
    for i, hw in enumerate((8, 5, 2)):
        line([(x - hw, y + 8 + 3 * i), (x + hw, y + 8 + 3 * i)])


def hres(x, y, w=30):   # horizontal resistor body from x to x+w
    rect(x, y - 5, w, 10, fill='#fff', stroke=INK, sw=1.1)


def vres(x, y, h=26):   # vertical resistor body from y to y+h
    rect(x - 5, y, 10, h, fill='#fff', stroke=INK, sw=1.1)


def hbutton(x, y, name, to_gnd=True):
    """Push button from (x, y) going left; other side to ground."""
    line([(x, y), (x - 12, y)]); circle(x - 15, y, 3); circle(x - 42, y, 3)
    line([(x - 17, y - 9), (x - 40, y - 9)]); line([(x - 28, y - 9), (x - 28, y - 15)])
    line([(x - 45, y), (x - 56, y)])
    if to_gnd:
        gnd(x - 56, y)
    t(x - 28, y - 19, name, 9, 'middle')


def vbutton(x, y):
    """Push button from (x, y) going down 30 px."""
    circle(x, y + 3, 3); circle(x, y + 27, 3); line([(x - 9, y + 5), (x - 9, y + 25)]); line([(x - 9, y + 15), (x - 15, y + 15)])


def mosfet_box(x, y, ref, part='AO3400', w=50):
    rect(x, y - 14, w, 34, fill='#fff', stroke=INK, sw=1.2)
    t(x + w / 2, y + 2, ref, 10, 'middle', 'bold'); t(x + w / 2, y + 14, part, 8.5, 'middle')
    line([(x + w / 2, y + 20), (x + w / 2, y + 28)]); gnd(x + w / 2, y + 28)


def ic(x, y, w, ref, name, left, right, top=30, stub=26, part=None, inside=False):
    n = max(len(left), len(right))
    h = top + (n - 1) * P + 18
    rect(x, y, w, h)
    hy = y + 15 if inside else y - 7
    t(x + (6 if inside else 0), hy, ref, 12.5, weight='bold')
    t(x + w - (6 if inside else 0), hy, name, 11, 'end')
    if part:
        t(x + w / 2, y + 15, part, 9.5, 'middle', fill=NOTE, italic=True)
    pins = {}
    for side, lst in (('l', left), ('r', right)):
        for i, p in enumerate(lst):
            if p is None:
                continue
            py = y + top + i * P
            if side == 'l':
                line([(x - stub, py), (x, py)]); t(x + 6, py + 4, p); pins[p] = (x - stub, py)
            else:
                line([(x + w, py), (x + w + stub, py)]); t(x + w - 6, py + 4, p, anchor='end'); pins[p] = (x + w + stub, py)
    return pins


def wire(a, b, name=None, at=0.5, w=1.3):
    line([a, b], w=w)
    if name:
        t(a[0] + (b[0] - a[0]) * at, a[1] - 4, name, 9.5, 'middle', fill=BLUE)


def lab(pins, mapping, side):
    for p, n in mapping.items():
        x, y = pins[p]
        if n == 'GND':
            d = -8 if side == 'l' else 8
            line([(x, y), (x + d, y)]); gnd(x + d, y)
        else:
            label(x, y, n, side)


def wires(src, dst, triples, at=0.5):
    """triples: (pin on src, pin on dst, net name)."""
    for a, b, n in triples:
        wire(src[a], dst[b], n, at)


def frame(W, H, title, subtitle):
    out.append(f'<rect x="0" y="0" width="{W}" height="{H}" fill="#ffffff"/>')
    rect(10, 10, W - 20, H - 20, fill='none', stroke='#9ca3af', sw=1, rx=0)
    t(30, 46, title, 22, weight='bold')
    t(30, 70, subtitle, 12, fill='#4b5563')


def title_block(bx, by, model):
    rect(bx, by, 370, 92, fill='#fff', stroke='#9ca3af', sw=1)
    t(bx + 10, by + 20, f'mesh-msg handheld · {model}', 12, weight='bold')
    t(bx + 10, by + 38, 'Rev A draft · 2026-11-13 · not for fabrication', 10.5)
    t(bx + 10, by + 56, 'Design notes, BOM and pin rules: docs/handheld.md', 10.5)
    label(bx + 10, by + 76, 'NET', 'r', False); t(bx + 60, by + 80, 'signal net', 10)
    label(bx + 130, by + 76, '3V3_x', 'r', True); t(bx + 190, by + 80, 'power rail (from U2)', 10)


# ------------------------------------------------------------------ shared blocks
def lora(x, y, mcu, pins):
    """pins: MCU pin names for SCK, MOSI, MISO, CS, RST, BUSY, DIO1 in that order."""
    u4 = ic(x, y, 190, 'U4', 'SX1262 LoRa module', ['SCK', 'MOSI', 'MISO', 'NSS', 'NRESET', 'BUSY', 'DIO1'], ['ANT', 'VCC', 'GND'],
            part='TCXO · DIO2 RF switch · +22 dBm')
    nets = ['LORA_SCK', 'LORA_MOSI', 'LORA_MISO', 'LORA_CS', 'LORA_RST', 'LORA_BUSY', 'LORA_DIO1']
    for a, m, n in zip(['SCK', 'MOSI', 'MISO', 'NSS', 'NRESET', 'BUSY', 'DIO1'], pins, nets):
        wire(mcu[m], u4[a], n)
    lab(u4, {'VCC': '3V3_LORA', 'GND': 'GND'}, 'r')
    ax, ay = u4['ANT']
    line([(ax, ay), (ax + 70, ay)]); circle(ax + 74, ay, 4)
    line([(ax + 78, ay), (ax + 100, ay), (ax + 100, ay - 30)])
    out.append(f'<polygon points="{ax+88},{ay-50} {ax+112},{ay-50} {ax+100},{ay-30}" fill="none" stroke="{INK}" stroke-width="1.3"/>')
    t(ax + 40, ay - 8, 'J3 u.FL → SMA', 9.5, 'middle'); t(ax + 118, ay - 40, 'ANT1 whip', 9.5)


def codec(x, y, mcu, pins, power_on_left):
    """pins: MCU pin names for MCLK, BCLK, LRCK, DOUT, DIN."""
    left = ['MCLK', 'SCLK', 'LRCK', 'DSDIN', 'ASDOUT']
    right = ['LIN1', 'LIN2', 'RIN2', 'MICBIAS', 'LOUT1', 'ROUT1', 'LOUT2', 'ROUT2']
    ctl = ['CCLK', 'CDATA', 'DVDD', 'AVDD', 'GND']
    if power_on_left:
        left += [None, None] + ctl
    else:
        right += [None] + ctl
    u5 = ic(x, y, 190, 'U5', 'ES8388 audio codec', left, right, part='I2C 0x10 · 16 kHz · MCLK 256 fs')
    for a, m, n in zip(['MCLK', 'SCLK', 'LRCK', 'DSDIN', 'ASDOUT'], pins, ['I2S_MCLK', 'I2S_BCLK', 'I2S_LRCK', 'I2S_DOUT', 'I2S_DIN']):
        wire(mcu[m], u5[a], n)
    lab(u5, {'CCLK': 'I2C_SCL', 'CDATA': 'I2C_SDA', 'DVDD': '3V3_AUDIO', 'AVDD': '3V3_AUDIO', 'GND': 'GND'}, 'l' if power_on_left else 'r')
    lab(u5, {'LIN1': 'MIC', 'LIN2': 'HS_MIC', 'RIN2': 'AEC_REF', 'MICBIAS': 'MICBIAS', 'LOUT1': 'HS_L', 'ROUT1': 'HS_R',
             'LOUT2': 'EAR', 'ROUT2': 'SPK_IN'}, 'r')


def audio_parts(AX, aec_y=1052):
    # MK1 mic
    y = 500
    label(AX, y, 'MICBIAS', 'l'); line([(AX, y), (AX + 20, y)]); hres(AX + 20, y); t(AX + 35, y - 9, 'R 2.2k', 9, 'middle')
    line([(AX + 50, y), (AX + 80, y), (AX + 80, y + 22)]); dot(AX + 80, y + 22)
    label(AX, y + 22, 'MIC', 'l'); line([(AX, y + 22), (AX + 110, y + 22)])
    circle(AX + 126, y + 22, 16); line([(AX + 112, y + 8), (AX + 112, y + 36)], w=2)
    line([(AX + 142, y + 22), (AX + 160, y + 22)]); gnd(AX + 160, y + 22)
    t(AX + 126, y + 54, 'MK1 analog MEMS mic (1µF series C)', 9.5, 'middle')
    # J4 headset jack
    j4 = ic(AX + 40, 600, 140, 'J4', '3.5 mm TRRS (CTIA)', ['TIP', 'RING1', 'RING2', 'SLEEVE', 'DET'], [], stub=22)
    lab(j4, {'TIP': 'HS_L', 'RING1': 'HS_R', 'SLEEVE': 'HS_MIC', 'DET': 'HP_DET'}, 'l')
    x, y = j4['RING2']; line([(x, y), (x - 8, y)]); gnd(x - 8, y)
    note(AX + 110, 760, 'HS_MIC: bias 2.2k from MICBIAS', 'middle')
    # LS1 earpiece
    y = 780
    label(AX, y, 'EAR', 'l'); line([(AX, y), (AX + 30, y)])
    line([(AX + 30, y - 8), (AX + 30, y + 8)], w=2); line([(AX + 36, y - 8), (AX + 36, y + 8)], w=2); line([(AX + 36, y), (AX + 80, y)])
    rect(AX + 80, y - 8, 10, 16, fill='#fff', stroke=INK, sw=1.2)
    out.append(f'<polygon points="{AX+90},{y-8} {AX+104},{y-18} {AX+104},{y+18} {AX+90},{y+8}" fill="#fff" stroke="{INK}" stroke-width="1.2"/>')
    line([(AX + 85, y + 8), (AX + 85, y + 24)]); gnd(AX + 85, y + 24)
    t(AX + 112, y + 4, 'LS1 earpiece 32 Ω', 9.5); t(AX + 33, y - 12, '10µF', 9, 'middle')
    # U6 speaker amp + LS2
    u6 = ic(AX + 40, 860, 110, 'U6', 'NS4150B', ['IN+', 'IN−', 'CTRL', 'VDD', 'GND'], ['OUT+', 'OUT−'], stub=22)
    lab(u6, {'IN+': 'SPK_IN', 'CTRL': 'PA_EN', 'VDD': 'VBAT', 'GND': 'GND'}, 'l')
    x, y = u6['IN−']; line([(x, y), (x - 8, y)]); gnd(x - 8, y)
    xo, yo = u6['OUT+']; xm, ym = u6['OUT−']
    line([(xo, yo), (xo + 20, yo), (xo + 20, yo + 4)]); line([(xm, ym), (xo + 20, ym), (xo + 20, ym - 4)])
    rect(xo + 14, yo + 4, 12, 16, fill='#fff', stroke=INK, sw=1.2)
    out.append(f'<polygon points="{xo+26},{yo+4} {xo+42},{yo-8} {xo+42},{yo+32} {xo+26},{yo+20}" fill="#fff" stroke="{INK}" stroke-width="1.2"/>')
    t(xo - 4, yo - 24, 'LS2 speaker 8 Ω 1 W', 9.5)
    note(AX + 95, 1022, 'SPK_IN: 0.1µF series C at IN+', 'middle')
    # AEC reference divider
    y = aec_y
    label(AX - 140, y, 'SPK_IN', 'l'); line([(AX - 140, y), (AX - 110, y)]); hres(AX - 110, y)
    line([(AX - 80, y), (AX - 40, y)]); dot(AX - 60, y); line([(AX - 60, y), (AX - 60, y + 6)]); vres(AX - 60, y + 6, 22)
    line([(AX - 60, y + 28), (AX - 60, y + 34)]); gnd(AX - 60, y + 34)
    line([(AX - 40, y - 8), (AX - 40, y + 8)], w=2); line([(AX - 34, y - 8), (AX - 34, y + 8)], w=2)
    line([(AX - 34, y), (AX - 10, y)]); label(AX - 10, y, 'AEC_REF', 'r')
    t(AX - 95, y - 10, '10k', 9, 'middle'); t(AX - 50, y + 22, '2.2k', 9); t(AX - 37, y - 12, '1µF', 9, 'middle')
    note(AX - 200, y + 62, 'AEC reference: speaker signal back into the right ADC')


def pmu(x, y, rails):
    """rails: list of (AXP pin, net)."""
    right = [p for p, _ in rails] + ['IRQ', 'SDA', 'SCL']
    u2 = ic(x, y, 190, 'U2', 'AXP2101 PMU', ['VBUS', 'TS', None, 'PWRON', 'VBAT', None, 'GND'], right, part='charger 1 A · fuel gauge')
    lab(u2, dict(rails + [('IRQ', 'PMU_IRQ'), ('SDA', 'I2C_SDA'), ('SCL', 'I2C_SCL')]), 'r')
    lab(u2, {'VBUS': 'VBUS', 'GND': 'GND'}, 'l')
    bx, by = u2['VBAT']
    line([(bx, by), (bx - 70, by)]); dot(bx - 40, by)
    line([(bx - 40, by), (bx - 40, by + 10)]); line([(bx - 54, by + 10), (bx - 26, by + 10)], w=2.2)
    line([(bx - 48, by + 16), (bx - 32, by + 16)], w=2.2); line([(bx - 40, by + 16), (bx - 40, by + 24)]); gnd(bx - 40, by + 24)
    note(bx - 60, by + 26, 'BT1 Li-ion (NTC inside)', 'end'); label(bx - 70, by, 'VBAT', 'l')
    x1, y1 = u2['TS']; line([(x1, y1), (x1 - 12, y1)]); note(x1 - 16, y1 + 4, 'NTC 10k in the battery', 'end')
    hbutton(*u2['PWRON'], 'SW52 POWER')


def rtc(x, y):
    u7 = ic(x, y, 160, 'U7', 'PCF85063A RTC', ['OSCI', 'OSCO', 'VDD', 'VSS'], ['SDA', 'SCL', 'INT'])
    lab(u7, {'VDD': 'VRTC', 'VSS': 'GND'}, 'l')
    lab(u7, {'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL'}, 'r')
    x1, y1 = u7['OSCI']; x2, y2 = u7['OSCO']
    line([(x1, y1), (x1 - 20, y1), (x1 - 20, y1 + 6)]); line([(x2, y2), (x2 - 20, y2), (x2 - 20, y2 - 6)])
    rect(x1 - 26, y1 + 8, 12, 8, fill='#fff', stroke=INK, sw=1.1); t(x1 - 32, y1 + 16, 'Y1 32.768 kHz', 9.5, 'end')
    x3, y3 = u7['INT']; note(x3 + 6, y3 + 4, 'n.c.')
    u8 = ic(x, y + 160, 160, 'U8', 'TPS7A02 3.0 V, 25 nA', ['IN', 'GND'], ['OUT'])
    lab(u8, {'IN': 'VBAT', 'GND': 'GND'}, 'l'); lab(u8, {'OUT': 'VRTC'}, 'r')


def option(x, y, tx, rx, reset):
    j6 = ic(x, y, 180, 'J6', 'GPS / LTE option', ['RXD', 'TXD', 'RESET', 'VCC', 'VBAT', 'GND'], [])
    lab(j6, {'RXD': tx, 'TXD': rx, 'VCC': '3V3_GPS', 'VBAT': 'VBAT', 'GND': 'GND'}, 'l')
    if reset:
        lab(j6, {'RESET': reset}, 'l')
    else:
        x1, y1 = j6['RESET']; note(x1 - 6, y1 + 4, 'n.c. (rail reset)', 'end')
    note(x + 90, y + 186, 'L76K GPS, or LTE board (SIM7600G-H): fit one', 'middle')
    note(x + 90, y + 200, 'VCC also switches the LTE board on', 'middle')


def vibration(src, x, mx=None):
    """Q2 + motor M1. src: MCU pin end (wired) or None (fed by the VIB label at x - 40)."""
    if src:
        sx, y = src
        line([(sx, y), (x, y)]); t(x - 30, y - 4, 'VIB', 9.5, 'middle', fill=BLUE)
    else:
        sx, y = x - 40, mx
        mx = None
        label(sx, y, 'VIB', 'l'); line([(sx, y), (x, y)])
    mosfet_box(x, y, 'Q2', w=60)
    m = mx if mx else x + 124
    line([(x + 60, y), (m - 14, y)]); circle(m, y, 14); t(m, y + 4, 'M', 11, 'middle', 'bold')
    line([(m + 14, y), (m + 36, y)]); label(m + 36, y, 'VBAT', 'r')
    note(m, y + 30, 'M1 coin motor + flyback diode', 'middle')


def usb(x, y, mcu, dm, dp, vbus_net):
    j2 = ic(x, y, 160, 'J2', 'USB-C receptacle', ['VBUS', 'CC1', 'CC2', 'GND'], ['D−', 'D+'])
    lab(j2, {'VBUS': vbus_net, 'GND': 'GND'}, 'l')
    xu = x + 240
    for a, b, n in (('D−', dm, 'USB_D−'), ('D+', dp, 'USB_D+')):
        line([j2[a], (xu, j2[a][1])]); line([(xu + 70, j2[a][1]), mcu[b]]); t(xu + 115, j2[a][1] - 4, n, 9.5, 'middle', fill=BLUE)
    rect(xu, j2['D−'][1] - 16, 70, 56, fill='#fff', stroke=INK, sw=1.2)
    t(xu + 35, j2['D−'][1] + 10, 'U9', 10, 'middle', 'bold'); t(xu + 35, j2['D−'][1] + 22, 'USBLC6-2', 8.5, 'middle')
    return j2


# ------------------------------------------------------------------ Model S
def draw_s():
    W, H = 2140, 1160
    frame(W, H, 'mesh-msg handheld, Model S — schematic overview (rev A draft)',
          'ESP32-S3 + 3.5" SPI touchscreen. Decoupling capacitors, ESD parts and footprints are listed in docs/handheld.md, not drawn. Nets with the same label are connected.')
    EX = 960
    L = ['GPIO12', 'GPIO11', 'GPIO10', 'GPIO9', 'GPIO46', None, 'GPIO1', 'GPIO2', 'GPIO3', 'GPIO18', 'GPIO8', None,
         'GPIO19', 'GPIO20', None, 'GPIO0', 'GPIO43', 'GPIO44', None, 'EN', '3V3', 'GND']
    R = ['GPIO40', 'GPIO41', 'GPIO39', 'GPIO38', 'GPIO42', 'GPIO47', 'GPIO48', None, 'GPIO7', 'GPIO21', 'GPIO4', None,
         'GPIO45', None, None, 'GPIO14', 'GPIO13', 'GPIO15', 'GPIO16', 'GPIO17', 'GPIO5', 'GPIO6']
    u1 = ic(EX, 140, 240, 'U1', 'ESP32-S3-WROOM-1-N16R8', L, R, part='16 MB flash · 8 MB PSRAM · Wi-Fi + BLE 5')
    lab(u1, {'GPIO1': 'BTN_ADC', 'GPIO2': 'PMU_IRQ', 'GPIO3': 'TP_INT', 'GPIO18': 'I2C_SDA', 'GPIO8': 'I2C_SCL',
             'GPIO43': 'TXD0', 'GPIO44': 'RXD0', '3V3': '3V3', 'GND': 'GND'}, 'l')
    lab(u1, {'GPIO5': 'PA_EN', 'GPIO6': 'HP_DET'}, 'r')
    x, y = u1['EN']; label(x, y, 'EN', 'l'); note(x - 40, y + 20, 'R 10k to 3V3, C 1µF to GND', 'end')
    note(EX - 30, u1['GPIO8'][1] + 20, 'pull-ups: 10k on INT lines, 2.2k on I2C', 'end')
    hbutton(*u1['GPIO0'], 'SW51 BOOT')

    # J1 screen
    j1 = ic(560, 140, 180, 'J1', '3.5" ST7796S + touch', ['VCC', 'LEDA', 'RESET', 'TP_SDA', 'TP_SCL', 'TP_INT', 'TP_RST', 'GND'],
            ['SCLK', 'MOSI', 'CS', 'DC', 'LEDK'], part='320×480, SPI 80 MHz')
    lab(j1, {'VCC': '3V3_LCD', 'LEDA': '3V3_LCD', 'RESET': 'LCD_RST', 'TP_SDA': 'I2C_SDA', 'TP_SCL': 'I2C_SCL',
             'TP_INT': 'TP_INT', 'TP_RST': 'LCD_RST', 'GND': 'GND'}, 'l')
    wires(j1, u1, [('SCLK', 'GPIO12', 'LCD_SCLK'), ('MOSI', 'GPIO11', 'LCD_MOSI'), ('CS', 'GPIO10', 'LCD_CS'), ('DC', 'GPIO9', 'LCD_DC')])
    ya = j1['LEDK'][1]; xq = 820
    line([j1['LEDK'], (xq, ya)]); line([(xq + 50, ya), u1['GPIO46']]); mosfet_box(xq, ya, 'Q1')
    t(xq - 4, ya - 4, 'D', 9, 'end'); t(xq + 54, ya - 4, 'G', 9); t(xq + 100, ya - 4, 'BL_PWM', 9.5, 'middle', fill=BLUE)
    note(560, 372, 'LCD_RST: RC 10k / 1µF, power-on reset')

    # side buttons: resistor ladder on BTN_ADC
    t(40, 140, 'Side buttons on one ADC pin', 11, weight='bold')
    ny = 236
    label(330, ny, 'BTN_ADC', 'r'); line([(70, ny), (330, ny)])
    label(70, 156, '3V3', 'l'); line([(70, 156), (90, 156), (90, 176)]); vres(90, 176, 30); line([(90, 206), (90, ny)]); dot(90, ny)
    note(100, 196, '10k')
    line([(130, ny), (130, ny + 10)]); dot(130, ny)
    line([(122, ny + 10), (138, ny + 10)], w=2); line([(122, ny + 16), (138, ny + 16)], w=2); line([(130, ny + 16), (130, ny + 26)]); gnd(130, ny + 26)
    note(130, ny + 56, '100n', 'middle')
    for i, (name, r) in enumerate((('Vol+', '2.2k'), ('Vol−', '6.8k'), ('PTT', '22k'))):
        bx = 190 + i * 55
        dot(bx, ny); line([(bx, ny), (bx, ny + 10)]); vres(bx, ny + 10, 26); line([(bx, ny + 36), (bx, ny + 46)])
        vbutton(bx, ny + 46); line([(bx, ny + 76), (bx, ny + 84)]); gnd(bx, ny + 84)
        note(bx + 8, ny + 28, r); t(bx, ny + 122, name, 10, 'middle', 'bold')
    note(40, ny + 145, 'SW53–SW55; ≈0.6 V, 1.3 V, 2.3 V; none pressed = 3.3 V')

    j2 = usb(560, u1['GPIO19'][1] - 30, u1, 'GPIO19', 'GPIO20', 'VBUS')
    for p in ('CC1', 'CC2'):
        x, y = j2[p]; line([(x, y), (x - 14, y)]); hres(x - 44, y); line([(x - 44, y), (x - 54, y)]); gnd(x - 54, y)
    note(560 + 80, j2['GND'][1] + 36, 'CC1/CC2: 5.1k to GND (sink)', 'middle')
    pmu(270, 740, [('DCDC1', '3V3'), ('ALDO2', '3V3_LORA'), ('ALDO3', '3V3_AUDIO'), ('ALDO4', '3V3_SD'), ('BLDO1', '3V3_LCD'), ('BLDO2', '3V3_GPS')])
    rtc(700, 830)
    option(1010, 880, 'TXD0', 'RXD0', None)
    lora(1400, 140, u1, ['GPIO40', 'GPIO41', 'GPIO39', 'GPIO38', 'GPIO42', 'GPIO47', 'GPIO48'])
    j5 = ic(1400, u1['GPIO7'][1] - 28, 190, 'J5', 'microSD socket', ['CLK', 'CMD', 'DAT0'], ['VDD', 'DAT1–3', 'CD', 'GND'], top=28, inside=True)
    wires(u1, j5, [('GPIO7', 'CLK', 'SD_CLK'), ('GPIO21', 'CMD', 'SD_CMD'), ('GPIO4', 'DAT0', 'SD_D0')])
    lab(j5, {'VDD': '3V3_SD', 'GND': 'GND'}, 'r'); x, y = j5['DAT1–3']; note(x + 6, y + 4, '10k to 3V3_SD'); x, y = j5['CD']; note(x + 6, y + 4, 'n.c.')
    vibration(u1['GPIO45'], 1300, mx=1704)
    codec(1400, u1['GPIO14'][1] - 30, u1, ['GPIO14', 'GPIO13', 'GPIO15', 'GPIO16', 'GPIO17'], power_on_left=True)
    audio_parts(1820)
    title_block(1230, 1010, 'Model S (ESP32-S3)')
    return W, H


# ------------------------------------------------------------------ Model P
def draw_p():
    W, H = 2140, 1960
    frame(W, H, 'mesh-msg handheld, Model P — schematic overview (rev A draft)',
          'ESP32-P4 + ESP32-C6 + 5" MIPI touchscreen. GPIO numbers are a suggestion: verify against the ESP32-P4 datasheet. ± = differential pair drawn as one line.')
    EX = 900
    L = ['DSI_CLK±', 'DSI_D0±', 'DSI_D1±', 'GPIO28', 'GPIO29', 'GPIO30', None, None, None,
         'CSI_CLK±', 'CSI_D0±', 'CSI_D1±', 'GPIO31', None, None, None, None,
         'USB_DP', 'USB_DM', None, None, None, None,
         'FLASH_CS', 'FLASH_CLK', 'FLASH_D0–D3', None,
         'GPIO7', 'GPIO8', 'GPIO4', 'GPIO5', None, 'GPIO35', None, 'XTAL_P/N', 'CHIP_PU', 'VDD (3V3)', 'GND']
    R = ['GPIO18', 'GPIO19', 'GPIO14', 'GPIO15', 'GPIO16', 'GPIO17', 'GPIO54', None, None,
         'GPIO20', 'GPIO21', 'GPIO22', 'GPIO23', 'GPIO32', 'GPIO33', 'GPIO3', None, None,
         'GPIO43', 'GPIO44', 'GPIO39', 'GPIO40', 'GPIO41', 'GPIO42', None, None,
         'GPIO13', 'GPIO12', 'GPIO10', 'GPIO9', 'GPIO11', 'GPIO53', 'GPIO52', None,
         'GPIO45', 'GPIO46', 'GPIO47', 'GPIO48', 'GPIO49', 'GPIO50', 'GPIO51', 'GPIO2', 'GPIO6']
    u1 = ic(EX, 140, 260, 'U1', 'ESP32-P4NRW32', L, R, part='RISC-V 400 MHz · 32 MB PSRAM')
    lab(u1, {'GPIO7': 'I2C_SDA', 'GPIO8': 'I2C_SCL', 'GPIO4': 'TP_INT', 'GPIO5': 'PMU_IRQ', 'VDD (3V3)': '3V3', 'GND': 'GND'}, 'l')
    lab(u1, {'GPIO53': 'PA_EN', 'GPIO52': 'HP_DET', 'GPIO45': 'MDM_TXD', 'GPIO46': 'MDM_RXD', 'GPIO47': 'MDM_RTS', 'GPIO48': 'MDM_CTS',
             'GPIO49': 'PCM_CLK', 'GPIO50': 'PCM_SYNC', 'GPIO51': 'PCM_OUT', 'GPIO2': 'PCM_IN', 'GPIO6': 'EXP_INT'}, 'r')
    x, y = u1['CHIP_PU']; label(x, y, 'EN', 'l'); note(x - 40, y + 20, 'R 10k to 3V3, C 1µF to GND', 'end')
    x, y = u1['XTAL_P/N']; line([(x, y), (x - 20, y)]); rect(x - 46, y - 6, 26, 12, fill='#fff', stroke=INK, sw=1.1); note(x - 52, y + 4, 'Y3 40 MHz', 'end')
    hbutton(*u1['GPIO35'], 'SW51 BOOT')
    note(EX + 110, u1['GND'][1] + 50, 'pull-ups: 10k INT, 2.2k I2C', 'middle')
    note(EX + 110, u1['GND'][1] + 64, 'VDD_CORE: U2 DCDC2 if needed', 'middle')

    # J1 5" MIPI screen + touch
    j1 = ic(520, 140, 200, 'J1', '5" IPS 720×1280 MIPI + GT911', ['VCI', 'IOVCC', 'TP_SDA', 'TP_SCL', 'TP_INT', 'TP_VDD', 'GND'],
            ['DSI_CLK±', 'DSI_D0±', 'DSI_D1±', 'RESET', 'TP_RST', 'LED+ / LED−'], part='2-lane DSI · ILI9881C / JD9365')
    lab(j1, {'VCI': '3V3_LCD', 'IOVCC': '1V8', 'TP_SDA': 'I2C_SDA', 'TP_SCL': 'I2C_SCL', 'TP_INT': 'TP_INT', 'TP_VDD': '3V3_LCD', 'GND': 'GND'}, 'l')
    for a, b, n in (('DSI_CLK±', 'DSI_CLK±', 'DSI_CLK'), ('DSI_D0±', 'DSI_D0±', 'DSI_D0'), ('DSI_D1±', 'DSI_D1±', 'DSI_D1'),
                    ('RESET', 'GPIO28', 'LCD_RST'), ('TP_RST', 'GPIO29', 'TP_RST')):
        wire(j1[a], u1[b], n, w=2.4 if '±' in a else 1.3)
    ya = j1['LED+ / LED−'][1]; xb = 770
    line([j1['LED+ / LED−'], (xb, ya)], w=2.4); line([(xb + 70, ya), u1['GPIO30']])
    rect(xb, ya - 16, 70, 40, fill='#fff', stroke=INK, sw=1.2)
    t(xb + 35, ya, 'U13', 10, 'middle', 'bold'); t(xb + 35, ya + 13, 'SGM3732', 8.5, 'middle')
    t(xb + 100, ya - 4, 'BL_PWM', 9.5, 'middle', fill=BLUE)
    line([(xb + 35, ya + 24), (xb + 35, ya + 40)]); label(xb + 35, ya + 48, 'VBAT', 'l'); note(xb + 42, ya + 36, 'LED boost')

    # J7 camera
    j7 = ic(520, u1['CSI_CLK±'][1] - 30, 200, 'J7', 'camera FPC (OV5647, option)', ['SCCB_SDA', 'SCCB_SCL', 'XCLK', 'AVDD', 'DOVDD', 'GND'],
            ['CSI_CLK±', 'CSI_D0±', 'CSI_D1±', 'PWDN'], part='5 MP · 2-lane CSI')
    lab(j7, {'SCCB_SDA': 'I2C_SDA', 'SCCB_SCL': 'I2C_SCL', 'AVDD': '2V8_CAM', 'DOVDD': '1V8', 'GND': 'GND'}, 'l')
    x, y = j7['XCLK']; line([(x, y), (x - 40, y)]); rect(x - 110, y - 12, 70, 24, fill='#fff', stroke=INK, sw=1.2)
    t(x - 75, y + 4, 'Y2 24 MHz', 9, 'middle', 'bold')
    for a, b, n in (('CSI_CLK±', 'CSI_CLK±', 'CSI_CLK'), ('CSI_D0±', 'CSI_D0±', 'CSI_D0'), ('CSI_D1±', 'CSI_D1±', 'CSI_D1'), ('PWDN', 'GPIO31', 'CAM_PWDN')):
        wire(j7[a], u1[b], n, w=2.4 if '±' in a else 1.3)

    # USB-C, dual role
    j2 = ic(560, u1['USB_DP'][1] - 30, 160, 'J2', 'USB-C receptacle', ['VBUS', 'CC1', 'CC2', 'GND'], ['D+', 'D−'], part='USB 2.0 high speed')
    lab(j2, {'VBUS': 'VBUS_C', 'CC1': 'CC1', 'CC2': 'CC2', 'GND': 'GND'}, 'l')
    xu = 760
    for a, b, n in (('D+', 'USB_DP', 'USB_D+'), ('D−', 'USB_DM', 'USB_D−')):
        line([j2[a], (xu, j2[a][1])], w=2.4 if False else 1.3); line([(xu + 70, j2[a][1]), u1[b]]); t(xu + 105, j2[a][1] - 4, n, 9.5, 'middle', fill=BLUE)
    rect(xu, j2['D+'][1] - 16, 70, 56, fill='#fff', stroke=INK, sw=1.2)
    t(xu + 35, j2['D+'][1] + 10, 'U9', 10, 'middle', 'bold'); t(xu + 35, j2['D+'][1] + 22, 'USBLC6-2', 8.5, 'middle')
    u14 = ic(130, 560, 160, 'U14', 'TUSB320 (DRP)', ['VDD', 'GND'], ['CC1', 'CC2', 'SDA', 'SCL'], part='USB-C role detect')
    lab(u14, {'VDD': '3V3', 'GND': 'GND'}, 'l')
    lab(u14, {'CC1': 'CC1', 'CC2': 'CC2', 'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL'}, 'r')
    u10 = ic(130, 730, 160, 'U10', 'OTG power', ['VBAT', 'EN', 'GND'], ['VBUS_C', 'VBUS'], part='TPS61023 + 2× TPS22918')
    lab(u10, {'VBAT': 'VBAT', 'EN': 'OTG_EN', 'GND': 'GND'}, 'l')
    lab(u10, {'VBUS_C': 'VBUS_C', 'VBUS': 'VBUS'}, 'r')
    note(40, 860, 'host: 5 V boost → VBUS_C, connector cut from the PMU input;')
    note(40, 874, 'device: VBUS_C → VBUS (charging)')

    # U11 flash
    u11 = ic(560, u1['FLASH_CS'][1] - 30, 160, 'U11', 'W25Q128JV 16 MB', ['VCC', 'GND'], ['/CS', 'CLK', 'IO0–IO3'], part='QSPI flash')
    lab(u11, {'VCC': '3V3', 'GND': 'GND'}, 'l')
    wire(u11['/CS'], u1['FLASH_CS'], 'FLASH_CS'); wire(u11['CLK'], u1['FLASH_CLK'], 'FLASH_CLK'); wire(u11['IO0–IO3'], u1['FLASH_D0–D3'], 'FLASH_D[0..3]', w=3.2)

    # C6 Wi-Fi + BLE
    u12 = ic(1400, 140, 200, 'U12', 'ESP32-C6-MINI-1', ['IO19 SDIO_CLK', 'IO18 SDIO_CMD', 'IO20 SDIO_D0', 'IO21 SDIO_D1', 'IO22 SDIO_D2', 'IO23 SDIO_D3', 'EN'],
             ['3V3', 'GND', 'IO9 (BOOT)', 'ANT'], part='Wi-Fi 6 · BLE 5 · ESP-Hosted')
    wires(u1, u12, [('GPIO18', 'IO19 SDIO_CLK', 'SDIO_CLK'), ('GPIO19', 'IO18 SDIO_CMD', 'SDIO_CMD'), ('GPIO14', 'IO20 SDIO_D0', 'SDIO_D0'),
                    ('GPIO15', 'IO21 SDIO_D1', 'SDIO_D1'), ('GPIO16', 'IO22 SDIO_D2', 'SDIO_D2'), ('GPIO17', 'IO23 SDIO_D3', 'SDIO_D3'),
                    ('GPIO54', 'EN', 'C6_EN')])
    lab(u12, {'3V3': '3V3', 'GND': 'GND'}, 'r')
    x, y = u12['IO9 (BOOT)']; note(x + 6, y + 4, 'test pad, 10k pull-up'); x, y = u12['ANT']; note(x + 6, y + 4, 'PCB antenna, keep-out')

    lora(1400, u1['GPIO20'][1] - 30, u1, ['GPIO20', 'GPIO21', 'GPIO22', 'GPIO23', 'GPIO32', 'GPIO33', 'GPIO3'])
    j5 = ic(1400, u1['GPIO43'][1] - 30, 190, 'J5', 'microSD socket', ['CLK', 'CMD', 'DAT0', 'DAT1', 'DAT2', 'DAT3'], ['VDD', 'CD', 'GND'], part='SDMMC slot 0, 4-bit')
    wires(u1, j5, [('GPIO43', 'CLK', 'SD_CLK'), ('GPIO44', 'CMD', 'SD_CMD'), ('GPIO39', 'DAT0', 'SD_D0'), ('GPIO40', 'DAT1', 'SD_D1'),
                   ('GPIO41', 'DAT2', 'SD_D2'), ('GPIO42', 'DAT3', 'SD_D3')])
    lab(j5, {'VDD': '3V3_SD', 'GND': 'GND'}, 'r'); x, y = j5['CD']; note(x + 6, y + 4, 'n.c.')
    codec(1400, u1['GPIO13'][1] - 30, u1, ['GPIO13', 'GPIO12', 'GPIO10', 'GPIO9', 'GPIO11'], power_on_left=False)
    audio_parts(1820, aec_y=1160)

    # bottom row: PMU, RTC, option, buttons, vibration
    pmu(270, 920, [('DCDC1', '3V3'), ('DCDC2', 'VDD_CORE'), ('ALDO1', '2V8_CAM'), ('ALDO2', '3V3_LORA'), ('ALDO3', '3V3_AUDIO'),
                   ('ALDO4', '3V3_SD'), ('BLDO1', '3V3_LCD'), ('BLDO2', '3V3_GPS'), ('DLDO1', '1V8')])
    rtc(700, 1110)
    bx0, by0 = 1340, 1200
    t(bx0 - 40, by0 - 30, 'Side buttons', 11, weight='bold')
    for i, n in enumerate(('VOL_UP', 'VOL_DN', 'PTT')):
        y = by0 + i * 40
        label(bx0 + 60, y, n, 'l'); line([(bx0 + 60, y), (bx0 + 110, y)]); hbutton(bx0 + 150, y, ['SW53 Vol+', 'SW54 Vol−', 'SW55 PTT'][i])
        line([(bx0 + 110, y), (bx0 + 138, y)])
    note(bx0 - 40, by0 + 110, 'internal or 10k pull-ups')
    vibration(None, 1560, mx=1252)
    modem_row(1450)
    title_block(1730, 1845, 'Model P (P4 + C6 + LTE)')
    return W, H


def antenna(x, y, text):
    line([(x, y), (x, y - 30)])
    out.append(f'<polygon points="{x-10},{y-50} {x+10},{y-50} {x},{y-30}" fill="none" stroke="{INK}" stroke-width="1.3"/>')
    t(x + 14, y - 40, text, 9.5)


def modem_row(y0):
    """Model P: SIM7600G-H with level shifter, I2C expander, power switch, SIM and antennas."""
    t(40, y0 - 40, 'Cellular: SIM7600G-H (4G LTE Cat-4, 3G/2G, GNSS) — modem I/O is 1.8 V', 13, weight='bold')
    u18 = ic(130, y0, 170, 'U18', 'TCA9535 I/O expander', ['VDD', 'A0–A2', 'GND'],
             ['SDA', 'SCL', 'INT', 'P0.0', 'P0.1', 'P0.2', 'P0.3', 'P0.4', 'P0.5', 'P0.6', 'P0.7', 'P1.0', 'P1.1', 'P1.2', 'P1.3', 'P1.4'],
             part='I2C 0x20')
    lab(u18, {'VDD': '3V3', 'GND': 'GND'}, 'l'); x, y = u18['A0–A2']; note(x - 6, y + 4, 'to GND', 'end')
    lab(u18, {'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL', 'INT': 'EXP_INT', 'P0.0': 'MDM_PWR_EN', 'P0.1': 'MDM_PWRKEY', 'P0.2': 'MDM_RESET',
              'P0.3': 'MDM_DTR', 'P0.4': 'MDM_STATUS', 'P0.5': 'MDM_RI', 'P0.6': 'VIB', 'P0.7': 'OTG_EN', 'P1.0': 'VOL_UP',
              'P1.1': 'VOL_DN', 'P1.2': 'PTT', 'P1.3': 'SIM_DET', 'P1.4': 'MDM_FLIGHT'}, 'r')
    u17 = ic(600, y0, 170, 'U17', 'TXS0108E', ['B1', 'B2', 'B3', 'B4', 'B5', 'B6', 'B7', 'B8', 'VCCB', 'OE', 'GND'],
             ['A1', 'A2', 'A3', 'A4', 'A5', 'A6', 'A7', 'A8', 'VCCA'], part='3.3 V (B) ↔ 1.8 V (A)')
    lab(u17, {'B1': 'MDM_TXD', 'B2': 'MDM_RXD', 'B3': 'MDM_RTS', 'B4': 'MDM_CTS', 'B5': 'PCM_CLK', 'B6': 'PCM_SYNC',
              'B7': 'PCM_OUT', 'B8': 'PCM_IN', 'VCCB': '3V3', 'OE': '3V3', 'GND': 'GND'}, 'l')
    lab(u17, {'VCCA': '1V8_MDM'}, 'r')
    u16 = ic(1000, y0, 210, 'U16', 'SIMCom SIM7600G-H',
             ['RXD', 'TXD', 'RTS', 'CTS', 'PCM_CLK', 'PCM_SYNC', 'PCM_DIN', 'PCM_DOUT', 'VDD_EXT', 'PWRKEY', 'RESET', 'DTR', 'STATUS', 'RI', 'FLIGHTMODE', 'GND'],
             ['MAIN_ANT', 'AUX_ANT', 'GNSS_ANT', None, 'USIM_VDD', 'USIM_DATA', 'USIM_CLK', 'USIM_RST', None, 'USB', None, None, 'VBAT'],
             part='LTE Cat-4 · 3G/2G · GNSS')
    for a, b in zip(['A1', 'A2', 'A3', 'A4', 'A5', 'A6', 'A7', 'A8'], ['RXD', 'TXD', 'RTS', 'CTS', 'PCM_CLK', 'PCM_SYNC', 'PCM_DIN', 'PCM_DOUT']):
        wire(u17[a], u16[b])
    lab(u16, {'VDD_EXT': '1V8_MDM', 'PWRKEY': 'MDM_PWRKEY', 'RESET': 'MDM_RESET', 'DTR': 'MDM_DTR', 'STATUS': 'MDM_STATUS',
              'RI': 'MDM_RI', 'FLIGHTMODE': 'MDM_FLIGHT', 'GND': 'GND'}, 'l')
    note(600, y0 + 430, 'PWRKEY, RESET: NPN open collector Q3 / Q4 (pulled to GND from the expander side)')
    note(600, y0 + 444, 'DTR, STATUS, RI, FLIGHTMODE: through U19 TXS0104E (1.8 V ↔ 3.3 V), not drawn')
    # antennas
    for (pin, dx, txt) in (('MAIN_ANT', 60, 'ANT2 LTE main'), ('AUX_ANT', 130, 'ANT3 diversity'), ('GNSS_ANT', 200, 'ANT4 GNSS')):
        x, y = u16[pin]; line([(x, y), (x + dx, y)]); antenna(x + dx, y, txt)
    # SIM holder
    j8 = ic(1420, u16['USIM_VDD'][1] - 30, 140, 'J8', 'nano-SIM', ['VCC', 'DATA', 'CLK', 'RST'], ['DET', 'GND'])
    for a, b, n in (('USIM_VDD', 'VCC', 'USIM_VDD'), ('USIM_DATA', 'DATA', 'USIM_DATA'), ('USIM_CLK', 'CLK', 'USIM_CLK'), ('USIM_RST', 'RST', 'USIM_RST')):
        wire(u16[a], j8[b], n)
    lab(j8, {'DET': 'SIM_DET', 'GND': 'GND'}, 'r')
    note(1490, j8['RST'][1] + 34, 'ESD: SMF05C, 22 Ω series, 33 pF', 'middle')
    # power switch + kill switch
    x, ya = u16['VBAT']
    line([(x, ya), (1300, ya)]); rect(1300, ya - 18, 100, 40, fill='#fff', stroke=INK, sw=1.2)
    t(1350, ya - 2, 'U20', 10, 'middle', 'bold'); t(1350, ya + 12, 'TPS22965 4 A', 8.5, 'middle')
    line([(1400, ya), (1430, ya)]); label(1430, ya, 'VBAT', 'r')
    note(1480, ya + 4, '(battery, not via U2)')
    note(1240, ya - 26, 'C 470µF + 100µF + 10nF + 33pF at VBAT')
    line([(1350, ya + 22), (1350, ya + 50)]); dot(1350, ya + 50); line([(1350, ya + 50), (1330, ya + 50)]); label(1330, ya + 50, 'MDM_PWR_EN', 'l')
    line([(1350, ya + 50), (1390, ya + 50), (1390, ya + 56)]); vbutton(1390, ya + 56); line([(1390, ya + 86), (1390, ya + 92)]); gnd(1390, ya + 92)
    t(1404, ya + 76, 'SW56 cellular kill switch', 9.5)
    x, y = u16['USB']; note(x + 6, y + 4, 'n.c. (later: P4 full-speed USB)')


def main():
    model, path = sys.argv[1], sys.argv[2]
    W, H = {'s': draw_s, 'p': draw_p}[model]()
    svg = (f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" '
           'font-family="DejaVu Sans, Arial, Helvetica, sans-serif">\n' + '\n'.join(out) + '\n</svg>\n')
    with open(path, 'w') as f:
        f.write(svg)


if __name__ == '__main__':
    main()

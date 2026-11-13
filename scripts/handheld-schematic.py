#!/usr/bin/env python3
# Draws docs/images/handheld-schematic.svg (overview schematic of docs/handheld.md).
#   python3 scripts/handheld-schematic.py docs/images/handheld-schematic.svg
import html, sys
W, H = 2140, 1160
P = 24
out = []
BLUE, RED, INK, BOX, BOXS = '#1d4ed8', '#b91c1c', '#111827', '#fff8db', '#7c2d12'

def t(x, y, s, size=11, anchor='start', weight='normal', fill=INK, italic=False):
    st = ' font-style="italic"' if italic else ''
    out.append(f'<text x="{x}" y="{y}" font-size="{size}" text-anchor="{anchor}" font-weight="{weight}" fill="{fill}"{st}>{html.escape(s)}</text>')

def line(pts, w=1.3, col=INK):
    out.append('<polyline points="%s" fill="none" stroke="%s" stroke-width="%s"/>' % (' '.join(f'{x},{y}' for x, y in pts), col, w))

def rect(x, y, w, h, fill=BOX, stroke=BOXS, sw=1.6, rx=2, dash=False):
    d = ' stroke-dasharray="6 4"' if dash else ''
    out.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" stroke="{stroke}" stroke-width="{sw}"{d}/>')

def circle(x, y, r, fill='#fff'):
    out.append(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}" stroke="{INK}" stroke-width="1.3"/>')

def dot(x, y):
    out.append(f'<circle cx="{x}" cy="{y}" r="2.6" fill="{INK}"/>')

def label(x, y, s, side, power=False):
    col = RED if power else BLUE
    w = 7 * len(s) + 12
    x0 = x if side == 'r' else x - w
    out.append(f'<rect x="{x0}" y="{y-8}" width="{w}" height="16" rx="8" fill="#fff" stroke="{col}" stroke-width="1.1"/>')
    t(x0 + w / 2, y + 4, s, 10.5, 'middle', 'bold', col)

def gnd(x, y):
    line([(x, y), (x, y + 8)])
    for i, hw in enumerate((8, 5, 2)):
        line([(x - hw, y + 8 + 3 * i), (x + hw, y + 8 + 3 * i)])

def ic(x, y, w, ref, name, left, right, top=30, stub=26, part=None, inside=False):
    n = max(len(left), len(right))
    h = top + (n - 1) * P + 18
    rect(x, y, w, h)
    hy = y + 15 if inside else y - 7
    t(x + (6 if inside else 0), hy, ref, 12.5, weight='bold')
    t(x + w - (6 if inside else 0), hy, name, 11, 'end')
    if part:
        t(x + w / 2, y + 15, part, 9.5, 'middle', fill='#57534e', italic=True)
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
    return pins, h

def wire(a, b, name=None, at=0.5):
    line([a, b])
    if name:
        t(a[0] + (b[0] - a[0]) * at, a[1] - 4, name, 9.5, 'middle', fill=BLUE)

def lab(pins, mapping, side, power=()):
    for p, n in mapping.items():
        x, y = pins[p]
        if n == 'GND':
            line([(x, y), (x + (-8 if side == 'l' else 8), y)]); gnd(x + (-8 if side == 'l' else 8), y)
        else:
            label(x, y, n, side, n in power or n.startswith('3V3') or n in ('VBAT', 'VBUS', 'VRTC'))

POW = ()
# ---------------------------------------------------------------- frame
out.append(f'<rect x="0" y="0" width="{W}" height="{H}" fill="#ffffff"/>')
rect(10, 10, W - 20, H - 20, fill='none', stroke='#9ca3af', sw=1, rx=0)
t(30, 46, 'mesh-msg handheld — schematic overview (rev A draft)', 22, weight='bold')
t(30, 70, 'Connections between blocks. Decoupling capacitors, ESD parts and footprints are listed in docs/handheld.md, not drawn. Nets with the same label are connected.', 12, fill='#4b5563')

# ---------------------------------------------------------------- U1 ESP32-S3
EX, EY, EW = 960, 140, 240
L = ['GPIO12', 'GPIO11', 'GPIO10', 'GPIO9', 'GPIO46', None, 'GPIO1', 'GPIO2', 'GPIO3', 'GPIO18', 'GPIO8', None,
     'GPIO19', 'GPIO20', None, 'GPIO0', 'GPIO43', 'GPIO44', None, 'EN', '3V3', 'GND']
R = ['GPIO40', 'GPIO41', 'GPIO39', 'GPIO38', 'GPIO42', 'GPIO47', 'GPIO48', None, 'GPIO7', 'GPIO21', 'GPIO4', None,
     'GPIO45', None, None, 'GPIO14', 'GPIO13', 'GPIO15', 'GPIO16', 'GPIO17', 'GPIO5', 'GPIO6']
u1, u1h = ic(EX, EY, EW, 'U1', 'ESP32-S3-WROOM-1-N16R8', L, R, part='16 MB flash · 8 MB PSRAM · Wi-Fi + BLE 5')
lab(u1, {'GPIO1': 'KEY_INT', 'GPIO2': 'PMU_IRQ', 'GPIO3': 'TP_INT', 'GPIO18': 'I2C_SDA', 'GPIO8': 'I2C_SCL',
         'GPIO43': 'TXD0', 'GPIO44': 'RXD0', '3V3': '3V3', 'GND': 'GND'}, 'l')
lab(u1, {'GPIO5': 'PA_EN', 'GPIO6': 'HP_DET'}, 'r')
x, y = u1['EN']; label(x, y, 'EN', 'l'); t(x - 40, y + 20, 'R 10k to 3V3, C 1µF to GND', 9.5, 'end', fill='#57534e')
t(EX - 30, u1['I2C_SCL' if False else 'GPIO8'][1] + 20, 'pull-ups: 10k on INT lines, 2.2k on I2C', 9.5, 'end', fill='#57534e')
# BOOT button
x, y = u1['GPIO0']
line([(x, y), (x - 40, y)]); circle(x - 44, y, 3); circle(x - 74, y, 3); line([(x - 46, y - 10), (x - 72, y - 10)]); line([(x - 59, y - 10), (x - 59, y - 16)])
line([(x - 77, y), (x - 90, y)]); gnd(x - 90, y); t(x - 59, y - 20, 'SW51 BOOT', 9.5, 'middle')

# ---------------------------------------------------------------- J1 LCD
j1, _ = ic(560, 140, 180, 'J1', '2.8" IPS ST7789 + CTP (FPC)', ['VCC', 'LEDA', 'RESET', 'TP_SDA', 'TP_SCL', 'TP_INT', 'TP_RST', 'GND'],
           ['SCLK', 'MOSI', 'CS', 'DC', 'LEDK'], part='320×240, SPI 80 MHz')
lab(j1, {'VCC': '3V3_LCD', 'LEDA': '3V3_LCD', 'RESET': 'LCD_RST', 'TP_SDA': 'I2C_SDA', 'TP_SCL': 'I2C_SCL', 'TP_INT': 'TP_INT', 'TP_RST': 'LCD_RST', 'GND': 'GND'}, 'l')
for a, b, n in (('SCLK', 'GPIO12', 'LCD_SCLK'), ('MOSI', 'GPIO11', 'LCD_MOSI'), ('CS', 'GPIO10', 'LCD_CS'), ('DC', 'GPIO9', 'LCD_DC')):
    wire(j1[a], u1[b], n)
# Q1 backlight
ya = j1['LEDK'][1]; xq = 820
line([j1['LEDK'], (xq, ya)]); line([(xq + 50, ya), u1['GPIO46']])
rect(xq, ya - 14, 50, 34, fill='#fff', stroke=INK, sw=1.2)
t(xq + 25, ya + 2, 'Q1', 10, 'middle', 'bold'); t(xq + 25, ya + 14, 'AO3400', 8.5, 'middle')
t(xq - 4, ya - 4, 'D', 9, 'end'); t(xq + 54, ya - 4, 'G', 9)
t(xq + 100, ya - 4, 'BL_PWM', 9.5, 'middle', fill=BLUE)
line([(xq + 25, ya + 20), (xq + 25, ya + 30)]); gnd(xq + 25, ya + 30); t(xq + 31, ya + 30, 'S', 9)

# ---------------------------------------------------------------- J2 USB-C + U9
j2, _ = ic(560, u1['GPIO19'][1] - 30, 160, 'J2', 'USB-C receptacle', ['VBUS', 'CC1', 'CC2', 'GND'], ['D−', 'D+'], part='5.1k on CC1/CC2 to GND')
lab(j2, {'VBUS': 'VBUS', 'GND': 'GND'}, 'l')
for p in ('CC1', 'CC2'):
    x, y = j2[p]; line([(x, y), (x - 14, y)]); rect(x - 44, y - 5, 30, 10, fill='#fff', stroke=INK, sw=1.1); line([(x - 44, y), (x - 54, y)]); gnd(x - 54, y)
xu = 800
for a, b, n in (('D−', 'GPIO19', 'USB_D−'), ('D+', 'GPIO20', 'USB_D+')):
    line([j2[a], (xu, j2[a][1])]); line([(xu + 70, j2[a][1]), u1[b]]); t(xu + 115, j2[a][1] - 4, n, 9.5, 'middle', fill=BLUE)
rect(xu, j2['D−'][1] - 16, 70, 56, fill='#fff', stroke=INK, sw=1.2)
t(xu + 35, j2['D−'][1] + 10, 'U9', 10, 'middle', 'bold'); t(xu + 35, j2['D−'][1] + 22, 'USBLC6-2', 8.5, 'middle')

# ---------------------------------------------------------------- U3 TCA8418 + key matrix
u3, _ = ic(130, 140, 190, 'U3', 'TCA8418 keypad scanner', ['ROW0–ROW4', 'COL0–COL9'],
           ['INT', 'SDA', 'SCL', 'ROW5 (GPIO)', 'ROW6 (GPIO)', 'ROW7 (GPIO)', 'VCC', 'GND'], part='I2C 0x34, 8×10 max')
lab(u3, {'INT': 'KEY_INT', 'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL', 'ROW5 (GPIO)': 'LCD_RST', 'ROW6 (GPIO)': 'KBD_BL', 'ROW7 (GPIO)': 'OPT_RST', 'VCC': '3V3', 'GND': 'GND'}, 'r')
mx, my, mw, mh = 40, 410, 330, 275
for p, xo in (('ROW0–ROW4', 0), ('COL0–COL9', 14)):
    x, y = u3[p]
    line([(x, y), (60 + xo, y), (60 + xo, my)], w=3.2)
t(82, my - 10, 'ROW[0..4]', 9.5, fill=BLUE); t(82, my - 24, 'COL[0..9]', 9.5, fill=BLUE)
rect(mx, my, mw, mh, fill='#f8fafc', stroke='#475569', sw=1.3)
t(mx + 8, my + 18, 'Key matrix 5 × 10: SW1–SW50, D1–D50 1N4148W', 11, weight='bold')
gx, gy = mx + 60, my + 64
for r in range(3):
    line([(gx - 30, gy + r * 55), (gx + 230, gy + r * 55)])
    t(gx - 34, gy + r * 55 + 4, f'R{r}', 9.5, 'end')
for c in range(4):
    cx = gx + c * 60
    line([(cx + 30, gy - 20), (cx + 30, gy + 150)])
    t(cx + 30, gy - 24, f'C{c}', 9.5, 'middle')
    for r in range(3):
        ry = gy + r * 55
        dot(cx, ry)
        line([(cx, ry), (cx, ry + 8)]); line([(cx, ry + 8), (cx + 8, ry + 18)]); line([(cx, ry + 20), (cx, ry + 26)])  # switch
        out.append(f'<polygon points="{cx-5},{ry+26} {cx+5},{ry+26} {cx},{ry+34}" fill="{INK}"/>'); line([(cx - 5, ry + 34), (cx + 5, ry + 34)])
        line([(cx, ry + 34), (cx, ry + 40), (cx + 30, ry + 40)]); dot(cx + 30, ry + 40)
t(gx + 250, gy + 60, '…', 18); t(gx + 100, gy + 172, '⋮  key → diode (anode on row) → column', 10, 'middle', fill='#57534e')
t(mx + 8, my + mh - 8, 'Space = 2 switches in parallel; power key on U2 PWRON, not in the matrix', 9.5, fill='#57534e')

# ---------------------------------------------------------------- U2 AXP2101
u2, _ = ic(270, 740, 190, 'U2', 'AXP2101 PMU', ['VBUS', 'TS', None, 'PWRON', 'VBAT', None, 'GND'],
           ['DCDC1', 'ALDO2', 'ALDO3', 'ALDO4', 'BLDO1', 'BLDO2', 'IRQ', 'SDA', 'SCL'], part='charger 1 A · fuel gauge')
lab(u2, {'DCDC1': '3V3', 'ALDO2': '3V3_LORA', 'ALDO3': '3V3_AUDIO', 'ALDO4': '3V3_SD', 'BLDO1': '3V3_LCD', 'BLDO2': '3V3_GPS',
         'IRQ': 'PMU_IRQ', 'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL'}, 'r')
lab(u2, {'VBUS': 'VBUS', 'GND': 'GND'}, 'l')
x, y = u2['VBAT']; line([(x, y), (x - 70, y)]); dot(x - 40, y)
line([(x - 40, y), (x - 40, y + 10)]); line([(x - 54, y + 10), (x - 26, y + 10)], w=2.2); line([(x - 48, y + 16), (x - 32, y + 16)], w=2.2); line([(x - 40, y + 16), (x - 40, y + 24)]); gnd(x - 40, y + 24)
t(x - 60, y + 26, 'BT1 Li-ion 3000 mAh', 9.5, 'end'); label(x - 70, y, 'VBAT', 'l', True)
x, y = u2['TS']; line([(x, y), (x - 12, y)]); t(x - 16, y + 4, 'NTC 10k in the battery', 9.5, 'end', fill='#57534e')
x, y = u2['PWRON']; line([(x, y), (x - 20, y)]); circle(x - 23, y, 3); circle(x - 50, y, 3); line([(x - 25, y - 9), (x - 48, y - 9)]); line([(x - 53, y), (x - 62, y)]); gnd(x - 62, y)
t(x - 36, y - 13, 'SW52 POWER', 9, 'middle')

# ---------------------------------------------------------------- RTC
u7, _ = ic(700, 830, 160, 'U7', 'PCF85063A RTC', ['OSCI', 'OSCO', 'VDD', 'VSS'], ['SDA', 'SCL', 'INT'])
lab(u7, {'VDD': 'VRTC', 'VSS': 'GND'}, 'l')
x1, y1 = u7['OSCI']; x2, y2 = u7['OSCO']
line([(x1, y1), (x1 - 20, y1)]); line([(x2, y2), (x2 - 20, y2)]); line([(x1 - 20, y1), (x1 - 20, y1 + 6)]); line([(x2 - 20, y2), (x2 - 20, y2 - 6)])
rect(x1 - 26, y1 + 8, 12, 8, fill='#fff', stroke=INK, sw=1.1); t(x1 - 32, y1 + 16, 'Y1 32.768 kHz', 9.5, 'end')
lab(u7, {'SDA': 'I2C_SDA', 'SCL': 'I2C_SCL'}, 'r')
x, y = u7['INT']; t(x + 6, y + 4, 'n.c.', 9.5, fill='#57534e')
u8, _ = ic(700, 990, 160, 'U8', 'TPS7A02 3.0 V, 25 nA', ['IN', 'GND'], ['OUT'])
lab(u8, {'IN': 'VBAT', 'GND': 'GND'}, 'l')
lab(u8, {'OUT': 'VRTC'}, 'r')

# ---------------------------------------------------------------- J6 option
j6, _ = ic(1010, 880, 180, 'J6', 'GPS / LTE option', ['RXD', 'TXD', 'RESET', 'VCC', 'GND'], [], top=30)
lab(j6, {'RXD': 'TXD0', 'TXD': 'RXD0', 'RESET': 'OPT_RST', 'VCC': '3V3_GPS', 'GND': 'GND'}, 'l')
t(1100, 1042, 'L76K GPS or A7670 LTE: fit one', 9.5, 'middle', fill='#57534e')

# ---------------------------------------------------------------- U4 LoRa
u4, _ = ic(1400, 140, 190, 'U4', 'SX1262 LoRa module', ['SCK', 'MOSI', 'MISO', 'NSS', 'NRESET', 'BUSY', 'DIO1'], ['ANT', 'VCC', 'GND'],
           part='TCXO · DIO2 RF switch · +22 dBm')
for a, b, n in (('SCK', 'GPIO40', 'LORA_SCK'), ('MOSI', 'GPIO41', 'LORA_MOSI'), ('MISO', 'GPIO39', 'LORA_MISO'), ('NSS', 'GPIO38', 'LORA_CS'),
                ('NRESET', 'GPIO42', 'LORA_RST'), ('BUSY', 'GPIO47', 'LORA_BUSY'), ('DIO1', 'GPIO48', 'LORA_DIO1')):
    wire(u1[b], u4[a], n)
lab(u4, {'VCC': '3V3_LORA', 'GND': 'GND'}, 'r')
x, y = u4['ANT']; line([(x, y), (x + 70, y)]); circle(x + 74, y, 4)
line([(x + 78, y), (x + 100, y), (x + 100, y - 30)]); out.append(f'<polygon points="{x+88},{y-50} {x+112},{y-50} {x+100},{y-30}" fill="none" stroke="{INK}" stroke-width="1.3"/>')
t(x + 40, y - 8, 'J3 u.FL → SMA', 9.5, 'middle'); t(x + 118, y - 40, 'ANT1 whip', 9.5)

# ---------------------------------------------------------------- J5 microSD
j5, _ = ic(1400, u1['GPIO7'][1] - 28, 190, 'J5', 'microSD socket', ['CLK', 'CMD', 'DAT0'], ['VDD', 'DAT1–3', 'CD', 'GND'], top=28, inside=True)
for a, b, n in (('CLK', 'GPIO7', 'SD_CLK'), ('CMD', 'GPIO21', 'SD_CMD'), ('DAT0', 'GPIO4', 'SD_D0')):
    wire(u1[b], j5[a], n)
lab(j5, {'VDD': '3V3_SD', 'GND': 'GND'}, 'r'); x, y = j5['DAT1–3']; t(x + 6, y + 4, '10k to 3V3_SD', 9.5, fill='#57534e')
x, y = j5['CD']; t(x + 6, y + 4, 'n.c.', 9.5, fill='#57534e')

# ---------------------------------------------------------------- vibration
x, y = u1['GPIO45']; xq = 1300
line([(x, y), (xq, y)]); rect(xq, y - 14, 60, 34, fill='#fff', stroke=INK, sw=1.2)
t(xq + 30, y + 2, 'Q2', 10, 'middle', 'bold'); t(xq + 30, y + 14, 'AO3400', 8.5, 'middle'); t(xq - 30, y - 4, 'VIB', 9.5, 'middle', fill=BLUE)
line([(xq + 60, y), (xq + 390, y)]); xq += 280; circle(xq + 124, y, 14); t(xq + 124, y + 4, 'M', 11, 'middle', 'bold')
line([(xq + 138, y), (xq + 160, y)]); label(xq + 160, y, 'VBAT', 'r', True)
t(xq + 124, y + 30, 'M1 coin motor + flyback diode', 9.5, 'middle', fill='#57534e')
line([(xq - 250, y + 20), (xq - 250, y + 28)]); gnd(xq - 250, y + 28)

# ---------------------------------------------------------------- U5 ES8388
u5, _ = ic(1400, u1['GPIO14'][1] - 30, 190, 'U5', 'ES8388 audio codec',
           ['MCLK', 'SCLK', 'LRCK', 'DSDIN', 'ASDOUT', None, None, 'CCLK', 'CDATA', 'DVDD', 'AVDD', 'GND'],
           ['LIN1', 'LIN2', 'RIN2', 'MICBIAS', 'LOUT1', 'ROUT1', 'LOUT2', 'ROUT2'], part='I2C 0x10 · 16 kHz · MCLK 256 fs')
for a, b, n in (('MCLK', 'GPIO14', 'I2S_MCLK'), ('SCLK', 'GPIO13', 'I2S_BCLK'), ('LRCK', 'GPIO15', 'I2S_LRCK'), ('DSDIN', 'GPIO16', 'I2S_DOUT'), ('ASDOUT', 'GPIO17', 'I2S_DIN')):
    wire(u1[b], u5[a], n)
lab(u5, {'CCLK': 'I2C_SCL', 'CDATA': 'I2C_SDA', 'DVDD': '3V3_AUDIO', 'AVDD': '3V3_AUDIO', 'GND': 'GND'}, 'l')
lab(u5, {'LIN1': 'MIC', 'LIN2': 'HS_MIC', 'RIN2': 'AEC_REF', 'MICBIAS': 'MICBIAS', 'LOUT1': 'HS_L', 'ROUT1': 'HS_R', 'LOUT2': 'EAR', 'ROUT2': 'SPK_IN'}, 'r')

# ---------------------------------------------------------------- audio parts (column at x 1800)
AX = 1820
# MK1 mic
y = 500
label(AX, y, 'MICBIAS', 'l'); line([(AX, y), (AX + 20, y)]); rect(AX + 20, y - 5, 30, 10, fill='#fff', stroke=INK, sw=1.1); t(AX + 35, y - 9, 'R 2.2k', 9, 'middle')
line([(AX + 50, y), (AX + 80, y), (AX + 80, y + 22)]); dot(AX + 80, y + 22)
label(AX, y + 22, 'MIC', 'l'); line([(AX, y + 22), (AX + 80, y + 22), (AX + 110, y + 22)])
circle(AX + 126, y + 22, 16); line([(AX + 112, y + 8), (AX + 112, y + 36)], w=2)
line([(AX + 142, y + 22), (AX + 160, y + 22)]); gnd(AX + 160, y + 22); t(AX + 126, y + 54, 'MK1 analog MEMS mic (1µF series C)', 9.5, 'middle')
# J4 TRRS
j4, _ = ic(AX + 40, 600, 140, 'J4', '3.5 mm TRRS (CTIA)', ['TIP', 'RING1', 'RING2', 'SLEEVE', 'DET'], [], top=30, stub=22)
lab(j4, {'TIP': 'HS_L', 'RING1': 'HS_R', 'SLEEVE': 'HS_MIC', 'DET': 'HP_DET'}, 'l')
x, y = j4['RING2']; line([(x, y), (x - 8, y)]); gnd(x - 8, y)
t(AX + 110, 760, 'HS_MIC: bias 2.2k from MICBIAS', 9.5, 'middle', fill='#57534e')
# LS1 earpiece
y = 780
label(AX, y, 'EAR', 'l'); line([(AX, y), (AX + 30, y)]); line([(AX + 30, y - 8), (AX + 30, y + 8)], w=2); line([(AX + 36, y - 8), (AX + 36, y + 8)], w=2)
line([(AX + 36, y), (AX + 80, y)])
rect(AX + 80, y - 8, 10, 16, fill='#fff', stroke=INK, sw=1.2); out.append(f'<polygon points="{AX+90},{y-8} {AX+104},{y-18} {AX+104},{y+18} {AX+90},{y+8}" fill="#fff" stroke="{INK}" stroke-width="1.2"/>')
line([(AX + 85, y + 8), (AX + 85, y + 24)]); gnd(AX + 85, y + 24); t(AX + 112, y + 4, 'LS1 earpiece 32 Ω', 9.5); t(AX + 33, y - 12, '10µF', 9, 'middle')
# U6 NS4150B + LS2
u6, _ = ic(AX + 40, 860, 110, 'U6', 'NS4150B', ['IN+', 'IN−', 'CTRL', 'VDD', 'GND'], ['OUT+', 'OUT−'], top=30, stub=22)
lab(u6, {'IN+': 'SPK_IN', 'CTRL': 'PA_EN', 'VDD': 'VBAT', 'GND': 'GND'}, 'l')
x, y = u6['IN−']; line([(x, y), (x - 8, y)]); gnd(x - 8, y)
xo, yo = u6['OUT+']; xm, ym = u6['OUT−']
line([(xo, yo), (xo + 20, yo), (xo + 20, yo + 4)]); line([(xm, ym), (xo + 20, ym), (xo + 20, ym - 4)])
rect(xo + 14, yo + 4, 12, 16, fill='#fff', stroke=INK, sw=1.2)
out.append(f'<polygon points="{xo+26},{yo+4} {xo+42},{yo-8} {xo+42},{yo+32} {xo+26},{yo+20}" fill="#fff" stroke="{INK}" stroke-width="1.2"/>')
t(xo - 4, yo - 24, 'LS2 speaker 8 Ω 1 W', 9.5)
t(AX + 95, 1022, 'SPK_IN: 0.1µF series C at IN+', 9.5, 'middle', fill='#57534e')
# AEC divider
y = 1052
label(AX - 140, y, 'SPK_IN', 'l'); line([(AX - 140, y), (AX - 110, y)]); rect(AX - 110, y - 5, 30, 10, fill='#fff', stroke=INK, sw=1.1)
line([(AX - 80, y), (AX - 40, y)]); dot(AX - 60, y); line([(AX - 60, y), (AX - 60, y + 6)]); rect(AX - 65, y + 6, 10, 22, fill='#fff', stroke=INK, sw=1.1); line([(AX - 60, y + 28), (AX - 60, y + 34)]); gnd(AX - 60, y + 34)
line([(AX - 40, y - 8), (AX - 40, y + 8)], w=2); line([(AX - 34, y - 8), (AX - 34, y + 8)], w=2); line([(AX - 34, y), (AX - 10, y)]); label(AX - 10, y, 'AEC_REF', 'r')
t(AX - 95, y - 10, '10k', 9, 'middle'); t(AX - 50, y + 22, '2.2k', 9); t(AX - 37, y - 12, '1µF', 9, 'middle')
t(AX - 200, y + 62, 'AEC reference: speaker signal back into the right ADC', 9.5, fill='#57534e')

# ---------------------------------------------------------------- legend / title block
bx, by = 1230, 1010
rect(bx, by, 370, 92, fill='#fff', stroke='#9ca3af', sw=1)
t(bx + 10, by + 20, 'mesh-msg handheld · schematic overview', 12, weight='bold')
t(bx + 10, by + 38, 'Rev A draft · 2026-11-13 · not for fabrication', 10.5)
t(bx + 10, by + 56, 'Design notes, BOM and pin rules: docs/handheld.md', 10.5)
label(bx + 10, by + 76, 'NET', 'r'); t(bx + 60, by + 80, 'signal net', 10)
label(bx + 130, by + 76, '3V3_x', 'r', True); t(bx + 190, by + 80, 'power rail (from U2)', 10)

svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="DejaVu Sans, Arial, Helvetica, sans-serif">\n' + '\n'.join(out) + '\n</svg>\n'
open(sys.argv[1], 'w').write(svg)

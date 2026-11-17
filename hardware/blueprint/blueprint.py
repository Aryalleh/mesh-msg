#!/usr/bin/env python3
"""The Mava blueprint: one poster of the whole project — networks, protocol, Bluetooth,
LoRa on Meshtastic, the board, the handheld and its keyboard module, software, security
and release — drawn in the project's violet blueprint style (docs/design.md).

    python3 hardware/blueprint/blueprint.py      → docs/images/mava-blueprint.{png,pdf}

Facts come from the docs and code (architecture.md, protocol.md, board.md,
handheld-proto.md, web-access.md, firmware/, web/); update them here when they change.
"""
import os

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.patches import FancyArrowPatch, FancyBboxPatch, Polygon, Rectangle  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs", "images", "mava-blueprint")

BG, GRID, GRID2 = "#120A2A", "#1D1240", "#2A1C58"
LINE, FG, MUTED = "#C9B8FF", "#ECE8FF", "#9C92C8"
CYAN, MAGENTA, VIOLET, GREEN, AMBER = "#22D3EE", "#E879F9", "#A78BFA", "#4ADE80", "#FBBF24"
MONO, SANS = "DejaVu Sans Mono", "DejaVu Sans"

W, H = 300.0, 200.0
fig = plt.figure(figsize=(60, 40), dpi=120)
fig.patch.set_facecolor(BG)
ax = fig.add_axes([0, 0, 1, 1])
ax.set_xlim(0, W)
ax.set_ylim(0, H)
ax.set_axis_off()
ax.set_facecolor(BG)

# blueprint grid
for x in range(0, int(W) + 1):
    ax.plot([x, x], [0, H], color=GRID2 if x % 10 == 0 else GRID, lw=0.9 if x % 10 == 0 else 0.4, zorder=0)
for y in range(0, int(H) + 1):
    ax.plot([0, W], [y, y], color=GRID2 if y % 10 == 0 else GRID, lw=0.9 if y % 10 == 0 else 0.4, zorder=0)


def chamfer(x, y, w, h, c=1.2):
    return [(x + c, y), (x + w, y), (x + w, y + h - c), (x + w - c, y + h), (x, y + h), (x, y + c)]


def panel(x, y, w, h, num, title, sub=""):
    ax.add_patch(Polygon(chamfer(x, y, w, h, 2.0), closed=True, fill=True, fc="#170E36", ec=VIOLET, lw=2.4, zorder=1))
    ax.add_patch(Rectangle((x + 1.2, y + h - 5.2), 6.5, 3.6, fc=MAGENTA, ec="none", zorder=2))
    ax.text(x + 4.45, y + h - 3.4, f"{num:02d}", color=BG, fontsize=26, weight="bold", ha="center", va="center",
            family=MONO, zorder=3)
    ax.text(x + 9.2, y + h - 3.3, title.upper(), color=FG, fontsize=30, weight="bold", va="center", family=SANS, zorder=3)
    if sub:
        ax.text(x + 9.2, y + h - 6.4, sub, color=MUTED, fontsize=17, va="center", family=MONO, zorder=3)
    ax.plot([x + 1.2, x + w - 1.2], [y + h - 7.8, y + h - 7.8], color=GRID2, lw=1.5, zorder=2)


FS = 1.45  # text scale for the poster


def box(x, y, w, h, title, lines=(), color=LINE, fs=15, tfs=18, fill="#1C1244"):
    fs, tfs = fs * FS, tfs * FS * 0.9
    ax.add_patch(Polygon(chamfer(x, y, w, h, 0.8), closed=True, fc=fill, ec=color, lw=1.8, zorder=4))
    ax.text(x + 0.8, y + h - 1.0, title, color=color, fontsize=tfs, weight="bold", va="top", family=SANS, zorder=5)
    for i, l in enumerate(lines):
        ax.text(x + 0.8, y + h - 3.3 - i * (fs / 7.2), l, color=FG, fontsize=fs, va="top", family=MONO, zorder=5)


def arrow(x1, y1, x2, y2, label="", color=CYAN, style="<|-|>", lw=2.2, ls="-", fs=14, off=(0, 0.7), rad=0.0):
    ax.add_patch(FancyArrowPatch((x1, y1), (x2, y2), arrowstyle=style, mutation_scale=22, color=color, lw=lw,
                                 linestyle=ls, connectionstyle=f"arc3,rad={rad}", zorder=6))
    if label:
        ax.text((x1 + x2) / 2 + off[0], (y1 + y2) / 2 + off[1], label, color=color, fontsize=fs, ha="center",
                va="bottom", family=MONO, zorder=7, bbox=dict(fc=BG, ec="none", pad=1.5, alpha=0.85))


def text(x, y, s, fs=15, color=FG, **kw):
    fs = fs * FS
    kw.setdefault("family", MONO)
    ax.text(x, y, s, color=color, fontsize=fs, va="top", zorder=5, **kw)


# ======================================================================== title
ax.text(3, 197.2, "MAVA", color=VIOLET, fontsize=96, weight="bold", family=SANS, va="top",
        path_effects=None)
ax.text(36, 196.0, "MESH OF MESHES — END-TO-END ENCRYPTED MESSAGING WITHOUT A WORKING INTERNET", color=FG, fontsize=34,
        weight="bold", family=SANS, va="top")
ax.text(36, 191.4, "phones · laptops · Mava board (ESP32-S3 + SX1262) · handheld · LoRa on Meshtastic · Bluetooth · "
        "regional + global relays · browsers (iPhone)        github.com/Aryalleh/mesh-msg · v0.1.1-beta7 (test) · "
        "blueprint rev 2026-11-17", color=MUTED, fontsize=20, family=MONO, va="top")

# ======================================================================== 01 network
panel(2, 112, 148, 74, 1, "Network: four tiers, one packet format", "docs/architecture.md")
# tier 2b
for i, n in enumerate(["EU relay", "US relay", "ASIA relay", "AU relay"]):
    box(8 + i * 34, 165, 26, 9, n, ["Go · HTTPS · SQLite"], color=VIOLET, fs=13, tfs=16)
    if i:
        arrow(8 + i * 34 - 8, 169.5, 8 + i * 34, 169.5, color=VIOLET, lw=1.8)
text(97, 178.2, "TIER 2b  global federation (/v1/sync)", 15, VIOLET, weight="bold")
box(8, 146, 44, 11, "BRIDGE", ["meshd --bridge / phone in bridge mode", "VPN · Starlink · unfiltered SIM"], color=AMBER,
    fs=13, tfs=16)
arrow(30, 157, 21, 165, "international (cut in blackout)", color=AMBER, ls="--", fs=13, off=(10, 0))
box(62, 146, 46, 11, "TIER 2a  regional relay (e.g. IR)", ["domestic network, usually up", "small, many, disposable"],
    color=VIOLET, fs=13, tfs=16)
arrow(52, 151.5, 62, 151.5, "sync", color=AMBER)
box(114, 146, 33, 11, "GATEWAYS", ["phone / PC / Mava board", "POST /v1/gateway (PoW)"], color=CYAN, fs=13, tfs=16)
arrow(108, 151.5, 114, 151.5, color=CYAN)
# tier 0 / 1
box(6, 117, 40, 22, "TIER 0  local", ["BLE GATT phone mesh 10–100 m", "Wi-Fi Direct · LAN (meshd)", "direct IP links (NAT-agnostic)",
                                     "every phone & PC is a router"], color=CYAN, fs=13, tfs=16)
box(54, 117, 46, 22, "TIER 1  LoRa backbone", ["SX1262 · Meshtastic LongFast", "SF11 · 250 kHz · CR 4/5 · 0x2B",
                                               "869.525 MHz (EU) · 906.875 (US)", "1–15 km per hop · ≤233 B / frame",
                                               "managed flood, 3 hops, dedup 24 h"], color=MAGENTA, fs=13, tfs=16)
box(108, 117, 39, 22, "ROLES", ["client · gateway · bridge", "relay · LoRa repeater", "any device holds several",
                                "identity = keypair", "address = 16-B hash"], color=LINE, fs=13, tfs=16)
arrow(46, 128, 54, 128, "BLE / USB host link", color=CYAN, fs=12, off=(0, 0.8))
arrow(26, 139, 75, 146, "gateway upload", color=CYAN, fs=12, rad=-0.15)
arrow(77, 139, 120, 146, "board Wi-Fi uplink", color=CYAN, fs=12, rad=-0.1)

# ======================================================================== 02 packet
panel(152, 112, 72, 74, 2, "Packet & encryption", "docs/protocol.md · spec/test-vectors")
text(155, 175, "OUTER HEADER — 26 bytes, the only thing relays see", 16, CYAN, weight="bold")
fields = [("ver·type", 6), ("ctx·hop", 6), ("max", 4.5), ("packet id · 8 B", 14.5), ("dest hash · 16 B", 20),
          ("frag", 4.5), ("body", 10)]
x = 155
for name, w2 in fields:
    ax.add_patch(Rectangle((x, 166.5), w2, 5, fc="#241650", ec=CYAN, lw=1.5, zorder=4))
    ax.text(x + w2 / 2, 169, name, color=FG, fontsize=15, ha="center", va="center", family=MONO, zorder=5)
    x += w2
text(155, 164.5, "types: DATA · RECEIPT · ANNOUNCE · PATH_REQ · SUMMARY · LINK", 13.5)
text(155, 161.5, "ctx flags: PRIORITY · NO_LORA · FRAGMENTED (≤16 frags)", 13.5)
box(155, 136, 32, 22, "Noise K", ["known contact", "25519 · ChaChaPoly", "SHA-256", "48 B overhead", "prologue binds",
                                  "header ↔ ciphertext"], color=VIOLET, fs=13, tfs=17)
box(189, 136, 32, 22, "Noise X", ["first contact", "sender key inside", "the ciphertext:", "sender hidden from",
                                  "relays & gateways", "+ ephemeral FS"], color=VIOLET, fs=13, tfs=17)
box(155, 116, 66, 18, "Sizes on LoRa", ["MTU 233 B = Meshtastic DATA_PAYLOAD_LEN", "233 − 26 hdr − 48 Noise − 5 inner",
                                        "= 154 B text ≈ 150 Persian chars (compact enc.)",
                                        "RECEIPT signed (Ed25519) purges copies"], color=GREEN, fs=13, tfs=17)

# ======================================================================== 03 BLE
panel(226, 112, 72, 74, 3, "How Bluetooth LE works here", "android/ble · ios · firmware/src/ble.cpp")
box(229, 154, 33, 23, "PHONE ↔ PHONE mesh", ["svc 6d657368-6d73-6700…", "RX …6701  write", "TX …6702  notify",
                                              "advert: svc + 4-B session", "  (random per start)",
                                              "larger session = client"], color=CYAN, fs=12, tfs=15)
box(264, 154, 32, 23, "PHONE ↔ BOARD host", ["svc 6d657368-6c72-0000…", "RX …0001 write (MITM)", "TX …0002 notify",
                                             "6-digit passkey bond", "COBS frames + CRC16", "same bytes as USB"],
    color=MAGENTA, fs=12, tfs=15)
text(229, 151.5, "LINK SEQUENCE", 15, VIOLET, weight="bold")
seq = [("scan / advertise", 0), ("connect (GATT)", 1), ("MTU 23 → 247", 2), ("subscribe TX (CCCD)", 3),
       ("hello 0x4D + session", 4), ("chunked packets ⇄", 5)]
for i, (s, _) in enumerate(seq):
    yy = 147.5 - i * 4.4
    ax.add_patch(Rectangle((229, yy - 1.6), 4, 3.2, fc=CYAN if i % 2 == 0 else MAGENTA, ec="none", zorder=4))
    ax.text(231, yy, str(i + 1), color=BG, fontsize=20, weight="bold", ha="center", va="center", zorder=5)
    text(234.5, yy + 1.1, s, 14)
    if i < len(seq) - 1:
        arrow(231, yy - 1.6, 231, yy - 2.8, color=LINE, style="-|>", lw=1.4)
box(262, 116, 34, 34, "RULES", ["≤4 peers per phone", "core chunks 180 B,", "split to the MTU", "2 MB queue / peer",
                                "no vendor ID in advert", "  (company 0xFFFF)", "iPhone: endpoint,", "  foreground first",
                                "board: 1 phone at a", "  time, host port 1"], color=LINE, fs=12.5, tfs=15)

# ======================================================================== 04 board
panel(2, 34, 98, 75, 4, "Mava board", "docs/board.md · firmware/ (PlatformIO, C++17)")
box(5, 76, 44, 25, "ESP32-S3-WROOM-1-N16R8", ["2× Xtensa LX7 @ 240 MHz", "512 KB SRAM + 8 MB PSRAM (OPI)",
                                               "16 MB flash: 2×4 MB OTA app", "  + 7.9 MB LittleFS",
                                               "Wi-Fi 4 · BLE 5 · native USB", "LVGL UI · NimBLE · RadioLib"],
    color=VIOLET, fs=12.5, tfs=15)
box(52, 86, 45, 15, "SX1262 LoRa (E22-900M22S)", ["150–960 MHz · +22 dBm · TCXO", "own SPI: 12/11/13 · CS 10",
                                                   "DIO1 1 · BUSY 4 · RST 8"], color=MAGENTA, fs=12.5, tfs=15)
arrow(49, 92, 52, 92, color=MAGENTA)
box(52, 66, 45, 17, "node::Node (host-tested)", ["router · MAC (CAD, duty cycle)", "store 64 slots, per-sink bits",
                                                  "ports: USB 0 · BLE 1 · web 2", "uplink sink → relay worker"],
    color=CYAN, fs=12.5, tfs=15)
box(5, 54, 44, 19, "SETUP SWITCH (GPIO 14)", ["ON : hotspot Mava-XXXX (WPA2)", "     captive portal panel",
                                              "OFF: join saved Wi-Fi, NTP,", "     relay worker, web app /app/"],
    color=AMBER, fs=12.5, tfs=15)
box(52, 37, 45, 26, "RELAY WORKER (FreeRTOS)", ["POST /v1/gateway", "up: ≤16 pkts + 16-bit PoW", "down: long-poll 10 s:",
                                                "  receipts + announced IDs", "carrier mode (quota)",
                                                "TLS: ISRG Root X1/X2", "cursor saved every 5 min"],
    color=CYAN, fs=12.5, tfs=15)
box(5, 37, 44, 14, "FLASH LAYOUT", ["0x0 boot · 0x8000 part · 0xe000 ota", "0x10000 app0 · 0x410000 app1",
                                    "0x810000 LittleFS (web, id, cfg)"], color=LINE, fs=12, tfs=15)

# ======================================================================== 05 handheld
panel(102, 34, 98, 75, 5, "Handheld & keyboard module", "docs/handheld-proto.md · firmware/keyboard")
box(105, 84, 30, 17, "ILI9341 2.4\" TFT", ["240×320 · own SPI 39/40/41", "XPT2046 touch · microSD", "backlight PWM IO45"],
    color=VIOLET, fs=12, tfs=15)
box(137, 84, 30, 17, "WM8960 audio", ["I2S 15–18, 46 · I2C 0x1A", "MCLK from ESP32", "8 Ω speaker · 32 Ω ear"],
    color=VIOLET, fs=12, tfs=15)
box(169, 84, 29, 17, "POWER", ["USB-C → TP4056 → LiPo", "→ switch → TPS63020", "3.3 V 2 A · MAX17048 0x36"],
    color=GREEN, fs=12, tfs=15)
box(105, 59, 42, 22, "KEYBOARD MODULE — Raspberry Pi Pico", ["RP2040 2× Cortex-M0+ 133 MHz", "264 KB SRAM · 2 MB flash",
                                                             "I2C target 0x1F + INT → IO9", "scans matrix, debounces",
                                                             "reads trackpad, backlight"], color=MAGENTA, fs=12, tfs=14)
arrow(126, 81, 126, 84, color=MAGENTA)
box(150, 59, 48, 22, "THREE VERSIONS", ["A  7×7 diode matrix, 32 Persian", "   letters on ISIRI 9147 keys",
                                        "B  BlackBerry 9900 keypad 7×6", "C  9900 keypad + ADBS-A320", "   trackpad (I2C 0x3B, 1.8 V)"],
    color=AMBER, fs=12, tfs=15)
arrow(147, 70, 150, 70, color=AMBER)
box(105, 37, 93, 19, "UI (LVGL 9.2) + TOUCH KEYBOARD", ["Home · Chats · Chat · Network · Web access (QR) · Settings",
                                                       "Vazirmatn 16 px, Persian shaping + RTL (LVGL patched: digits, ۲)",
                                                       "keys: Alt+Shift fa/en · Sym = symbols/arrows · green = call · red = end",
                                                       "calls over Wi-Fi: Opus 16 kHz · video receive-only 240×180"],
    color=CYAN, fs=12.5, tfs=15)

# ======================================================================== 06 LoRa / Meshtastic
panel(202, 34, 96, 75, 6, "LoRa on the Meshtastic backbone", "firmware/lib/meshtastic")
text(205, 99, "ON-AIR FRAME (≤255 B)", 16, MAGENTA, weight="bold")
fr = [("to", 4), ("from", 4), ("id", 4), ("flg", 1), ("ch", 1), ("nh", 1), ("rly", 1)]
x = 205
for name, w in fr:
    w2 = w * 2.2
    ax.add_patch(Rectangle((x, 91), w2, 4.5, fc="#2A1250", ec=MAGENTA, lw=1.5, zorder=4))
    ax.text(x + w2 / 2, 93.25, name, color=FG, fontsize=15, ha="center", va="center", family=MONO, zorder=5)
    x += w2
ax.add_patch(Rectangle((x, 91), 35, 4.5, fc="#13304A", ec=CYAN, lw=1.5, zorder=4))
ax.text(x + 17.5, 93.25, "AES-CTR( Data{port 256, Mava pkt} )", color=FG, fontsize=15, ha="center", va="center",
        family=MONO, zorder=5)
text(205, 89, "16-byte header (LE) · nonce = id ‖ from · channel hash LongFast = 8", 13)
box(205, 63, 44, 22, "NO DOUBLE FLOODING", ["a Mava packet heard from", "Meshtastic is relayed with",
                                           "its original from / id and", "hop_limit − 1: Meshtastic", "nodes drop the copy"],
    color=CYAN, fs=12.5, tfs=15)
box(251, 63, 44, 22, "IMPLICIT ACK & RETRIES", ["own retries keep their id", "a relay of our packet =",
                                                "the router's implicit ACK", "MAC airtime: full frame", "≈ 2.16 s (LongFast)"],
    color=VIOLET, fs=12.5, tfs=15)
box(205, 37, 90, 23, "WHY MESHTASTIC", ["existing meshes repeat Mava frames without reading them (end-to-end encrypted)",
                                        "Mava's own router still decides: dedup, store-and-forward, priorities",
                                        "RECEIPT > priority DATA > DATA > SUMMARY > ANNOUNCE · NO_LORA never airs",
                                        "duty cycle per region (EU 10 % sub-band) enforced by the MAC"],
    color=LINE, fs=12.5, tfs=15)

# ======================================================================== 07 software
panel(2, 2, 118, 30, 7, "Software", "Go core shared by every app · firmware is the second implementation")
apps = [("core/ (Go)", ["identity · e2e · router", "store · relayclient · call"], VIOLET),
        ("relay server", ["HTTP API · federation", "PoW · CORS · release.json"], VIOLET),
        ("meshd + mesh", ["Linux daemon · web UI", "USB LoRa · bridge"], CYAN),
        ("Android", ["Kotlin + gomobile AAR", "BLE mesh · USB LoRa · calls"], CYAN),
        ("iOS", ["SwiftUI · LoRa over BLE", "(no App Store → web)"], CYAN),
        ("web/lite (WASM)", ["browser client 2.9 MB gz", "board link + relays"], MAGENTA)]
for i, (t, ls, c) in enumerate(apps):
    box(4 + i * 19.2, 6, 18.4, 17, t, ls, color=c, fs=9.6, tfs=12)

# ======================================================================== 08 path
panel(122, 2, 90, 30, 8, "A message's path", "iPhone in a blackout → Berlin")
steps = [("iPhone\nbrowser", CYAN), ("Mava\nboard", MAGENTA), ("LoRa\nmesh", MAGENTA), ("gateway\nboard", MAGENTA),
         ("IR relay", VIOLET), ("bridge", AMBER), ("EU relay", VIOLET), ("Sara's\nphone", CYAN)]
for i, (s, c) in enumerate(steps):
    x = 124 + i * 10.9
    ax.add_patch(Polygon(chamfer(x, 10, 9, 10, 0.8), closed=True, fc="#1C1244", ec=c, lw=1.8, zorder=4))
    ax.text(x + 4.5, 15, s, color=FG, fontsize=18, ha="center", va="center", family=SANS, zorder=5)
    if i < len(steps) - 1:
        arrow(x + 9, 15, x + 10.9, 15, color=c, style="-|>", lw=1.6)
text(124, 8.5, "same encrypted packet end to end · receipt returns the same way and purges every copy", 12.5, MUTED)

# ======================================================================== 09 security / release / design
panel(214, 2, 84, 30, 9, "Security · release · design", "docs/threat-model.md · flashing.md · design.md")
text(216, 23.0, "• relays, gateways, boards see only dest hash, id, hops, size", 12.5)
text(216, 19.8, "• board never sees plaintext · web port untrusted (no wipe/config)", 12.5)
text(216, 16.6, "• PoW on anonymous uploads · signed receipts · signed release.json", 12.5)
text(216, 13.4, "• release: APKs · meshd/mesh · relay · board flasher (esptool, UF2)", 12.5)
for i, (c, n) in enumerate([("#0B0A14", "bg"), ("#7C3AED", "primary"), ("#A78BFA", "hi"), ("#D946EF", "accent"),
                            ("#22D3EE", "ok"), ("#ECE8FF", "fg")]):
    ax.add_patch(Rectangle((216 + i * 13, 5.2), 12, 4.6, fc=c, ec=LINE, lw=1.2, zorder=4))
    ax.text(222 + i * 13, 4.2, n, color=MUTED, fontsize=15, ha="center", va="top", family=MONO, zorder=5)

os.makedirs(os.path.dirname(OUT), exist_ok=True)
fig.savefig(OUT + ".png", facecolor=BG)
fig.savefig(OUT + ".pdf", facecolor=BG)
print(OUT + ".png")
print(OUT + ".pdf")

# PlatformIO pre-script: two fixes to LVGL 9.2's Persian text, applied to the
# downloaded library (idempotent; also used by `make uishots`):
#
# 1. lv_bidi.c lays out Arabic-Indic and Persian digits (U+0660–0669,
#    U+06F0–06F9) as right-to-left letters, so "۱۴۰۵" shows as "۵۰۴۱".
#    Unicode's bidi algorithm treats them as numbers: make them weak, not RTL.
# 2. lv_text_ap.c gives ۰ (U+06F0) a "middle form" at +2 — which is ۲
#    (U+06F2) — so every ۲ is drawn as ۰. Digits have no joining forms.
import os
import sys

MARK = "/* mava: Persian digits are weak */"


def patch(path):
    if not os.path.exists(path):
        return False
    src = open(path, encoding="utf-8").read()
    if MARK in src:
        return True
    weak = "static bool lv_bidi_letter_is_weak(uint32_t letter)\n{\n"
    rtl = "static bool lv_bidi_letter_is_rtl(uint32_t letter)\n{\n"
    digits = "    if((letter >= 0x660 && letter <= 0x669) || (letter >= 0x6F0 && letter <= 0x6F9)) return %s; " + MARK + "\n"
    if weak not in src or rtl not in src:
        print("patch_lvgl: lv_bidi.c changed upstream, not patched", file=sys.stderr)
        return False
    src = src.replace(weak, weak + digits % "true", 1).replace(rtl, rtl + digits % "false", 1)
    open(path, "w", encoding="utf-8").write(src)
    print("patch_lvgl: Persian digits are weak in", path)
    return True


AP_BAD = "{206, 0x06F0, -1, 2, 0,  {0, 0}},"
AP_GOOD = "{206, 0x06F0, 0, 0, 0,  {0, 0}}, /* mava: digits do not join */"


def patch_ap(path):
    if not os.path.exists(path):
        return
    src = open(path, encoding="utf-8").read()
    if AP_BAD in src:
        open(path, "w", encoding="utf-8").write(src.replace(AP_BAD, AP_GOOD))
        print("patch_lvgl: ۲ no longer drawn as ۰ in", path)
    elif AP_GOOD not in src:
        print("patch_lvgl: lv_text_ap.c changed upstream, not patched", file=sys.stderr)


def lvgl_paths(root):
    libdeps = os.path.join(root, ".pio", "libdeps")
    if not os.path.isdir(libdeps):
        return []
    return [os.path.join(libdeps, env, "lvgl", "src", "misc", "lv_bidi.c") for env in os.listdir(libdeps)]


try:
    Import("env")  # noqa: F821 (PlatformIO)
    root = env.subst("$PROJECT_DIR")  # noqa: F821
except NameError:
    root = sys.argv[1] if len(sys.argv) > 1 else "."

for p in lvgl_paths(root):
    patch(p)
    patch_ap(p.replace("lv_bidi.c", "lv_text_ap.c"))

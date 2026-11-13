"""Writes lib/rails.kicad_sym: one global power port per rail of this board,
copied from KiCad's own power:VCC so they behave exactly like stock ports.
A power port's net is its name, so each rail needs its own symbol."""
import os, re, sys

RAILS = ["VBAT", "VSYS", "3V3_LORA", "3V3_AUDIO", "3V3_SD", "3V3_LCD", "3V3_GPS",
         "VSPARE", "VRTC"]


def main():
    src = os.path.join(os.environ["KICAD10_SYMBOL_DIR"], "power.kicad_symdir", "VCC.kicad_sym")
    t = open(src, encoding="utf-8").read()
    body = t[t.index('\t(symbol "VCC"'):t.rstrip().rindex(")")].rstrip() + "\n"
    out = ['(kicad_symbol_lib\n\t(version 20251024)\n\t(generator "make_rails")\n'
           '\t(generator_version "10.0")\n']
    for r in RAILS:
        b = body.replace('(symbol "VCC"', f'(symbol "{r}"').replace('"VCC_', f'"{r}_')
        b = b.replace('(property "Value" "VCC"', f'(property "Value" "{r}"')
        b = b.replace('global label with name \\"VCC\\"', f'global label with name \\"{r}\\"')
        b = re.sub(r'\(name "VCC"', f'(name "{r}"', b)
        out.append(b)
    out.append(")\n")
    os.makedirs("lib", exist_ok=True)
    open(os.path.join("lib", "rails.kicad_sym"), "w", encoding="utf-8").write("".join(out))
    print("lib/rails.kicad_sym:", ", ".join(RAILS))


if __name__ == "__main__":
    sys.exit(main())

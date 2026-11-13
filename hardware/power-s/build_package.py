"""Model S power board -- manufacturing package for JLCPCB (PCB + PCBA).

    . ./kicad-env.sh && python3 build_package.py

Writes jlc/:
  power-s-gerbers.zip   Gerbers + Excellon drill, upload as the PCB
  power-s_bom.csv       Comment, Designator, Footprint, LCSC  (JLCPCB BOM format)
  power-s_cpl.csv       Designator, Mid X, Mid Y, Layer, Rotation (jlc_cpl.py)
  power-s.pdf           schematic, for reference

Refuses when DRC has errors or unconnected items: a board that is not finished does not
leave this folder.
"""
import csv
import json
import os
import shutil
import subprocess
import sys
import tempfile
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
NAME = "power-s"
PCB = os.path.join(HERE, NAME + ".kicad_pcb")
SCH = os.path.join(HERE, NAME + ".kicad_sch")
OUT = os.path.join(HERE, "jlc")
CLI = shutil.which("kicad-cli") or "kicad-cli"
LAYERS = "F.Cu,B.Cu,F.Paste,F.Silkscreen,B.Silkscreen,F.Mask,B.Mask,Edge.Cuts"


def run(*args):
    r = subprocess.run([CLI, *args], capture_output=True, text=True)
    if r.returncode:
        sys.exit("kicad-cli %s failed:\n%s%s" % (" ".join(args[:3]), r.stdout, r.stderr))
    return r


def drc_gate():
    with tempfile.TemporaryDirectory() as t:
        rep = os.path.join(t, "drc.json")
        run("pcb", "drc", "--format", "json", "--units", "mm", "--schematic-parity",
            "--severity-error", "-o", rep, PCB)
        d = json.load(open(rep, encoding="utf-8"))
    errors = len(d.get("violations", [])) + len(d.get("schematic_parity", []))
    open_ = len(d.get("unconnected_items", []))
    if errors or open_:
        sys.exit("DRC: %d error(s), %d unconnected -- not building a package" % (errors, open_))
    print("DRC: 0 errors, 0 unconnected, schematic parity clean")


def gerbers():
    with tempfile.TemporaryDirectory() as t:
        run("pcb", "export", "gerbers", "--layers", LAYERS, "--use-drill-file-origin",
            "--no-protel-ext", "-o", t + os.sep, PCB)
        run("pcb", "export", "drill", "--format", "excellon", "--drill-origin", "plot",
            "--excellon-units", "mm", "--excellon-separate-th", "--generate-map",
            "--map-format", "gerberx2", "-o", t + os.sep, PCB)
        z = os.path.join(OUT, NAME + "-gerbers.zip")
        with zipfile.ZipFile(z, "w", zipfile.ZIP_DEFLATED) as zf:
            for f in sorted(os.listdir(t)):
                zf.write(os.path.join(t, f), f)
        print("wrote jlc/%s (%d files)" % (os.path.basename(z), len(os.listdir(t))))


def bom():
    with tempfile.TemporaryDirectory() as t:
        raw = os.path.join(t, "bom.csv")
        run("sch", "export", "bom", "--fields", "Reference,Value,Footprint,LCSC,${DNP}",
            "--group-by", "", "-o", raw, SCH)
        rows = list(csv.DictReader(open(raw, encoding="utf-8")))
    groups = {}
    for r in rows:
        if r.get("DNP") or r.get("${DNP}") or not r["LCSC"]:
            continue                         # not fitted, or not a part (test pads)
        key = (r["Value"], r["Footprint"].split(":")[-1], r["LCSC"])
        groups.setdefault(key, []).append(r["Reference"])
    path = os.path.join(OUT, NAME + "_bom.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["Comment", "Designator", "Footprint", "LCSC"])
        for (val, fp, lcsc), refs in sorted(groups.items(), key=lambda kv: kv[1][0]):
            w.writerow([val, ",".join(refs), fp, lcsc])
    print("wrote jlc/%s (%d lines, %d parts)" % (os.path.basename(path), len(groups),
                                                 sum(len(v) for v in groups.values())))
    return {ref for refs in groups.values() for ref in refs}


def cpl(bom_refs):
    r = subprocess.run([sys.executable, os.path.join(HERE, "jlc_cpl.py"), PCB, "-o",
                        os.path.join(OUT, NAME + "_cpl.csv")], capture_output=True, text=True)
    print(r.stdout.strip())
    if r.returncode:
        sys.exit(r.stderr)
    rows = list(csv.DictReader(open(os.path.join(OUT, NAME + "_cpl.csv"), encoding="utf-8")))
    cpl_refs = {row["Designator"] for row in rows}
    if cpl_refs != bom_refs:
        sys.exit("BOM and CPL differ: only BOM %s, only CPL %s"
                 % (sorted(bom_refs - cpl_refs), sorted(cpl_refs - bom_refs)))
    print("BOM and CPL list the same %d parts" % len(cpl_refs))


def main():
    os.makedirs(OUT, exist_ok=True)
    drc_gate()
    gerbers()
    refs = bom()
    cpl(refs)
    run("sch", "export", "pdf", "-o", os.path.join(OUT, NAME + ".pdf"), SCH)
    print("wrote jlc/%s.pdf" % NAME)


if __name__ == "__main__":
    main()

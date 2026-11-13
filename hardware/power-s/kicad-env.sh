#!/usr/bin/env bash
# KiCad 10 from the AppImage, without installing it. Source this file:
#   . ./kicad-env.sh            then: kpy generate_schematic.py   (KiCad's Python, has pcbnew)
#                                     kicad-cli ...
# KICAD_APPIMAGE overrides the AppImage path. The AppImage stays mounted in the
# background (one `--appimage-mount` process) so every call reuses the same path.
KICAD_APPIMAGE=${KICAD_APPIMAGE:-$HOME/Applications/kicad-10.0.8-x86_64.AppImage}

_kicad_mount() {
  local m
  m=$(ls -d /tmp/.mount_kicad*/ 2>/dev/null | head -1)
  if [ -z "$m" ] || [ ! -x "$m/bin/kicad-cli" ]; then
    local log; log=$(mktemp)
    nohup "$KICAD_APPIMAGE" --appimage-mount >"$log" 2>&1 &
    for _ in $(seq 1 50); do m=$(head -1 "$log" 2>/dev/null); [ -n "$m" ] && [ -x "$m/bin/kicad-cli" ] && break; sleep 0.2; done
  fi
  echo "${m%/}"
}

KICAD_ROOT=$(_kicad_mount)
[ -x "$KICAD_ROOT/bin/kicad-cli" ] || { echo "kicad-env: could not mount $KICAD_APPIMAGE" >&2; return 1 2>/dev/null || exit 1; }
export KICAD_ROOT
export KICAD10_SYMBOL_DIR=$KICAD_ROOT/usr/share/kicad/symbols
export KICAD10_FOOTPRINT_DIR=$KICAD_ROOT/usr/share/kicad/footprints
export KICAD10_3DMODEL_DIR=$KICAD_ROOT/usr/share/kicad/3dmodels
export KICAD10_TEMPLATE_DIR=$KICAD_ROOT/usr/share/kicad/template

# kicad-cli as a real file, so tools that look up a path (shutil.which) find it.
_kbin=${XDG_RUNTIME_DIR:-/tmp}/kicad10-bin
mkdir -p "$_kbin"
printf '#!/bin/sh\nexec "%s/sharun" kicad-cli "$@"\n' "$KICAD_ROOT" > "$_kbin/kicad-cli"
printf '#!/bin/sh\nexec "%s/sharun" python3.11 "$@"\n' "$KICAD_ROOT" > "$_kbin/kpy"
chmod +x "$_kbin/kicad-cli" "$_kbin/kpy"
export KICAD_CLI=$_kbin/kicad-cli
case ":$PATH:" in *":$_kbin:"*) ;; *) export PATH=$_kbin:$PATH ;; esac

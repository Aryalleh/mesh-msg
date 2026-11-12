#!/usr/bin/env bash
# Installs meshd + mesh from a release tarball for the current user and runs
# meshd as a systemd user service. Re-run with a newer release to update.
set -euo pipefail
cd "$(dirname "$0")"
bin=${BIN:-$HOME/.local/bin}
mkdir -p "$bin" "$HOME/.config/systemd/user"
install -m 0755 meshd mesh "$bin/"
install -m 0644 meshd.service "$HOME/.config/systemd/user/meshd.service"
echo "installed $("$bin/meshd" -version) to $bin"
if command -v systemctl >/dev/null && systemctl --user show-environment >/dev/null 2>&1; then
  systemctl --user daemon-reload
  systemctl --user enable meshd >/dev/null 2>&1
  systemctl --user restart meshd
  echo "meshd is running as a user service (systemctl --user status meshd)"
  echo "to keep it running after logout: sudo loginctl enable-linger $USER"
else
  echo "no systemd user session: start it yourself with: $bin/meshd &"
fi
case ":$PATH:" in *":$bin:"*) ;; *) echo "note: add $bin to your PATH to type 'mesh' instead of '$bin/mesh'";; esac

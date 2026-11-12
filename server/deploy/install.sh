#!/bin/sh
# Installs or updates a Mava relay (with its admin panel at /admin).
#
#   curl -fsSL https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/server/deploy/install.sh | sudo sh
#   sudo sh install.sh            (from a release tarball: uses the meshmsg-relay next to it)
#
# It asks for the relay's name and region and for YOUR Mava account id: that
# account gets one-time login codes for the admin panel, sent as Mava
# messages. (The network owner's account is always an admin too.)
# Non-interactive: set MAVA_ADMIN_ID, MAVA_NAME, MAVA_REGION, MAVA_URL,
# MAVA_MEDIA_MB (0 = no media), MAVA_FIREWALL (y/n) and run with MAVA_YES=1.
#
# Mava relay ports (the same on every relay, docs/server.md §6d):
#   TCP 80, 443         HTTP(S) API, admin panel
#   UDP 3478, 3479      address reflector (direct links); TCP+UDP 3478 TURN
#   UDP 61000-61999     TURN relayed addresses (only used when TURN is on)
set -eu

REPO=Aryalleh/mesh-msg
BIN=/usr/local/bin/meshmsg-relay
CONF_DIR=/etc/meshmsg
CONF=$CONF_DIR/relay.json
DATA=/var/lib/meshmsg-relay
UNIT=/etc/systemd/system/meshmsg-relay.service

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root (sudo sh install.sh)"
[ "$(uname -s)" = Linux ] || die "Linux only"
command -v systemctl >/dev/null || die "needs systemd"

# Questions come from the terminal even when the script is piped into sh.
ask() { # ask VAR "question" default
	eval "cur=\${$1:-}"
	if [ -n "$cur" ] || [ "${MAVA_YES:-}" = 1 ]; then
		[ -n "$cur" ] || eval "$1=\$3"
		return
	fi
	if [ -r /dev/tty ]; then
		printf '%s [%s]: ' "$2" "$3" > /dev/tty
		read -r ans < /dev/tty || ans=""
	else
		ans=""
	fi
	[ -n "$ans" ] || ans=$3
	eval "$1=\$ans"
}

case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	*) die "unsupported CPU $(uname -m) (amd64 and arm64 are built)" ;;
esac

# 1. The binary: next to this script (release tarball), else the newest release.
HERE=$(cd "$(dirname "$0")" 2>/dev/null && pwd || echo /nonexistent)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
if [ -x "$HERE/meshmsg-relay" ]; then
	cp "$HERE/meshmsg-relay" "$TMP/meshmsg-relay"
	[ -f "$HERE/meshmsg-relay.service" ] && cp "$HERE/meshmsg-relay.service" "$TMP/"
else
	command -v curl >/dev/null || die "needs curl"
	say "Looking up the newest release…"
	URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=10" \
		| grep -o "\"browser_download_url\": *\"[^\"]*meshmsg-relay-[^\"]*-linux-$ARCH\.tar\.gz\"" \
		| head -n 1 | sed 's/.*"\(https[^"]*\)"/\1/')
	[ -n "$URL" ] || die "no relay build for linux-$ARCH in the releases"
	say "Downloading $URL"
	curl -fsSL "$URL" -o "$TMP/relay.tar.gz"
	SUMS=$(dirname "$URL")/SHA256SUMS
	if curl -fsSL "$SUMS" -o "$TMP/SHA256SUMS" 2>/dev/null; then
		want=$(grep " $(basename "$URL")\$" "$TMP/SHA256SUMS" | cut -d' ' -f1)
		got=$(sha256sum "$TMP/relay.tar.gz" | cut -d' ' -f1)
		[ -z "$want" ] || [ "$want" = "$got" ] || die "checksum mismatch for $(basename "$URL")"
	fi
	tar xzf "$TMP/relay.tar.gz" -C "$TMP"
	d=$(find "$TMP" -maxdepth 2 -name meshmsg-relay -type f | head -n 1)
	[ -n "$d" ] || die "archive has no meshmsg-relay"
	cp "$d" "$TMP/meshmsg-relay"
	[ -f "$(dirname "$d")/meshmsg-relay.service" ] && cp "$(dirname "$d")/meshmsg-relay.service" "$TMP/"
fi
chmod 755 "$TMP/meshmsg-relay"
NEWVER=$("$TMP/meshmsg-relay" -version 2>/dev/null || echo "?")

# 2. Config: new install asks; an existing config is kept.
if [ -f "$CONF" ]; then
	say "Keeping $CONF (updating the binary only)."
	FRESH=0
else
	FRESH=1
	ask MAVA_NAME "Relay name (short, e.g. eu-2)" "$(hostname -s 2>/dev/null || echo relay)"
	ask MAVA_REGION "Region (country code like ir, or global)" global
	IP=$(curl -fsS -4 --max-time 5 https://api.ipify.org 2>/dev/null || echo "")
	ask MAVA_URL "Public URL clients use" "${IP:+http://$IP}"
	ask MAVA_MEDIA_MB "Largest media file in MB (0 = no photos/voice/video on this relay)" 16
	case "$MAVA_MEDIA_MB" in ''|*[!0-9]*) die "media size must be a number" ;; esac
	mkdir -p "$CONF_DIR"
	TOKEN=$("$TMP/meshmsg-relay" -gen-token)
	URLS=""
	[ -n "$MAVA_URL" ] && URLS="\"$MAVA_URL\""
	cat > "$CONF" <<JSON
{
  "name": "$MAVA_NAME",
  "region": "$MAVA_REGION",
  "listen": "127.0.0.1:8080",
  "data_dir": "$DATA",
  "pow_bits": 16,
  "ttl_hours": 168,
  "sync_tokens": ["$TOKEN"],
  "peers": [],
  "relays": [
    { "name": "$MAVA_NAME", "region": "$MAVA_REGION", "urls": [$URLS] }
  ],
  "media_max_mb": $MAVA_MEDIA_MB,
  "media_store_mb": 2048,
  "media_important_per_day": 5,
  "media_important_hours": 168,
  "media_accounts": [],
  "log": false
}
JSON
	chmod 640 "$CONF"
	say "Wrote $CONF (sync token for peers/bridges: $TOKEN)"
fi

# 3. Your admin account (codes for the panel go to it in Mava).
say ""
say "Admin panel login: enter your Mava account id (Mava → Me / 'mesh id'). Codes to log in"
say "are sent to that account. Leave empty to skip; change later with: meshmsg-relay -config $CONF admin add <id>"
ask MAVA_ADMIN_ID "Your account id" ""
if [ -n "$MAVA_ADMIN_ID" ]; then
	echo "$MAVA_ADMIN_ID" | grep -Eqi '^[0-9a-f]{32}$' || die "account id must be 32 hex characters"
fi

# 4. Install and (re)start.
systemctl stop meshmsg-relay 2>/dev/null || true
[ -f "$BIN" ] && cp "$BIN" "$BIN.prev"
install -m 755 "$TMP/meshmsg-relay" "$BIN"
if [ ! -f "$UNIT" ]; then
	if [ -f "$TMP/meshmsg-relay.service" ]; then
		cp "$TMP/meshmsg-relay.service" "$UNIT"
	else
		curl -fsSL "https://raw.githubusercontent.com/$REPO/main/server/deploy/meshmsg-relay.service" -o "$UNIT"
	fi
	systemctl daemon-reload
	systemctl enable meshmsg-relay >/dev/null 2>&1
fi
# The data dir belongs to the service: create it once through systemd.
if [ ! -d "$DATA" ]; then
	systemctl start meshmsg-relay && sleep 2 && systemctl stop meshmsg-relay
fi
if [ -n "$MAVA_ADMIN_ID" ]; then
	"$BIN" -config "$CONF" admin add "$MAVA_ADMIN_ID" >/dev/null
fi
systemctl start meshmsg-relay
sleep 2
systemctl is-active --quiet meshmsg-relay || { journalctl -u meshmsg-relay -n 20 --no-pager; die "the relay did not start"; }

# Firewall: open the Mava relay ports if ufw or firewalld is active. It only
# adds rules; it never turns a firewall on (that could lock out SSH).
PORTS_TCP="80 443 3478"
PORTS_UDP="3478 3479 61000-61999"
ask MAVA_FIREWALL "Open the Mava relay ports in the firewall (TCP 80,443,3478; UDP 3478,3479,61000-61999)? y/n" y
case "$MAVA_FIREWALL" in
y* | Y*)
	if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
		for p in $PORTS_TCP; do ufw allow "$p/tcp" >/dev/null; done
		for p in $PORTS_UDP; do ufw allow "$(echo "$p" | tr - :)/udp" >/dev/null; done
		say "Opened the Mava ports in ufw."
	elif command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; then
		for p in $PORTS_TCP; do firewall-cmd -q --permanent --add-port="$p/tcp"; done
		for p in $PORTS_UDP; do firewall-cmd -q --permanent --add-port="$p/udp"; done
		firewall-cmd -q --reload
		say "Opened the Mava ports in firewalld."
	else
		say "No active ufw/firewalld found: nothing changed here."
	fi
	say "If your provider has a cloud firewall, open the same ports there."
	;;
esac

say ""
say "Mava relay $NEWVER is running (127.0.0.1:8080)."
say "Admins:"
"$BIN" -config "$CONF" admin list | sed 's/^/  /'
say ""
say "Admin panel: <your relay URL>/admin — enter your account id, then the code Mava receives."
say "The relay must know your account first: add this relay in Mava (Settings → Relays) and open the app."
if [ "$FRESH" = 1 ]; then
	say ""
	say "Next: put a web server in front for HTTPS (server/deploy/Caddyfile), or for a quick test"
	say "set \"listen\": \"0.0.0.0:80\" in $CONF and: systemctl restart meshmsg-relay"
fi

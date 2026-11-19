#!/usr/bin/env bash
# Mava relay installer: one command for the three kinds of relay
# (docs/edge-relay.md):
#
#   1) this server        Linux + systemd, the full relay (runs install.sh)
#   2) Cloudflare Workers  an edge relay on Cloudflare (D1 database), no server
#   3) ArvanCloud Edge     an edge relay on Arvan's edge (Object Storage bucket), no server
#
#   curl -fsSL https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/server/deploy/install-relay.sh | bash
#
# Every question can be answered ahead with an environment variable
# (MAVA_TARGET=local|cloudflare|arvan, MAVA_NAME, MAVA_REGION, MAVA_ADMIN_ID,
# MAVA_PEER_URL, MAVA_PEER_TOKEN, CLOUDFLARE_API_TOKEN, ARVAN_API_KEY,
# MAVA_ARVAN_APP, MAVA_S3_ENDPOINT, MAVA_S3_REGION, MAVA_S3_BUCKET,
# MAVA_S3_ACCESS_KEY, MAVA_S3_SECRET_KEY); MAVA_YES=1 takes the defaults.
set -euo pipefail

REPO=Aryalleh/mesh-msg
REF=${MAVA_REF:-main}
RAW="https://raw.githubusercontent.com/$REPO/$REF"
ARVAN_CLI_VERSION=${ARVAN_CLI_VERSION:-0.4.0}

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# Questions come from the terminal even when the script is piped into bash.
ask() { # ask VAR "question" default [secret]
	local var=$1 q=$2 def=${3:-} secret=${4:-} cur ans=""
	cur=${!var:-}
	if [ -n "$cur" ] || [ "${MAVA_YES:-}" = 1 ]; then
		printf -v "$var" '%s' "${cur:-$def}"
		return
	fi
	if (: </dev/tty) 2>/dev/null; then
		if [ -n "$secret" ]; then
			printf '%s: ' "$q" >/dev/tty
			IFS= read -rs ans </dev/tty || ans=""
			printf '\n' >/dev/tty
		else
			printf '%s [%s]: ' "$q" "$def" >/dev/tty
			IFS= read -r ans </dev/tty || ans=""
		fi
	fi
	printf -v "$var" '%s' "${ans:-$def}"
}

need() { command -v "$1" >/dev/null 2>&1 || die "$2"; }

random_token() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# The edge relay bundle: from this checkout, else from GitHub.
edge_bundle() { # edge_bundle cloudflare|arvan DEST
	local here
	here=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo /nonexistent)
	if [ -f "$here/../../edge/dist/$1.js" ]; then
		cp "$here/../../edge/dist/$1.js" "$2"
	else
		curl -fsSL "${MAVA_EDGE_URL:-$RAW/edge/dist}/$1.js" -o "$2" || die "could not download the edge relay ($1.js)"
	fi
}

say "Mava relay installer"
say ""
say "Where should the relay run?"
say "  1) this server (Linux with systemd): the full relay, with calls' UDP services and media"
say "  2) Cloudflare Workers: no server; messages, trips, admin panel, federation (no media, no UDP)"
say "  3) ArvanCloud Edge Computing: like 2, on Arvan's edge, with an Arvan Object Storage bucket"
ask MAVA_TARGET "Choose 1, 2 or 3" 1
case "$MAVA_TARGET" in
	1 | local) MAVA_TARGET=local ;;
	2 | cloudflare | cf) MAVA_TARGET=cloudflare ;;
	3 | arvan | r1 | ar) MAVA_TARGET=arvan ;;
	*) die "choose 1, 2 or 3" ;;
esac

# ---- 1) this server: the existing installer ----
if [ "$MAVA_TARGET" = local ]; then
	here=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo /nonexistent)
	script="$here/install.sh"
	if [ ! -f "$script" ]; then
		script=$(mktemp)
		curl -fsSL "$RAW/server/deploy/install.sh" -o "$script" || die "could not download install.sh"
	fi
	if [ "$(id -u)" = 0 ]; then exec sh "$script"; fi
	need sudo "run as root (the server install needs it)"
	exec sudo -E sh "$script"
fi

# ---- edge relays: shared questions ----
need curl "needs curl"
ask MAVA_NAME "Relay name (short, letters, digits and -, e.g. trip-ir)" "mava-edge"
MAVA_NAME=$(printf '%s' "$MAVA_NAME" | tr 'A-Z' 'a-z' | tr -c 'a-z0-9-' '-' | sed 's/^-*//; s/-*$//')
[ -n "$MAVA_NAME" ] || die "the relay needs a name"
ask MAVA_REGION "Region (country code like ir, or global)" global
say ""
say "Admin panel login: your Mava account id (Mava → Me). Codes to log in are sent to it."
ask MAVA_ADMIN_ID "Your account id (empty: none for now)" ""
if [ -n "$MAVA_ADMIN_ID" ]; then
	printf '%s' "$MAVA_ADMIN_ID" | grep -Eqi '^[0-9a-f]{32}$' || die "an account id is 32 hex characters"
fi
say ""
say "Federation: a relay this one exchanges messages with (another Mava relay's URL and its"
say "sync token from its relay.json). Empty: none (add later by running this again)."
ask MAVA_PEER_URL "Peer relay URL" ""
PEERS="[]"
if [ -n "$MAVA_PEER_URL" ]; then
	ask MAVA_PEER_TOKEN "Its sync token (empty: we only pull from it; let it pull from us)" "" secret
	peer_name=$(printf '%s' "$MAVA_PEER_URL" | sed 's#^[a-z]*://##; s#[/:].*##')
	PEERS="[{\"name\":\"$peer_name\",\"url\":\"$MAVA_PEER_URL\",\"token\":\"${MAVA_PEER_TOKEN:-}\"}]"
fi
SYNC_TOKEN=$(random_token)
WORK=${MAVA_WORKDIR:-$HOME/mava-relay-$MAVA_NAME}
mkdir -p "$WORK"
chmod 700 "$WORK"

finish() { # finish URL
	local url=$1
	say ""
	if [ -n "$url" ] && curl -fsS --max-time 20 "$url/v1/info" >"$WORK/info.json" 2>/dev/null; then
		say "The relay answers: $(cat "$WORK/info.json")"
	else
		say "Could not reach the relay yet; try $url/v1/info in a minute."
	fi
	say ""
	say "Done. Your relay: $url"
	say "  • In Mava: Settings → Relays → add $url"
	say "  • Admin panel: $url/admin (log in with the code sent to your account)"
	say "  • Other relays can sync with it using this token: $SYNC_TOKEN"
	say "    (kept in $WORK/sync-token)"
	printf '%s\n' "$SYNC_TOKEN" >"$WORK/sync-token"
	chmod 600 "$WORK/sync-token"
}

# ---- 2) Cloudflare Workers ----
if [ "$MAVA_TARGET" = cloudflare ]; then
	need node "Cloudflare needs Node.js 18+ (https://nodejs.org) for its tool, wrangler"
	need npx "Cloudflare needs npx (comes with Node.js)"
	say ""
	say "Cloudflare login: an API token with 'Edit Cloudflare Workers' and 'D1 Edit' rights"
	say "(dash.cloudflare.com → My Profile → API Tokens). Empty: log in with the browser."
	ask CLOUDFLARE_API_TOKEN "API token" "" secret
	export CLOUDFLARE_API_TOKEN
	WR="npx --yes wrangler@4"
	cd "$WORK"
	edge_bundle cloudflare "$WORK/cloudflare.js"
	if [ -z "$CLOUDFLARE_API_TOKEN" ]; then
		$WR whoami >/dev/null 2>&1 || $WR login
	fi
	DB_NAME="mava-relay-$MAVA_NAME"
	say "Creating the D1 database $DB_NAME…"
	DB_ID=$($WR d1 list --json 2>/dev/null | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const l=JSON.parse(s);const d=l.find(x=>x.name===process.argv[1]);if(d)console.log(d.uuid)}catch(e){}})' "$DB_NAME" || true)
	if [ -z "$DB_ID" ]; then
		out=$($WR d1 create "$DB_NAME" 2>&1) || { printf '%s\n' "$out"; die "could not create the D1 database"; }
		DB_ID=$(printf '%s' "$out" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -n 1)
	fi
	[ -n "$DB_ID" ] || die "could not find the D1 database id"
	cat >"$WORK/wrangler.toml" <<TOML
name = "mava-relay-$MAVA_NAME"
main = "cloudflare.js"
compatibility_date = "2025-06-01"
workers_dev = true

[[d1_databases]]
binding = "DB"
database_name = "$DB_NAME"
database_id = "$DB_ID"

# Expiry and federation every minute.
[triggers]
crons = ["* * * * *"]

[vars]
RELAY_NAME = "$MAVA_NAME"
RELAY_REGION = "$MAVA_REGION"
ADMINS = "$MAVA_ADMIN_ID"
POW_BITS = "16"
TTL_HOURS = "168"
TOML
	say "Deploying…"
	out=$($WR deploy 2>&1) || { printf '%s\n' "$out"; die "deploy failed"; }
	printf '%s\n' "$out" | tail -n 5
	URL=$(printf '%s' "$out" | grep -oE 'https://[a-zA-Z0-9.-]+\.workers\.dev' | head -n 1)
	printf '%s' "$SYNC_TOKEN" | $WR secret put SYNC_TOKENS >/dev/null
	printf '%s' "$PEERS" | $WR secret put PEERS >/dev/null
	finish "${URL:-https://mava-relay-$MAVA_NAME.workers.dev}"
	say "  • Files (redeploy with: cd $WORK && $WR deploy): $WORK"
	exit 0
fi

# ---- 3) ArvanCloud Edge Computing ----
if [ "$MAVA_TARGET" = arvan ]; then
	say ""
	say "ArvanCloud: an API key (panel → Profile → API Keys, a machine user) and an Edge"
	say "Computing application (panel → Edge Computing → create one; its name goes here)."
	ask ARVAN_API_KEY "Arvan API key" "" secret
	[ -n "$ARVAN_API_KEY" ] || die "the Arvan API key is needed"
	ask MAVA_ARVAN_APP "Edge Computing application name" "$MAVA_NAME"
	say ""
	say "Storage: an Arvan Object Storage bucket (panel → Object Storage → create a private bucket,"
	say "and an access key). Messages wait there for their recipients."
	ask MAVA_S3_ENDPOINT "Object Storage endpoint" "https://s3.ir-thr-at1.arvanstorage.ir"
	ask MAVA_S3_REGION "Its region" "ir-thr-at1"
	ask MAVA_S3_BUCKET "Bucket name" "mava-relay-$MAVA_NAME"
	ask MAVA_S3_ACCESS_KEY "Access key" ""
	ask MAVA_S3_SECRET_KEY "Secret key" "" secret
	[ -n "$MAVA_S3_ACCESS_KEY" ] && [ -n "$MAVA_S3_SECRET_KEY" ] || die "the bucket's access and secret keys are needed"

	ARVAN=$(command -v arvan || true)
	if [ -z "$ARVAN" ]; then
		case "$(uname -s)-$(uname -m)" in
			Linux-x86_64 | Linux-amd64) plat=linux-amd64 ;;
			Linux-aarch64 | Linux-arm64) plat=linux-arm64 ;;
			Darwin-arm64) plat=darwin-arm64 ;;
			Darwin-x86_64) plat=darwin-amd64 ;;
			*) die "install Arvan's CLI (arvan) yourself for $(uname -s) $(uname -m), then run this again" ;;
		esac
		base="https://git.arvancloud.ir/arvancloud/cli/-/releases/v$ARVAN_CLI_VERSION/downloads"
		archive="arvan-cli-$ARVAN_CLI_VERSION-$plat.tar.gz"
		say "Downloading Arvan's CLI ($archive)…"
		curl -fsSL "$base/$archive" -o "$WORK/$archive" || die "could not download Arvan's CLI from git.arvancloud.ir"
		if curl -fsSL "$base/arvan-cli-$ARVAN_CLI_VERSION-checksums.txt" -o "$WORK/arvan-checksums.txt" 2>/dev/null; then
			want=$(grep " $archive\$" "$WORK/arvan-checksums.txt" | cut -d' ' -f1)
			got=$( (sha256sum "$WORK/$archive" 2>/dev/null || shasum -a 256 "$WORK/$archive") | cut -d' ' -f1)
			[ -z "$want" ] || [ "$want" = "$got" ] || die "checksum mismatch for $archive"
		fi
		tar xzf "$WORK/$archive" -C "$WORK"
		ARVAN=$(find "$WORK" -type f -name arvan | head -n 1)
		[ -n "$ARVAN" ] || die "the archive has no arvan binary"
		chmod 755 "$ARVAN"
	fi

	# The bundle with this relay's settings written in (Arvan's edge has no
	# variables for us to set from here).
	esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
	CONFIG="{\"RELAY_NAME\":\"$(esc "$MAVA_NAME")\",\"RELAY_REGION\":\"$(esc "$MAVA_REGION")\",\"ADMINS\":\"$(esc "$MAVA_ADMIN_ID")\",\"POW_BITS\":16,\"TTL_HOURS\":168,\"MAX_WAIT\":20,\"POLL_MS\":3000,\"SYNC_TOKENS\":\"$SYNC_TOKEN\",\"PEERS\":$PEERS,\"S3\":{\"endpoint\":\"$(esc "$MAVA_S3_ENDPOINT")\",\"region\":\"$(esc "$MAVA_S3_REGION")\",\"bucket\":\"$(esc "$MAVA_S3_BUCKET")\",\"accessKey\":\"$(esc "$MAVA_S3_ACCESS_KEY")\",\"secretKey\":\"$(esc "$MAVA_S3_SECRET_KEY")\"}}"
	edge_bundle arvan "$WORK/arvan-template.js"
	CONFIG="$CONFIG" node -e 'const fs=require("fs");const s=fs.readFileSync(process.argv[1],"utf8");fs.writeFileSync(process.argv[2],s.replace(/\/\*MAVA_CONFIG\*\/[\s\S]*?\/\*END_MAVA_CONFIG\*\//,process.env.CONFIG))' "$WORK/arvan-template.js" "$WORK/main.js" 2>/dev/null ||
		python3 -c 'import os,re,sys; s=open(sys.argv[1]).read(); open(sys.argv[2],"w").write(re.sub(r"/\*MAVA_CONFIG\*/[\s\S]*?/\*END_MAVA_CONFIG\*/", lambda m: os.environ["CONFIG"], s, count=1))' "$WORK/arvan-template.js" "$WORK/main.js" ||
		die "needs node or python3 to write the relay's settings into the bundle"
	chmod 600 "$WORK/main.js"
	say "Deploying to Arvan Edge Computing ($MAVA_ARVAN_APP)…"
	if ! ARVAN_API_KEY="$ARVAN_API_KEY" "$ARVAN" ec deploy --file "$WORK/main.js" "$MAVA_ARVAN_APP"; then
		say ""
		say "The deploy failed. Check that the application \"$MAVA_ARVAN_APP\" exists in the Arvan panel"
		say "(Edge Computing) and that the API key may use it. You can also upload $WORK/main.js there by hand."
		exit 1
	fi
	ask MAVA_URL "The application's address (shown in the Arvan panel, https://…)" ""
	finish "${MAVA_URL:-}"
	say "  • The bundle with your settings (keys inside, keep it private): $WORK/main.js"
	exit 0
fi

# Deploying a relay

## Quick install (Linux amd64/arm64, systemd)

```bash
curl -fsSL https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/server/deploy/install.sh | sudo sh
```

or download `meshmsg-relay-<version>-linux-<arch>.tar.gz` from the releases and run
`sudo sh install.sh` inside. It asks for the relay's name, region, public URL, media size
and **your Mava account id** (Mava → Me, or `mesh id`). Running it again updates the binary
and keeps the config.

## Ports (same on every relay)

| Port | Protocol | Use |
|---|---|---|
| 80, 443 | TCP | API and admin panel (HTTP, HTTPS) |
| 3478, 3479 | UDP | address reflector for direct links (coming) |
| 3478 | TCP + UDP | TURN for the relay's admins (coming, off by default) |
| 61000–61999 | UDP | TURN relayed addresses (only with TURN on) |

`install.sh` opens them in ufw or firewalld if one is active (it never turns a firewall
on); open the same in your provider's cloud firewall. Details: docs/server.md §6d.

## Admin panel

Every relay has a panel at `/admin`. Log in with your account id: the relay sends a
one-time code (8 digits, 10 minutes, 5 tries) to that account **as a Mava message**, so only
the person holding the account can log in. The relay must know the account's keys, which it
learns when the account's Mava app uses this relay. In the panel: traffic counters, media
storage and files (delete), settings (proof-of-work, keep times, media limits, important
files per account per day, media upload accounts, logs; saved in `data_dir/panel.json`,
applied on restart, except the daily allowance and keep time, which apply at once),
admins, restart.

Relays carry only files the sender marked **important** (they reach someone who is
offline): each account may upload `media_important_per_day` of them per UTC day (default 5;
one file to a group counts once), kept until every reader has it or for
`media_important_hours` (default 168). Normal files go only phone to phone.

The network owner's account `ad2107c8e944bf2a99b3f0cdb2c40903` is an admin of every relay
built from this code and can't be removed. Other admins:

```bash
meshmsg-relay -config /etc/meshmsg/relay.json admin list
meshmsg-relay -config /etc/meshmsg/relay.json admin add <account id>
meshmsg-relay -config /etc/meshmsg/relay.json admin rm <account id>
```

Serve the relay over HTTPS (Caddy below): on plain HTTP the code and the session cookie
cross the network unencrypted.

## By hand

```bash
# build (any Linux, no cgo needed)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o meshmsg-relay ./server/cmd/relay
sudo install meshmsg-relay /usr/local/bin/

# config
sudo mkdir -p /etc/meshmsg
sudo cp server/deploy/relay.example.json /etc/meshmsg/relay.json
meshmsg-relay -gen-token          # token peers/bridges use to sync with you
sudo $EDITOR /etc/meshmsg/relay.json   # name, region, peers, tokens, data_dir=/var/lib/meshmsg-relay

# service
sudo cp server/deploy/meshmsg-relay.service /etc/systemd/system/
sudo systemctl enable --now meshmsg-relay

# TLS: install Caddy and use server/deploy/Caddyfile
# Onion service: append server/deploy/torrc.example to /etc/tor/torrc
```

## Regional relay (inside a censoring country)

- Set `"region"` to the country code (e.g. `"ir"`).
- Assume it can be seized: it stores only ciphertext, and request logging is
  off by default (`"log": false`). Keep `ttl_hours` short.
- Use a boring domain and plain HTTPS. Run several on different providers.
- Bridges connect it to global relays — give trusted bridge operators a sync
  token; untrusted bridges can sync with proof-of-work only.

## Federation

Each entry in `peers` is synced every `every_secs` (default 15 s) in both
directions. Configure a peer on **one** side only (or both — duplicates are
dropped, it just costs bandwidth). Cursors are persisted in `data_dir`.

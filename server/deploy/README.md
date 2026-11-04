# Deploying a relay

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

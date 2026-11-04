# mesh-msg desktop (Linux first)

`meshd` runs in the background (client, gateway, bridge, LAN mesh); `mesh` is
the command-line client; a web UI is served on `127.0.0.1` only.

## Install

```bash
go build -o ~/.local/bin/ ./desktop/cmd/meshd ./desktop/cmd/mesh
cp desktop/deploy/meshd.service ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now meshd
```

First start creates `~/.config/meshd/config.toml` (commented) and your
identity in `~/.local/share/meshd/`. Edit the config to add relays, then
`systemctl --user restart meshd`.

## Use

```bash
mesh id                          # your contact link — share it
mesh add 'meshmsg:…' Sara        # add a contact
mesh send Sara "سلام"            # send
mesh read Sara                   # history (✓ sent, ✓✓ delivered)
mesh watch                       # live incoming messages
mesh status                      # links, peers, stored packets
mesh ui                          # open the web UI
```

## Roles

| Role | Config |
|---|---|
| client | one or more `[[relay]]` entries |
| gateway | `[gateway] enabled = true`, `mode = "targeted"` or `"carrier"` |
| bridge | two relays + `[[bridge]] a = "ir-1"  b = "eu-1"`; give the far relay a `via` proxy (VPN / Tor / Starlink router) |
| LAN mesh | `[lan] enabled = true` (multicast discovery, or `static_peers`) |

Example bridge PC with a VPN client exposing SOCKS5 on port 1080:

```toml
[[relay]]
name = "ir-1"
url  = "https://relay-ir.example"
via  = "direct"                    # domestic network

[[relay]]
name = "eu-1"
url  = "https://relay-eu.example"
via  = "socks5://127.0.0.1:1080"   # through the VPN

[[bridge]]
a = "ir-1"
b = "eu-1"
budget_mb_per_day = 50
```

> ⚠️ Running a gateway or bridge can be risky where this traffic is
> monitored. See [docs/threat-model.md](../docs/threat-model.md).

## Security notes

- The control socket is `0600` in your data directory.
- The web UI listens on loopback only, requires a token (via `mesh ui`),
  checks the Host header and requires a custom header on writes.
- `identity.key` is your secret key — back it up privately, never share it.

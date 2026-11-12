# mesh-msg desktop (Linux first)

`meshd` runs in the background (client, gateway, bridge, LAN mesh); `mesh` is
the command-line client; a web UI is served on `127.0.0.1` only.

## Install from a release

Download `meshmsg-desktop-<version>-linux-amd64.tar.gz` (or `arm64`) from the
[releases](https://github.com/Aryalleh/mesh-msg/releases), then:

```bash
tar xzf meshmsg-desktop-*-linux-*.tar.gz && cd meshmsg-desktop-*/
./install.sh          # copies meshd + mesh to ~/.local/bin, starts meshd as a user service
mesh ui               # open the web UI
```

`meshd` must keep running for `mesh` to work. Without the service, run `./meshd` in its
own terminal (or `./meshd &`) and use `./mesh` from another; Ctrl+C stops meshd.
To update, run the new release's `./install.sh` (it restarts meshd; your config,
identity and messages stay).

## Install from source

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

### Settings from the web UI

`mesh ui` → **⚙** has everything that is in `config.toml`: your name, relays (with
direct / tor / proxy), the gateway (share your internet with the mesh), bridges, LAN
mesh, the USB LoRa node (port, plus role, region, preset, frequency, power and name of
the attached node), and account backup / restore. Saving applies the change at once
(links reconnect for a moment) and rewrites `config.toml` (its comments are not kept).

### Back up and restore your account

Your account is the secret key in `identity.key`. Back it up sealed with a passphrase:

```bash
mesh identity export -o backup.txt   # asks for a passphrase twice; prints to stdout without -o
# on the new machine, with meshd stopped:
mesh identity import backup.txt      # or paste it: mesh identity import
```

Or in the web UI: **⚙ → Account → Back up account / Restore from backup** (works
while meshd runs).

The backup (`meshkey1:…`, one line) also restores on Android ("I have a backup" when
setting up the app), and Android backups restore here. Contacts and messages are not
included. Run the account on one device at a time. `import -replace` switches a data
dir that already has another identity (the old key is kept as `identity.key.replaced-…`).

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
- `identity.key` is your secret key — never share it; back it up with `mesh identity export`
  (Argon2id + XChaCha20-Poly1305, so the backup is only as strong as its passphrase).

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

### The terminal app

Run `mesh` with no arguments to open the interactive app: your chats (most recent
first, with unread counts), live incoming messages and ✓✓ receipts, and every command
and setting as a `/command`.

```
mesh> 1                       open chat 1 (or type a name)
Sara> سلام                    type and press Enter to send
Sara> /back                   back to the chat list
mesh> /add meshmsg:… Reza     add a contact
mesh> /global                 join the global public chat (/region tehran for your area)
mesh> /group Family: Sara, Ali   start a private group; /members, /addmember, /kick
mesh> /set gateway=off        change a setting (/settings shows them all, /keys lists keys)
mesh> /relay add ir-1 https://relay-ir.example
mesh> /backup                 back up your account   (/restore to switch to a backup)
mesh> /help                   everything else; Tab completes; Ctrl+D or /quit leaves
```

Leaving the app does not stop meshd; messages keep arriving in the background.

### Commands

```bash
mesh id                          # your contact link — share it
mesh add 'meshmsg:…' Sara        # add a contact
mesh send Sara "سلام"            # send
mesh read Sara                   # history (✓ sent, ✓✓ delivered)
mesh watch                       # live incoming messages
mesh status                      # links, peers, stored packets
mesh ui                          # open the web UI
mesh config                      # all settings
mesh config set gateway=on lan.peers=10.0.0.5:7000   # change them (mesh config keys)
mesh relay add ir-1 https://relay-ir.example socks5://127.0.0.1:1080
mesh bridge add ir-1 eu-1 50     # MB/day
mesh ports                       # USB serial ports for a LoRa node
```

Settings changed from the app, `mesh config`/`relay`/`bridge` or the web UI are the
same: applied at once and written to `config.toml`.

### Relays found automatically

Besides the relays in your config, meshd uses relays from the official list
(`network/relays.json`, eu-1 first), from other relays' lists, and from nearby devices and
LoRa nodes, which share the relay they use. Each one is checked before use.

```sh
mesh relay known            # what was found, and which are in use
mesh relay forget <name>    # never use one
mesh config set discovery=off
```

### Groups and public chats

```bash
mesh group new Family Sara Ali   # private group with these contacts
mesh group add Family Reza       # add / remove members (owner only)
mesh group remove Family Reza
mesh group members Family
mesh group join global           # the global public chat
mesh group join '#tehran'        # a regional public chat (any city / country name)
mesh send Family "شام ساعت ۸"    # send / read work with groups too
mesh groups                      # joined groups;  mesh group leave <group>
```

In the app: `/group <name>: <contact>, …`, `/members`, `/addmember`, `/kick`, `/global`,
`/region <name>`, `/leave`. In the web UI: **+ → New group / Public chats**, and **Members** in a group.

A private group's message is sent to every member separately, so you see ✓ 2/3 until all
members have it (✓✓). Public chats are open to everyone and have no delivery receipts (✓ only).

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
# on the new machine:
mesh identity import backup.txt      # or paste it: mesh identity import
                                     # (with meshd running it switches to the account at once)
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

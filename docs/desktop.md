# Desktop Client (`meshd`) — Linux first

## 1. Scope

A desktop program anyone can install to chat, **and** to lend their connectivity to the
network: a PC with VPN, Starlink or an unfiltered connection becomes a gateway or bridge.

Platforms: **Linux first** (easiest to run as a service, most common for bridge operators),
then Windows and macOS from the same Go code base (cross-compiled).

## 2. Shape

```
meshd            daemon (systemd user/system service) — router, store, interfaces, roles
mesh             CLI: chat, contacts, status, config  (talks to meshd over a local socket)
web UI           served by meshd on 127.0.0.1 only — chat + network status in the browser
```

A local web UI gives one UI for Linux, Windows and macOS without a GUI toolkit; a native
tray app can come later.

## 3. Interfaces

| Interface | Purpose |
|---|---|
| Relay (HTTPS/WS) | client + gateway + bridge sessions to one or more relays |
| LoRa over USB serial | plug in a LoRa node → the PC becomes a Tier 1 gateway |
| LAN | UDP multicast discovery + TCP: PCs and phones on the same private network mesh directly |
| Bluetooth (BlueZ) | later: join Tier 0 phone mesh |

## 4. Roles (config)

```toml
[identity]
path = "~/.local/share/meshd/identity"

[roles]
client  = true
gateway = { enabled = true, mode = "carrier", quota_kb_per_hour = 512 }
bridge  = { enabled = true, relays = ["ir-1", "eu-1"], budget_mb_per_day = 50 }

[[relay]]
name = "ir-1"
url  = "https://..."
via  = "direct"          # direct | socks5://127.0.0.1:1080 | tor

[[relay]]
name = "eu-1"
url  = "https://..."
via  = "socks5://127.0.0.1:1080"   # e.g. through the user's VPN / proxy

[lora]
serial = "/dev/ttyUSB0"
```

`via` per relay matters for bridges: one side goes direct over the domestic network, the
other goes through the VPN / Starlink path.

## 5. Packaging

- Static binary + systemd unit (tarball), `.deb`, AppImage. Release builds reproducible and
  signed; fingerprint published and shown in the app.
- Windows: single `.exe` + service install; macOS: notarised binary + launchd plist (later).

## 6. Milestones

1. `meshd` + `mesh` CLI: identity, contacts, send/receive via one relay.
2. Gateway role + LAN interface.
3. Bridge role with per-relay `via` paths and data budget.
4. LoRa node over USB serial.
5. Local web UI. 6. Windows / macOS builds.

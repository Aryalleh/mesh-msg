# Relay Server

## 1. Role

A **dumb, untrusted, replaceable** store-and-forward relay. It never sees plaintext or sender
identities and has no accounts. Relays are grouped by **region** and federate with each other;
**gateways** connect meshes to them and **bridges** connect relays that can't reach each other.

## 2. Regional design

```
   IR regional relays           bridges              global relays
 ┌──────────┐ ┌──────────┐   (VPN / Starlink /   ┌────┐ ┌────┐ ┌────┐ ┌────┐
 │ ir-1     │◄►│ ir-2     │◄──unfiltered SIM)──►│ EU │◄►│ US │◄►│ AS │◄►│ AU │
 └──▲───────┘ └───▲──────┘                      └────┘ └────┘ └────┘ └────┘
    │ gateways     │ (phones / PCs on domestic network)
```

- **Regional relay** (Tier 2a): hosted *inside* a country so it stays reachable on the
  domestic network when international links are cut (as happened in Iran's shutdowns, where
  the domestic network largely kept working). Run several, on different providers; treat each
  as disposable.
- **Global relay** (Tier 2b): EU, US, Asia, Australia. Fully meshed federation.
- Federation is **one logical network**. When a direct link between relays is cut, a bridge
  carries the same sync protocol over whatever path it has. Relays don't care whether a peer
  sync arrives directly or through a bridge.

Every relay has a config `region = "ir" | "global" | ...` and a signed relay identity
(Ed25519). Relays publish a signed **relay list** (address, region, transports, pubkey),
spread via `/v1/relays`; clients also put the relay they use into their announces (tag 3,
protocol.md "Relay discovery"), and the official list lives in `network/relays.json`. So
users learn new addresses when old ones are blocked, even over Bluetooth and LoRa. A relay
fills in its own `pubkey` in the list it serves. To add an official relay, append it to
`network/relays.json` (with its `/v1/info` pubkey) and push to main.

## 3. Replication and routing between relays

- **v1: full replication.** Every relay syncs every packet, receipt and announce with its
  peers (dedup by `packet_id`), until receipt or expiry. Text traffic is small, and this
  maximises the chance a packet survives relays being blocked or seized.
- **Home region:** a relay records which region first saw each dest hash's announce
  (`announces.home_region`). Packets for that destination are pushed preferentially to
  gateways in that region.
- **v2 (scale):** relays exchange Bloom summaries of dest hashes with registered gateways
  and forward selectively; full replication only for packets with no known home.

## 4. Gateways ("I have internet, send me traffic for my mesh")

Any phone, PC or LoRa gateway node that can reach a relay opens a gateway session:

```
WS /v1/gateway
 → HELLO  {gateway_pubkey, mode, quota, transports: [ble, lora, lan]}
 → SERVE  {delegations: [signed "H(x) may be fetched by any gateway until T", ...],
           summary: bloom(dest hashes announced in my local mesh)}
 ← PUSH   packets for dest hashes this gateway serves
 → UPLOAD packets from the gateway's store (outbound from the mesh)
 ↔ RECEIPT both directions
```

Modes:

| Mode | Receives from relay |
|---|---|
| `targeted` | only packets for dest hashes it serves (delegations / summary match) |
| `carrier` | also up to `quota` KB/h of undelivered packets whose home region = this relay's region, to spray into its local mesh |

| `lora` | a LoRa GATEWAY node (directly over Wi-Fi, or via its host `meshd` / phone): the relay selects packets for **broadcast on air** within the node's airtime budget (firmware.md §2a); everything the node hears on air is uploaded |

A `lora` gateway session is authenticated by the **node's** key, whether the node connects
directly (Wi-Fi / Ethernet / LTE) or through a phone or PC tunnel. Several live connections
for the same node key are one gateway: each packet is pushed once, on any live connection
(firmware.md §2b).

**LoRa as a bridge between relays.** Two relays that cannot reach each other (two cities'
isolated regional relays, or an isolated relay and an internet-connected one) each have a
`lora` gateway in radio range of the same LoRa mesh. Relay A's packets are broadcast, heard by
B's gateway, and uploaded to relay B — and vice versa. Capacity is a few dozen text packets per
hour per node, so relays select for LoRa only what has **no other path**: destinations whose
home region is on the far side, not currently online, and without a receipt.

Gateways keep a long-lived WebSocket when possible; on flaky links they fall back to
periodic HTTPS sync. Phones that are online are also ordinary clients: they get their own
messages pushed live.

## 5. Bridges

A bridge holds sessions with two (or more) relays that cannot reach each other and runs the
federation sync between them:

```
bridge ── /v1/sync (cursor-based pull + push) ──► relay A   (e.g. ir-1, via domestic net)
bridge ── /v1/sync ─────────────────────────────► relay B   (e.g. EU, via VPN/Starlink)
```

- Same `/v1/sync` API relays use with each other; a bridge is effectively a federation link.
- Bridges are untrusted (they only see ciphertext) and rate-limited by bridge key; relays may
  allowlist known bridge keys for higher quotas.
- Bridge traffic budget is configurable (white-SIM / satellite data is expensive and scarce):
  priority order receipts → announces → text → everything else; `NO_LORA`/media last or never.

## 6. API (v1)

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/packets` | upload packets (proof-of-work for non-allowlisted keys) |
| GET  | `/v1/mailbox/{dest_hash}?cursor=` | fetch packets for a destination (signed challenge or delegation) |
| GET  | `/v1/challenge` | nonce for signed requests |
| POST | `/v1/receipts` | upload receipts; verified, matching packets deleted |
| POST | `/v1/receipts/query` | sender asks for receipts of its packet IDs |
| POST | `/v1/announces` | upload signed announces |
| WS   | `/v1/stream` | client live push |
| WS   | `/v1/gateway` | gateway session (§4) |
| POST | `/v1/sync` | federation / bridge sync: exchange cursors, push & pull batches |
| GET  | `/v1/relays` | signed relay list |
| PUT  | `/v1/media/{id}` | store an encrypted file (only on relays with media on; §6a) |
| GET  | `/v1/media/{id}` | fetch it (`Range: bytes=N-` resumes) |
| GET  | `/v1/media/quota` | the signed account's allowance today: `{per_day, used, left, resets, allowed}` |
| POST | `/v1/usage` | push signed important-media usage entries (§6a ledger) |
| GET  | `/v1/usage?since=` | pull the usage ledger (anyone) |

Bodies are binary (length-prefixed packets), not JSON, to stay small on throttled links.

**Abuse control (no accounts):** hashcash proof-of-work with adaptive difficulty, per-IP and
per-key rate limits, size caps (packet ≤ 64 KB, mailbox ≤ N MB), TTL (7 days text).

## 6a. Media (optional)

Photos, voice and video messages and files travel **outside** the packet stream, so they
never slow messages down: nearby phones pull them from each other in small windows over
Bluetooth or Wi-Fi, and relays that allow it hold them for people who are far away. Media
never goes over LoRa and is never synced between relays.

A relay takes media only if `media_max_mb > 0`. Files are end-to-end encrypted blobs named
by their hash; the relay can't read them.

| Config | Default | |
|---|---|---|
| `media_max_mb` | 0 (off) | largest file it takes |
| `media_store_mb` | 2048 | total storage; when full, uploads get 507 and clients try another relay |
| `media_important_per_day` | 5 | important files one account may upload per UTC day (below) |
| `media_important_hours` | 168 | how long a file waits for its readers (below) |
| `media_accounts` | `[]` (anyone, with PoW) | if set, only these accounts (hashes from `mesh id`) may upload. Anyone may download. |

Every upload is signed by the account: `X-Account` (hex public key), `X-Day` (UTC day
number = Unix time / 86400, the relay's day ± 1) and `X-Signature` over
`MESHMSG-MEDIA-PUT` | id (hex) | day (4 bytes, big-endian). Unsigned uploads get `401`.

The uploader says how many people will download it (`X-Readers`). **The relay deletes the
file as soon as that many complete downloads happened**, or when its time runs out, whichever
comes first. Nothing is kept after that.

### Important media only, with a daily allowance (implemented in v0.1.0-alpha10)

Relays carry only **important** media (protocol.md §6b); normal media moves directly
between devices and never touches a relay. Every upload is signed (`X-Account`,
`X-Signature`, as with `media_accounts`), so the relay can count per account:

| Config | Default | |
|---|---|---|
| `media_important_per_day` | 5 | files one account may upload per UTC day; one file to a group counts once, whatever the number of members. Each relay sets its own; editable in the admin panel (§6b), applies at once without a restart |
| `media_important_hours` | 168 (7 days, as text) | how long an important file waits for its readers |

- Over the allowance → `429` (`Retry-After`, body says when it resets); `GET /v1/media/quota`
  (signed like an upload, over `MESHMSG-MEDIA-QUOTA` | day) returns used / left; the app
  asks before sending and sends the file as normal media when nothing is left.
- For now each relay counts only the uploads it saw (`data_dir/media/usage.json`); the
  network-wide count (ledger) and the proof-of-work by account age below come next.
- An accepted file is **never evicted** before its readers have it or its time runs out.
  When storage would not allow that, the relay refuses the upload (`507`) and the client
  tries another relay; it never drops a file it accepted.
- `media_max_hours` / `media_min_hours` no longer apply to relay media.

### Abuse control for important media (requirement, not implemented yet)

Identities are free, so one person can make many accounts (a Sybil attack), and a person can
upload to several relays. Three measures work together; every number is set per relay in
the admin panel.

1. **Account age.** An account's age is the time of its first announce anywhere in the
   network (announces are already replicated to every relay, so all relays agree). Accounts
   younger than `media_new_account_days` (default 3) get `media_new_account_per_day`
   (default 1) instead of the full allowance.
2. **Proof-of-work that grows with use and shrinks with age.** Every important upload
   carries hashcash. Each extra bit doubles the work:

   ```
   bits = media_pow_base_bits                      (default 20: a few seconds on a phone)
        + age_bits(account age)                    (table below)
        + uploads_today                            (network-wide, from the ledger: +1 per file)
        + load_bits                                (0–2, from the relay's storage/traffic use)
   bits = min(bits, media_pow_max_bits)            (default 28: a few minutes, never more)
   ```

   | Account age (first announce in the network) | `age_bits` | First file of the day, roughly |
   |---|---|---|
   | under 3 days | +3 | ~8× base (≈ 30 s) |
   | 3–30 days | +2 | ~4× base |
   | 30–180 days | +1 | ~2× base |
   | over 180 days | 0 | base (a few seconds) |

   The tier limits and their bits are panel settings (`media_pow_age_tiers`). The relay tells
   the client the required bits before the upload (`GET /v1/media/quota` returns them), so the
   app can show "preparing…" instead of failing. The work is bound to the media id, the
   account and the day, so it can't be reused. New accounts pay the most, so making many
   accounts no longer pays, while long-standing users barely notice it.
3. **Network-wide usage ledger** (below). The allowance counts uploads on **all** relays, not
   only this one: `used = sum of the account's entries for today in the ledger`.

#### Usage ledger: open, verifiable, no owner

Like Bitcoin's ledger, every relay holds a full copy, anyone can read it, and nobody has to
be trusted: each entry carries the **account's own signature** over its upload, so a relay
can't invent uploads to block someone, and any relay can check every entry itself. Unlike
Bitcoin it needs no mining or global agreement on order: entries only add up, and copies
that met each other converge (a grow-only set).

```
entry = day(4) | media_id(16) | account_pubkey(32) | account_sig(64)  (the X-Signature of the
        upload: "MESHMSG-MEDIA-PUT" | media_id | day) | relay_pubkey(32) | relay_sig(64)
```

- Entries are deduplicated by (account, media_id): uploading the same file to four relays
  counts once. A day's entries are dropped after 2 days.
- **Spread:** every relay reports its new entries to the **origin relays** (`origin: true`
  in `network/relays.json`, the list kept in this GitHub repository) and pulls everything new
  from them; origin relays serve the merged ledger to every relay. Entries also travel in
  the ordinary `/v1/sync` between peers and through bridges, so when origin relays are
  blocked or gone, relays still exchange the ledger directly. Origin relays are meeting
  points (like Bitcoin's seed nodes), not authorities: they can't forge or hide-and-alter
  entries, only be slow, and any relay can serve the ledger.
- API: `POST /v1/usage` (a relay pushes signed entries), `GET /v1/usage?since=<cursor>`
  (anyone pulls). A relay that sends invalid entries is ignored and shown in the panel.
- Each relay still applies its own number: an account over this relay's
  `media_important_per_day` (counted network-wide) gets `429` here, even if another relay
  with a higher number would take it.
- Privacy cost: the ledger is public, so anyone who knows an account id can see how many
  important files it uploaded on a day and to which relays (not to whom, nor what). Ids are
  already public in announces. Accepting that is the price of a ledger nobody controls.
- A relay that does not report weakens only its own count; it can't raise anyone's
  allowance elsewhere.

## 6b. Admin panel

`/admin` on every relay. Login: account id → the relay encrypts an 8-digit code to that
account (Noise X, like any first message) and puts it in the account's mailbox → the admin
types it in. Codes last 10 minutes and allow 5 tries; one per account per minute, 30 per
hour per relay. Sessions: random token in an HttpOnly, SameSite=Strict cookie, 12 h; POSTs
need the `X-Mava-Panel` header. The owner account (`server/admin.OwnerID`) is always an admin
and can't be removed; other admins live in `data_dir/panel.json` (panel or `meshmsg-relay
admin add|rm|list`). Settings changed in the panel go to the same file and override
relay.json after a restart. The important-media settings (`media_important_per_day`, `media_important_hours`,
`media_new_account_days`, `media_new_account_per_day`, `media_pow_base_bits`,
`media_pow_max_bits`, `media_pow_age_tiers`, §6a) are set in the panel and take
effect immediately; the panel also shows the ledger's state and relays sending invalid entries.

## 6c. TURN for the relay's admins (optional)

Off by default; available on every relay, origin relays included. When on, only the relay's
own admins (§6b) can relay
calls and direct sessions through it when no direct path exists. Settings in the panel:
`turn_enabled`, `turn_max_sessions`, `turn_max_kbps`, `turn_monthly_gb`. Credentials:
`POST /v1/turn/credentials` (signed by an admin account, valid 10 min). Details and rules:
connectivity.md §7.

## 6d. Ports (the same on every relay)

Every relay uses one fixed set of ports, so one firewall rule set fits all relays and the
apps know where to look without asking. `install.sh` opens them in ufw or firewalld when
one is active (it never turns a firewall on); open the same ports in a provider's cloud
firewall. Constants: `core/wire` `Port*`.

| Port | Protocol | Use | Needed |
|---|---|---|---|
| 80 | TCP | API and admin panel over HTTP (or the HTTPS proxy's redirect / ACME) | always |
| 443 | TCP | API over TLS (Caddy or another proxy in front); TURN over TLS shares it | with a domain |
| 3478 | UDP | address reflector for direct links (connectivity.md §2) | when direct links ship |
| 3479 | UDP | second reflector port: NAT type detection (connectivity.md §3a) | when direct links ship |
| 3478 | TCP + UDP | TURN listener, for the relay's admins (§6c) | only with TURN on |
| 61000–61999 | UDP | TURN relayed addresses (1000 sessions at most) | only with TURN on |

Why these: 3478/3479 are the IANA-registered STUN/TURN ports, which most networks already
let through. 61000–61999 lies in the IANA dynamic range (no registered service) and above
Linux's default ephemeral range (32768–60999), so it never clashes with the relay's own
outgoing connections. The relay's HTTP listener itself stays on `127.0.0.1:8080` behind the
proxy; 8080 is not opened. Nothing listens on the UDP ports until those features exist, so
opening them early is harmless.

## 7. Data model

```
packets   (packet_id PK, dest_hash, frag_index, blob, origin_region, received_at, expires_at)
receipts  (packet_id PK, blob, received_at)
announces (dest_hash PK, blob, home_region, updated_at)
peers     (relay_pubkey PK, url, region, cursor_in, cursor_out, last_sync)
gateways  (session state, in memory only)
```

## 8. What a relay can still see

Destination hashes, sizes, timing, and the IP addresses of connecting gateways/clients
(unless they use Tor). **A regional relay inside a censoring country is the most exposed
component** — see threat-model.md. Defaults: no logs, short retention, in-memory gateway
state, traffic indistinguishable from ordinary HTTPS on a boring-looking domain.

## 9. Deployment

- `server/cmd/relay`: one static Linux binary; config file sets region, peers, transports.
- `deploy/`: systemd unit, Dockerfile, Caddyfile (TLS), torrc for an onion address.
- Storage: SQLite by default, Postgres optional.

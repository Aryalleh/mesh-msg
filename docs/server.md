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
| `media_max_hours` | 8 | how long a small file (≤ 1 MB) waits for its readers |
| `media_min_hours` | 1 | the shortest wait, for big files: keep time = max(min, max × 1 MB / size) |
| `media_accounts` | `[]` (anyone, with PoW) | if set, only these accounts (hashes from `mesh id`) may upload; uploads are then signed (`X-Account`, `X-Signature` over `MESHMSG-MEDIA-PUT` and the id). Anyone may download. |

The uploader says how many people will download it (`X-Readers`). **The relay deletes the
file as soon as that many complete downloads happened**, or when its time runs out, whichever
comes first. Nothing is kept after that.

### Important media only, with a daily allowance (requirement, not implemented yet)

Relays carry only **important** media (protocol.md §6b); normal media moves directly
between devices and never touches a relay. Every upload is signed (`X-Account`,
`X-Signature`, as with `media_accounts`), so the relay can count per account:

| Config | Default | |
|---|---|---|
| `media_important_per_day` | 5 | files one account may upload per UTC day; one file to a group counts once, whatever the number of members. Each relay sets its own; editable in the admin panel (§6b), applies at once without a restart |
| `media_important_hours` | 168 (7 days, as text) | how long an important file waits for its readers |

- Over the allowance → `429` with the time it resets; `GET /v1/media/quota` (signed) returns
  used / left so the app can show it before sending.
- An accepted file is **never evicted** before its readers have it or its time runs out.
  When storage would not allow that, the relay refuses the upload (`507`) and the client
  tries another relay; it never drops a file it accepted.
- `media_max_hours` / `media_min_hours` no longer apply to relay media.

Open: identities are free, so a new account gets a new allowance, and each relay counts on
its own (a client could use the allowance on several relays). Options: proof-of-work per
upload that grows with use, accounts vouched for by contacts, or syncing counters between
relays.

## 6b. Admin panel

`/admin` on every relay. Login: account id → the relay encrypts an 8-digit code to that
account (Noise X, like any first message) and puts it in the account's mailbox → the admin
types it in. Codes last 10 minutes and allow 5 tries; one per account per minute, 30 per
hour per relay. Sessions: random token in an HttpOnly, SameSite=Strict cookie, 12 h; POSTs
need the `X-Mava-Panel` header. The owner account (`server/admin.OwnerID`) is always an admin
and can't be removed; other admins live in `data_dir/panel.json` (panel or `meshmsg-relay
admin add|rm|list`). Settings changed in the panel go to the same file and override
relay.json after a restart. The important-media allowance (`media_important_per_day`, §6a)
and `media_important_hours` are set in the panel and take effect immediately.

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

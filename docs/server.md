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
spread both via `/v1/relays` and as a mesh announce, so offline users learn new addresses
when old ones are blocked.

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

Bodies are binary (length-prefixed packets), not JSON, to stay small on throttled links.

**Abuse control (no accounts):** hashcash proof-of-work with adaptive difficulty, per-IP and
per-key rate limits, size caps (packet ≤ 64 KB, mailbox ≤ N MB), TTL (7 days text).

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

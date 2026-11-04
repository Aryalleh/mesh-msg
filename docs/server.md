# Relay Server (Tier 2)

## 1. Role

A **dumb, untrusted, replaceable** store-and-forward mailbox that connects meshes through
whatever internet access exists. It never sees plaintext or sender identities and holds no
accounts. Anyone can run one; relays federate so a packet uploaded to one is fetchable from
the others.

## 2. Tech

- Go, single static binary (`cmd/relay`), easy to deploy on any VPS.
- Storage: SQLite (small relays) or Postgres (large), behind a `Store` interface.
- TLS via a reverse proxy (Caddy) or built-in autocert.
- Reachable via: direct HTTPS · CDN-fronted domain · Tor onion service. The server code is
  identical; only deployment differs.

## 3. Data model

```
packets   (packet_id PK, dest_hash, frag_index, blob, received_at, expires_at)
receipts  (packet_id PK, blob, received_at)        -- signed, verifiable
announces (dest_hash PK, blob, updated_at)         -- latest signed announce per identity
peers     (url, pubkey, last_sync)                 -- federation
```

## 4. API (v1)

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/packets` | upload a batch of raw packets (with proof-of-work) |
| GET  | `/v1/mailbox/{dest_hash}?cursor=` | fetch packets for a destination (auth required) |
| POST | `/v1/receipts` | upload receipts; server verifies sig and deletes matching packets |
| POST | `/v1/receipts/query` | sender asks for receipts of its packet IDs |
| POST | `/v1/announces` | upload signed announces (needed to verify auth + receipts) |
| GET  | `/v1/challenge` | nonce for mailbox auth |
| GET  | `/v1/stream` | WebSocket: live push for connected clients |
| GET  | `/v1/relays` | signed list of known relays / mirrors |

Bodies are binary (concatenated length-prefixed packets), not JSON, to keep them small over
slow or throttled links.

**Mailbox auth:** client signs `("FETCH" || nonce || dest_hash)` with the identity's
Ed25519 key, or presents a **delegation** (signed by the identity: "any gateway may fetch
for me until T") so an online neighbour can fetch on behalf of offline users.

**Abuse control (no accounts):** hashcash-style proof-of-work on uploads with adaptive
difficulty, per-IP rate limits, size caps (packet ≤ 64 KB, mailbox ≤ N MB), TTL expiry
(default 7 days).

## 5. Federation

Relays exchange packets, receipts and announces with configured peers (push + periodic
pull, dedup by `packet_id`). The signed relay list (`/v1/relays`) is also spread as a mesh
announce so offline users learn new relay addresses when old ones get blocked.

## 6. What the server can still see

Destination hashes, packet sizes and timing, uploader/fetcher IP addresses (unless they
use Tor). See threat-model.md. Mitigations: Tor path, batching/padding of uploads, fetching
cover mailboxes, short retention, no logs by default.

## 7. Layout

```
server/
  cmd/relay/           main
  internal/api/        HTTP + WebSocket handlers
  internal/proto/      outer header codec, announce/receipt verification (spec/ vectors)
  internal/store/      sqlite, postgres
  internal/federation/
  internal/pow/
  deploy/              Dockerfile, Caddyfile, torrc example
```

# Edge relays (Cloudflare Workers, ArvanCloud Edge Computing)

A Mava relay can run without a server, on an edge platform: **Cloudflare Workers** (storage
in a D1 database) or **ArvanCloud Edge Computing** (storage in an Arvan Object Storage
bucket). It is a full relay for messages: phones, desktops, boards, gateways and other relays
use it like any relay (server.md), and it has the admin panel and trips (trip.md). One
installer sets up any of the three kinds:

```sh
curl -fsSL https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/server/deploy/install-relay.sh | bash
```

It asks where the relay runs (this server / Cloudflare / Arvan), its name and region, your
Mava account id (admin panel logins), and optionally a peer relay to sync with.

## 1. What an edge relay does and doesn't do

| | Server relay | Edge relay |
|---|---|---|
| Messages, mailboxes (long-poll), receipts, announces, signed relay list | yes | yes |
| Gateways (phones serving a mesh, carrier mode) | yes | yes |
| Federation (`/v1/sync/pull`, `/v1/sync/push`) | yes | yes; it also pulls from and pushes to its peers every minute (Cloudflare cron; on Arvan after requests) |
| Admin panel (`/admin`, login code sent to your Mava account) | yes | yes (settings: proof-of-work bits, message lifetime; admins) |
| Trips and the trip map (`/v1/trip`, `/admin/trip`) | yes | yes |
| Release manifests (`/v1/release`) | from GitHub | passed on from `RELEASE_UPSTREAM` (default eu-1) |
| Media files (photos, voice, video) | yes | no (`/v1/info` says so; apps send files another way) |
| UDP: address reflector and TURN for calls | yes | no (edge platforms have no UDP) |
| Usage ledger (media) | yes | no |

## 2. How it is built

`edge/src/` is the relay in JavaScript with no dependencies; `node edge/build.mjs` joins it
into `edge/dist/cloudflare.js` (an ES module with `fetch` and `scheduled`) and
`edge/dist/arvan.js` (a service worker with a `CONFIG` placeholder the installer fills in).
The bundles are committed; CI fails if they are out of date. The admin panel and trip map are
the Go relay's pages (`server/admin/`), built in.

- **Same wire protocol**: frames, proof of work (`X-PoW`), signed mailbox fetches, gateway and
  sync bodies, `X-Cursor`/`X-More`, CORS, the trip API, as `server/api`.
- **Crypto**: Ed25519 through WebCrypto when the runtime has it, else in JavaScript (BigInt);
  SHA-2 and HMAC from WebCrypto; X25519, ChaCha20-Poly1305 and Noise X in JavaScript (the
  admin login code is a sealed Mava message, like the Go relay's).
- **Mailbox challenges** are stateless: the nonce carries its time and an HMAC with the
  relay's secret (good for 5 minutes), since edge instances share no memory.
- **The relay's key** is made on the first request and kept in the store.
- **Long-poll**: the relay looks at the store every 2 s (Arvan: 3 s) up to 25 s (Arvan: 20 s).

### Cloudflare: D1

The same `items` table as the Go relay (`core/store/sqlite.go`), with hex and base64 text
columns, and a `kv` table (identity, settings, sessions, login codes, trip card, sync
cursors). Bindings: D1 `DB`; variables `RELAY_NAME`, `RELAY_REGION`, `ADMINS`, `POW_BITS`,
`TTL_HOURS`; secrets `SYNC_TOKENS`, `PEERS` (JSON `[{"name","url","token"}]`); a cron
trigger every minute (expiry and federation). The installer writes `wrangler.toml`, creates
the database, deploys with `wrangler` and sets the secrets.

### Arvan: Object Storage

Arvan's edge has no documented database, so the relay keeps every packet as an object in an
S3-compatible bucket (requests signed with AWS Signature V4), with its metadata in the name:

```
i/<seq>_<type>_<packet id>_<fragment>_<destination>_<origin tag>   the packet
k/<packet id>_<fragment>    → its i/ name (duplicates, receipts, purges)
d/<destination>/<seq>       → its i/ name (mailboxes)
a/<destination>             → the newest announce
kv/<key>                    small values
```

`seq` is the time in microseconds plus a random part, so writes from different edge locations
get distinct, nearly ordered numbers; readers only look at objects older than 4 seconds, so a
cursor never skips a late write (messages wait at most a few seconds more). The installer
downloads Arvan's CLI (`arvan`, from git.arvancloud.ir), writes the relay's settings (bucket
keys included) into the bundle and runs `arvan ec deploy`. The Edge Computing application must
exist in the Arvan panel first.

## 3. Costs and limits

A phone keeps a mailbox long-poll open (one request about every 25 s, ~3,500 a day) plus
uploads. On **Cloudflare's free plan** (100,000 requests and 5 million D1 row reads a day) that
is roughly 20 phones; the paid plan (USD 5 a month, 10 million requests a month included)
covers a few dozen, more at USD 0.30 per extra million. Leaders also poll the trip's mailbox
every 10 s. On **Arvan**, edge requests and Object Storage requests are both billed; each
mailbox poll lists the bucket, so a server relay is cheaper for large groups. For a large
group the best mix is a server relay with an edge relay as a second, hard-to-block address
(federated both ways).

## 4. Tests

- `node --test edge/test/*.test.mjs`: the crypto against Node's own (RFC 8032 vector,
  X25519, ChaCha20-Poly1305, Noise X).
- `edge/edgetest` (Go, needs Node 22.5+): runs the bundles in Node (`edge/test/server.mjs`:
  D1 on `node:sqlite`, or the S3 store against a fake S3; the Arvan bundle as a service worker)
  and uses them from the Go code: two phones chat and get receipts (D1, S3, Arvan bundle), the
  signed relay list, the admin panel login with the code read in Mava, a trip end to end, and
  federation both ways with a Go relay.

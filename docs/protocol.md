# Wire Protocol v0 (draft)

All integers big-endian. All sizes in bytes. The hard limit is the LoRa frame: **255 bytes**
(SX126x max payload). Every text message must fit in ≤ 16 LoRa fragments; most fit in one.

## 1. Identity and addressing

Each identity has two keypairs:

- `X25519` — encryption (Noise DH)
- `Ed25519` — signatures (announces, receipts, server auth)

```
dest_hash = SHA-256(x25519_pub || ed25519_pub)[0..16]      # 16 bytes
```

Contacts are exchanged via QR code / share link (`meshmsg:` URI with both public keys),
or learned from signed announces.

## 2. Outer packet header (26 bytes, visible to every relay)

```
offset size field
0      1    flags      = version(2) | type(3) | ctx(3)
1      1    hops       = hop_count(4) | max_hops(4)
2      8    packet_id  random; dedup key
10     16   dest_hash  destination (or 0x00.. for broadcast)
26     ..   body
```

`type`:

| val | name | purpose |
|---|---|---|
| 0 | DATA | end-to-end encrypted message |
| 1 | RECEIPT | signed delivery proof; purges stored copies |
| 2 | ANNOUNCE | signed "I exist / reachable here" |
| 3 | PATH_REQ | ask neighbours for a route to `dest_hash` |
| 4 | SUMMARY | LoRa cluster reachability (Bloom filter) |
| 5 | LINK | hop-local control (ACK, inventory); never forwarded |
| 6–7 | reserved | |

`ctx` bits: `0x4 FRAGMENTED`, `0x2 NO_LORA`, `0x1 PRIORITY`.

If `FRAGMENTED`: byte 26 = `index(4) | (count-1)(4)`; all fragments share `packet_id`;
dedup key = `packet_id || index`.

`hop_count` is the only field mutated in transit and is excluded from authentication.

## 3. DATA body — end-to-end encryption (Noise)

Noise prologue = `flags & 0xF8 || packet_id || dest_hash` (binds header to ciphertext).

**Known contact** — `Noise_K_25519_ChaChaPoly_SHA256` (both statics known in advance):

```
e (32) | ciphertext(inner) | tag (16)            overhead = 48
```

The recipient identifies the sender by trial-decrypting against its contact list
(`es` computed once per packet, `ss` precomputed per contact; cost is negligible).

**First contact** — `Noise_X_25519_ChaChaPoly_SHA256` (sender static transmitted encrypted):

```
e (32) | enc(s) (48) | ciphertext(inner) | tag (16)   overhead = 96
```

Inner plaintext:

```
0  1  content_type   0=text, 1=text-compressed, 2=contact-card, 3=media-chunk, 4=e2e-ack
1  4  timestamp      unix seconds
5  1  sender_ed_hint first byte of sender ed25519 key (fast path; optional)
6  .. content
```

**Single-frame budget:** 255 − 26 header − 48 Noise − 6 inner = **175 bytes of content**.

### Text encoding

Persian/Arabic in UTF-8 is 2 bytes/char (≈ 87 chars per frame). Content type 1 uses a
**window encoding** (SCSU-style: one byte per char inside the Arabic block, escape for others)
followed by an optional static-dictionary compressor, targeting ~150–170 Persian chars per
frame. To be specified and benchmarked in M0.

Forward secrecy: v0 gets partial FS from the ephemeral `e`. v1 adds periodic key rotation
(contacts exchange fresh prekeys inside messages). A full Double Ratchet is not planned for
LoRa because of header overhead and heavy reordering.

## 4. RECEIPT

```
body: acked_packet_id (8) | ed25519 signature (64) over ("RCPT" || packet_id || dest_hash_of_original)
outer dest_hash = original dest_hash
```

Any holder that knows the recipient's Ed25519 key (from announces) verifies and purges.
Receipts follow the **reverse path**: each forwarder remembers which interface a DATA packet
arrived on for `receipt_window` (default 24 h). Servers index receipts by packet_id so the
sender can poll for them.

## 5. ANNOUNCE (≈ 190 bytes, fits one frame)

```
x25519_pub (32) | ed25519_pub (32) | timestamp (4) | app_data_len (1) | app_data (≤32) | sig (64)
```

`app_data` (TLV): nickname (optional, off by default), `gateway_delegation` flag, capability bits.
Announces are rate-limited per interface (see firmware/android docs) and are **opt-in for
visibility outside Tier 0** (privacy: see threat-model.md).

## 6. Routing

Each router keeps:

```
route_table : dest_hash → {interface, next_hop, hops, last_seen, source=announce|summary}
store       : packet_id → {packet, expires_at, forwarded_to[], arrival_iface}
seen        : packet_id(+frag) → time            # dedup, ~24 h
```

Forwarding decision for a packet with `dest_hash`:

1. Destination is me → deliver, emit RECEIPT.
2. Known route on a point-to-point interface (BLE link, relay) → send to next hop.
3. Otherwise keep in store and
   - Tier 0: epidemic exchange with neighbours (summary-vector inventory, then pull missing),
     bounded by `max_hops` and store quotas.
   - Tier 1 (LoRa, **always broadcast**): queue for broadcast under the airtime priority rules
     in firmware.md §2a; every node rebroadcasts once (managed flood, `max_hops` ≤ 3, dedup,
     SNR-ordered delay). Cluster SUMMARY matches raise priority; they don't select a next hop.
   - Tier 2: if any relay is reachable → upload.
4. Drop on RECEIPT seen or expiry (default 7 days text, 1 day media).

**SUMMARY:** each LoRa node periodically broadcasts a 128-byte Bloom filter of dest hashes
announced by phones in its local cluster in the last N hours (k=3, ~1% FP at ~100 entries).
Gateway nodes report heard summaries to their relay, so a relay knows which destinations are
reachable over its LoRa side and prioritises those packets for broadcast.

## 6a. Groups and public chats

### Private groups: a member list

A private group is a list of members kept by every member. Each message is an ordinary
one-to-one message (§4, always Noise **X** so the sender is authenticated and members need not
have added each other) sent **separately to every other member**, with inner content type
`5` (GROUP). Each copy gets its own RECEIPT, so the sender sees ✓ n/m and ✓✓ when all
members have it. Relays, LoRa nodes and non-members see only ordinary messages.

```
content = group_id(16) | op(1) | body
op 0 TEXT     text_type(1) | text                       (as content types 0/1)
op 1 ROSTER   version(4) | name_len(1) | name | n(1) | n × (x(32) | ed(32) | name_len(1) | name)
op 2 LEAVE    (empty)  the sender left
op 3 REMOVED  (empty)  the owner removed the recipient
```

`group_id` is 16 random bytes and serves as the chat's hash in UIs. The **owner** creates the
group and sends the ROSTER (at most 30 members, so it fits one message) to every member;
whoever sent the first ROSTER a member accepts is that group's owner, and only the owner's
later ROSTERs with a higher version count. The owner adds and removes members by sending a
new ROSTER to everyone (and REMOVED to whoever was removed). A member who leaves sends LEAVE
to everyone; the owner then sends an updated ROSTER. A TEXT that arrives before its ROSTER
opens the group with the sender as a member until the list arrives. Senders' names are those
the owner gave in the roster, unless the reader saved the person as a contact.

### Public chats: a shared key

A public chat is addressed like a person. Its 32-byte secret derives, with HKDF-SHA256:

```
group identity = identity.FromSeed(HKDF(secret, info "mesh-msg group identity", 64 bytes))
group key      = HKDF(secret, info "mesh-msg group key", 32 bytes)
dest_hash      = hash of the group identity (as in §1)
```

Every member holds the group identity, so members **announce** it (no name; delegation flag
as for their own announce) and **fetch its relay mailbox** with it (signed challenge), and
gateways fetch it for members who are offline. Relays and LoRa nodes need no changes and
cannot tell a group from a person.

| Kind | Secret | Joined by |
|---|---|---|
| regional | `SHA-256("mesh-msg public channel v1|" + lower(name))` | `meshgroup:#tehran`, or typing the name |
| global | same, name `global` | `meshgroup:#global` |

Public chats are readable by anyone who knows the name. That is the point.

DATA body to a public chat (no Noise; one symmetric key per chat):

```
ChaCha20-Poly1305(group key, nonce = 0x00000000 || packet_id, ad = prologue (§3),
  sender_x(32) | sender_ed(32) | name_len(1) | name(≤32) | content_type(1) | timestamp(4) | content
  | sig(64))
sig = Ed25519(sender, "mesh-msg group v1" || dest_hash || packet_id || everything before sig)
```

Overhead 149 bytes plus the name, so a short message fits one LoRa frame. The signature
stops members (and anyone, in public chats) from posting in someone else's name. The name
is the sender's own label; clients show a saved contact name instead when they have one.

Public chats send **no RECEIPTs**: a receipt purges stored copies, which the other members still
need. Members deliver a group packet *and keep forwarding it*; copies expire with the
store TTL. Global chat packets carry `NO_LORA` (their volume would swamp LoRa airtime);
regional chats use every link.

## 7. Link layers (framing per interface)

| Interface | Framing | Max packet |
|---|---|---|
| LoRa | raw frame = packet | 255 |
| BLE (phone↔phone, phone↔node) | `len(2) | packet`, chunked to ATT MTU | 4 KB |
| Wi-Fi Direct / TCP / WebSocket | `len(4) | packet` | 64 KB |

`NO_LORA` packets are never handed to a LoRa interface.

## 8. Open questions

- Exact text compression scheme for Persian (benchmark in sim/).
- Bloom filter size vs privacy leakage.
- Whether announces from phones should be pseudonymous per-epoch (rotating dest hash).

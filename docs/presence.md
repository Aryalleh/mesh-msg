# Presence from Mava nodes (protocol v1, 2026-11-23)

Shared with Hamyar (hamyar repo docs/presence.md is the same text). Firmware part: `firmware/lib/meshpresence`,
test vectors `spec/test-vectors/presence.json` (made by the Hamyar server code).

Goal: per student, a **place status**: in the university (which building/classroom), in the dorm (which dorm), or
outside / unknown. No coordinates are stored. Module `presence`, per university.

## Who is counted
- Dorm residents (`users.dormitory` = the dorm place's linked dormitory id or name): always, but only inside their own dorm.
- Everyone else, and residents outside their dorm: only if the student switched presence on in the Hamyar app.
- Sightings that don't qualify are dropped. Visit history: 30 days. Every admin look at a place list or a student is logged
  (`presence_views`). "Outside" = not seen for 15 minutes.

## Keys and tokens
- Window: `w = floor(unix_seconds / 120)`. Receivers accept the current and the previous window.
- Node key (32 bytes, shown once in admin → حضور و نودها → مکان‌ها و نودها; re-key there if lost):
  `HMAC-SHA256(SESSION_SECRET, "node|<slug>|<node id>|<key version>")`.
- User key (32 bytes, from `GET api.<base>/v1/presence` → `key`, hex): `HMAC-SHA256(SESSION_SECRET, "user|<slug>|<user id>|<epoch>")`.
- Tokens are the first 8 bytes of an HMAC-SHA256, as 16 hex chars:
  - node beacon: `HMAC(node key, "beacon|<w>")[:8]`
  - phone: `HMAC(user key, "phone|<w>")[:8]`

## 1. Phone hears node (node = beacon)
Node advertises BLE manufacturer data (company id 0xFFFF until Mava has its own), payload 16 bytes:
`"MV" | version 0x01 | flags 0x00 | node id (uint32 BE) | beacon token (8 bytes)` and changes the token every window.
The Mava board's mesh advert is full (29 of 31 bytes) and extended advertising is off, so the board alternates every
second: mesh advert, then this beacon (flags + manufacturer data, 23 bytes). Phones still find both.
The Hamyar app (presence on, or dorm resident) scans, and every ~60 s posts what it heard:

    POST https://api.<base>/v1/presence/seen        Authorization: Bearer <hamyar token>
    {"seen": [{"n": <node id>, "w": <window>, "t": "<16 hex>", "r": <rssi dBm>}]}
    -> {"ok": true, "counted": 0|1, "place": "<name>"}

The strongest valid sighting wins. Offline: the app keeps sightings and sends them through Mava later (v2).

## 2. Node hears phone (node = scanner)
The app advertises the same 16-byte layout with `"MP"` instead of `"MV"`, node id field 0, and the phone token.
The node collects tokens for one window and posts:

    POST https://<slug>.<base>/presence/node
    x-node: <node id>
    x-node-sig: hex HMAC-SHA256(node key, "report|" + <raw body>)
    {"w": <window>, "heard": [{"t": "<16 hex>", "r": <rssi>}]}       (≤ 500 entries)
    -> {"ok": true, "counted": n}   |   403 unknown_node / bad_signature   |   400 stale / bad_body

Nodes without internet hand the signed report to the mesh (LoRa / a gateway phone) in v2; the signature stays valid
while the window is fresh, so v2 must widen the accepted window for relayed reports.

## Setting up a board
Hamyar admin → حضور و نودها → مکان‌ها و نودها → «افزودن نود» shows node id, key (once) and the report URL. On the
board: setup switch on → hotspot `Mava-XXXX` → setup panel → «حضور همیار» (`/presence`): on, node id, key, URL → save.
The board needs Wi-Fi and a trusted clock; its log shows `presence node <id> … reports sent / failed (last HTTP …)`.
Settings live in `/presence.bin` (not in the main settings block, so older boards keep theirs).

## App settings
    GET  /v1/presence            -> {opt_in, required_dorm, counted, window_sec, key, status: uni|dorm|outside, place}
    POST /v1/presence {"opt_in": true|false, "rekey": true?}   (off also forgets the current non-dorm state)

## Android app
`android/app/src/main/java/org/meshmsg/app/hamyar/`, reached from Settings → Hamyar:
- `HamyarApi`: login on api.hamyared.ir (phone → SMS code → pick an account when there are several), bearer token kept
  in private prefs, refreshed when under a week is left; `/v1/presence` get/set and `/v1/presence/seen`.
- `PresenceRadio`, run by `MeshService` (so it works in the background): only while logged in, through the setup
  screen and counted (the server gives a key). Advertises "MP" + the phone token (non-connectable, low power, new
  token per window) and scans with a filter on 0xFFFF "MV" (allowed with the screen off); once per window posts the
  heard nodes, strongest first, at most 20.
- Screens: login, presence setup (once), "my presence" (status, zones, dorm rule, campus switch, what is recorded,
  radio state). Designs: UX Pilot page hamyared.ir, row y=10290.
- Risk to check on phones: the app declares `BLUETOOTH_SCAN` with `neverForLocation`, and Android may filter some
  beacon adverts out of scan results with that flag. If "MV" beacons never arrive, the Hamyar build needs the
  location form of the permission (node-hears-phone keeps working either way).

## Trip group (phone hears phone)
Same 16-byte layout with "MT", the trip group id in the id field and `HMAC(member key, "trip|<w>")[:8]`; the member
key comes from Hamyar `GET /v1/groups` once the student joins. Full protocol: Hamyar docs/presence.md, section 3.
The app queues heard tokens per trip (the server accepts windows up to 12 h old) and posts them to
`/v1/trips/<id>/group/heard`. Boards don't take part (yet).

## Next
- Hardware test: a board + two phones (dorm resident and opted-in), and 3 phones in one trip group.
- Hamyar branding as its own build (flavor), the Mava-side Hamyar features of docs/hamyar-app.md.
- Class attendance from classroom places + timetable (step 4).

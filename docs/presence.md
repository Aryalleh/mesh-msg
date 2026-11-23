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

## App settings
    GET  /v1/presence            -> {opt_in, required_dorm, counted, window_sec, key, status: uni|dorm|outside, place}
    POST /v1/presence {"opt_in": true|false, "rekey": true?}   (off also forgets the current non-dorm state)

## Next
- mesh-msg firmware: beacon mode + scanner mode with the layout above (ESP32 NimBLE), node id/key provisioning.
- Hamyar app (mesh-msg android flavor 'hamyar'): presence screen (on/off, status), background scan + advertise.
- Trips: "together / separated" from phone-to-phone sightings inside a trip group (step 3).
- Class attendance from classroom places + timetable (step 4).

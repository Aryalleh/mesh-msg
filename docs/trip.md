# Trip location sharing

For a group trip where people have no roaming or internet, e.g. a university group on the
Arbaeen walk (80 km from Najaf to Karbala): if someone gets lost, the trip's leaders can see
where they are. It works like AirTag reports: each member's phone sends its position every
few minutes, encrypted so only the leaders can read it, and every Mava device on the way
(phones over Bluetooth, LoRa boards, relays) carries the reports without being able to
read them. Parents stay in touch with ordinary messages through the trip's relay and the
leaders' roaming gateway; positions go only to the leaders.

## 1. Roles

- **Relay**: the trip's private relay (`server/`). Its admins (owner + the admins in
  `panel.json`, server.md §6b) are the only ones who can start a trip on it.
- **Leaders**: admins of that relay who lead the trip. They hold the trip's key and see every
  member's position, in the app and on the relay's web map.
- **Members**: everyone using that relay (or given the trip's link). Their phone asks them
  first; nothing is sent without a yes, and they can stop at any time.

## 2. The trip card

A trip has its own identity: X25519 + Ed25519, like an account. Only leaders hold its secret.
Members get a **trip card** signed with the trip's Ed25519 key (`core/trip`):

```json
{"card": {"v":1, "name":"Arbaeen 1405", "key":"<hex X||Ed of the trip>",
          "leaders":[{"id":"<account hash>","name":"Ali"}],
          "every":300, "ends":1800000000, "relay":"https://trip.example", "made":1799000000},
 "sig": "<hex Ed25519(domain 'mesh-msg trip v1' || card bytes)>"}
```

Checks: version 1, name 1–64 bytes, 1–16 distinct leaders, report interval 60–3600 s, ends
after `made` and within 90 days. A newer `made` replaces an older card of the same trip. The
same JSON goes into a link (`meshtrip:` + base64url) for a QR code.

## 3. What a member agrees to

The app shows the trip's name, its leaders, the interval and the end, and says in plain
words that only the leaders can read the positions (not other members, not the relay), and
that stopping is always possible and visible to the leaders. Only after "Share my location"
(and the system's location permission) does the phone send anything. While sharing:

- a notification says so, with a Stop button (a foreground service of type location runs only
  then);
- **I'm lost** (SOS): a report every minute, always on LoRa, and the leaders get an alarm;
- a leader can **ask for the position**: the phone reports now and every minute for 30
  minutes (only the trip's leaders may ask, only while the member shares);
- **Stop sharing** sends one last report flagged "stopped" (without a position);
- sharing stops by itself when the trip ends.

## 4. Reports on the wire

A report is an ordinary DATA packet to the trip's identity (`dest` = the trip's hash),
sealed with Noise X (protocol.md §3), content type **10** (`e2e.ContentTrip`). The member's
keys travel inside the ciphertext, so carriers see only "a packet for this trip".

```
content (kind 1, report): 01 | flags(1) | lat(4) | lon(4) | accuracy_m(2) | fix_age_s(2) | battery(1) | name_len(1) | name (≤ 24 B)
  lat, lon: signed 1e-7 degrees, big-endian; battery 255 = unknown
  flags: 1 SOS, 2 stopped, 4 answer (a leader asked), 8 no fix (lat/lon meaningless)
content (kind 2, locate, leader → member, one-to-one): 02 | trip_hash(16) | minutes(1)
content (kind 3, lead, leader → new leader, one-to-one): 03 | trip seed(64) | signed card JSON
```

A report is one LoRa frame (≤ 199 bytes with a 24-byte name). Ordinary reports carry
NO_LORA except one every 15 minutes; SOS reports, answers and the last report always may use
LoRa. A member's phone drops its previous report from its store when the next goes out;
phones that know the trip keep reports they carry for others only 6 hours.

**No receipts.** The trip is a *shared inbox* in the router (`Router.AddInbox`): its
messages are opened and delivered without a receipt (a receipt would purge the copies the
other leaders and the web map still need), and still stored and forwarded. Leaders announce
the trip's identity, so the relay lets them fetch its mailbox, like a group's.

## 5. The relay

`server/trips`, mounted by `meshmsg-relay`:

| Request | Who | What |
|---|---|---|
| `GET /v1/trip` | anyone | the current signed card, 404 if none or ended |
| `PUT /v1/trip` | a relay admin who is a leader in the card | sets the card; every leader must be an admin of this relay; an older card of the same trip is refused (409) |
| `DELETE /v1/trip?trip=<hash>` | a relay admin | ends the trip |

PUT and DELETE are signed by the account (`X-Account`, `X-Day`, `X-Signature` over
`MESHMSG-TRIP-PUT` / `MESHMSG-TRIP-DEL` and day ‖ SHA-256 of the card / the trip hash). The
card is kept in `data_dir/trip.json`. Apps ask each relay for its card every 10 minutes (2
after "no trip"), and at once when the trips screen opens.

The relay never holds the trip's key. Its **web map** (`/admin/trip`, for logged-in admins)
fetches the stored packets for the trip (`GET /admin/api/trip/reports`) and opens them in the
browser with the **map key** the leader pastes from the app (the trip's X25519 private key: it
can read reports, not sign cards or fetch the mailbox). The page (`server/admin/tripmap.html`,
`tripcrypto.js`) implements X25519, ChaCha20-Poly1305 and Noise X; `TestTripMapCrypto`
checks it against the Go core. Map tiles come from OpenStreetMap, so the page needs internet.

### "I'm lost" also opens a chat with the leaders

Besides the SOS reports, the member's phone sends its position to every leader as an ordinary
message and opens that chat, so the member can keep writing (`node.TripMessageLeaders`). The
card lists each leader's public key (`leaders[].key`, hex X||Ed, checked against the id), so a
member can write to leaders they never added; they are saved as contacts named "leader · trip".
The message is plain text any Mava shows (a board, the desktop):

```
📍 گم شده‌ام / I'm lost
32.616057, 44.024879 (±12 m)
Google Maps: https://www.google.com/maps/search/?api=1&query=32.616057,44.024879
Neshan: https://nshn.ir/?lat=32.616057&lng=44.024879
Balad: https://balad.ir/location?latitude=32.616057&longitude=44.024879&zoom=17
geo:32.616057,44.024879
```

The Android app draws such a message as a location card: **Navigate** asks which navigation
app to use (any app that opens `geo:` links: Google Maps, Neshan, Balad, OsmAnd…), and the
Google Maps / Neshan / Balad buttons open those links. Any chat can send one: attach →
Location (`LocationShare.kt`).

## 6. Leaders

- **Start a trip** (app → Trips): name, relay (one this account administers), interval
  (2/5/10/15 min), length. The app makes the trip key, signs the card and puts it on the relay;
  nothing is kept if the relay refuses.
- **QR code for members**: the `meshtrip:` link; members' phones also find the trip on the
  relay by themselves.
- **Add a leader**: a contact who is an admin of the relay. The card is re-signed and
  re-published, and the new leader gets the trip's secret in an end-to-end encrypted message
  (kind 3); their phone accepts it only from a listed leader, naming them as one.
- **Members**: last position, accuracy, time, battery, SOS/stopped, distance and direction from
  the leader's phone, "Navigate" (the user picks the navigation app), "Ask for position". The
  core keeps the last 200 points per member.
- **Map** in the app (`TripMap.kt`): every member's last position (red when lost, grey when
  old or stopped), their tracks, accuracy circles and the leader's own position; drag, pinch,
  tap a member for its details. Tiles come from OpenStreetMap and stay on the phone once seen;
  without them the map still draws members and tracks on a grid.
- **Key for the web map**: shown with the relay's map address.
- **End the trip**: deletes the card from the relay.

## 7. What it does not do (yet)

- A Mava board (no GPS) only carries reports; the handheld's GPS option could report too.
- meshd/desktop have the core API but no trips UI yet.
- iPhones: out of scope for v1 (android.md §1).

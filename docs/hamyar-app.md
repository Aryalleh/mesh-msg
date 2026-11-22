# Hamyar inside Mava (design)

[Hamyar](https://panel.hamyared.ir) is a platform for universities and student clubs: trips
(registration, instalments, payments), surveys, tickets. Each university on Hamyar now also has
its **own Mava relay**, and a Hamyar user can **link their Mava account**. This document
describes what Hamyar already does on its side and proposes the Mava side: a "Hamyar" screen
in the Android app.

Mava's identity rule stays as it is (identity = public key, no phone numbers, no accounts).
The phone number and OTP login stay in Hamyar; Mava only ever sees a public key.

## 1. Already built on the Hamyar side

### Relay per university

`https://<slug>-mava.hamyared.ir` is this repository's **edge relay** (`edge/src`, vendored
unchanged at a pinned commit) running in a Cloudflare Durable Object per university, with its
own SQLite store (the same `items` / `kv` tables as `store-d1.js`). Maintenance (expiry,
federation) runs from the object's alarm every minute instead of a cron trigger, and stops
after a quiet day. Same limits as any edge relay (docs/edge-relay.md): no media files, no UDP.

- **Relay admins** = the university's Hamyar admins who linked a Mava account (plus the network
  owner, as in every relay). Hamyar pushes the list whenever an admin links, unlinks or is
  added/removed. Only they can start a trip on the relay (`PUT /v1/trip`, trip.md §5).
- **Discovery**: `GET /.well-known/hamyar.json` on the relay host:

  ```json
  {"v":1, "university":"iribu", "name":"…", "site":"https://iribu.hamyared.ir",
   "api":"https://iribu.hamyared.ir/api/mava/v1", "link":"https://iribu.hamyared.ir/mava"}
  ```

### Linking a Mava account

1. The user logs in to Hamyar (phone + OTP, as always) and opens `/mava`.
2. They add the university's relay to Mava and paste their account id (Mava → Me).
3. Hamyar asks the relay to send a 6-digit code to that account: an ordinary DATA packet from
   the relay's own identity, sealed with Noise X (protocol.md §3), content kind 0 (text), the
   same way the relay sends admin login codes (`edge/src/admin.js` `sendCode`). The relay must
   already know the account's keys (from its announce), otherwise Hamyar asks the user to open
   Mava with the relay in its list first.
4. The user types the code into Hamyar (10 minutes, 5 tries, one code per minute). Hamyar stores
   the account id on the user (`users.mava_id`, unique per university).

Tested end to end with a scripted client built from `edge/src` (announce → code in the mailbox →
Noise X open → verify).

### Trips

A leader starts the trip in Mava on the university's relay (unchanged trip.md flow). In
Hamyar's admin page "رله و سفر ماوا" an admin attaches the relay's current signed card to a
Hamyar trip; registered members then see **join in Mava** (`meshtrip:` + base64url of the signed
card JSON) on `/mava`, and the admin can send them a Bale message pointing there. Hamyar never
holds the trip's key and never sees positions.

### API for a Mava client

Base: the `api` from `hamyar.json`. Every request is signed by the Mava account, with the same
scheme as trip requests (`edge/src/trips.js` `tripSigner`) and Hamyar's own domain:

```
X-Account:   hex(X25519 public || Ed25519 public)   (64 bytes)
X-Day:       days since 1970-01-01 (UTC), ±1 day accepted
X-Signature: hex(Ed25519(domain "HAMYAR-API-V1" || u32be(day) || SHA-256("<METHOD> <path>")))
```

| Request | Answer |
|---|---|
| `GET /me` | `{university:{slug,name}, user:{name, student_code, kyc_level}}` |
| `GET /trips` | `{trips:[{id, name, date, status, trip_card}]}` (`trip_card` = the signed card JSON or null) |
| `GET /payments` | `{installments:[…], pay_url}` (payment itself stays on the web, in the browser) |

`401 {"error":"signature"}` for a bad or old signature; `404 {"error":"not_linked","link":…}`
when the account isn't linked to anyone at that university.

## 2. Proposed: the Hamyar screen in Mava (Android)

Nothing below is built yet.

- **Where**: Settings → Relays: a relay whose `/.well-known/hamyar.json` answers gets a
  "Hamyar" badge. A new tab or a Me-screen card "Hamyar · <university>" appears when at least
  one such relay is in the list.
- **Not linked** (`/me` → 404): one button that opens `link` in the browser, with the account id
  copied to the clipboard. No phone number is typed into Mava.
- **Linked**: name and university; **My trips** from `/trips`. A trip with a `trip_card` gets
  "Join" (the existing trip consent screen of trip.md §3, fed with the card, so nothing is shared
  without the user's yes) and "Open in Hamyar" (browser). **Payments** shows the instalments and
  opens `pay_url` in the browser.
- **Signing**: in `core` (Go), next to the trip request signer (`core/relayclient/trip.go`): a
  `HamyarGet(path)` that adds the three headers with the account's key. Requests go straight to
  the Hamyar site over HTTPS; they don't travel through the mesh (the data is the user's own and
  the site needs the internet anyway). Offline, the screen shows the last answer kept on the
  device.
- **Several universities**: one card per Hamyar relay in the list.
- **Release**: the usual beta with release notes; no protocol change, no new content type, no
  relay change (the relay code runs unchanged inside Hamyar).

## 3. Open questions for the owner

1. Should a Hamyar relay federate with eu-1 by default (needs a sync token from eu-1), or stay
   private to the university? Hamyar can set peers per university.
2. Should the Hamyar screen appear for every Mava user who adds such a relay, or only after
   `/me` answers (linked users)?

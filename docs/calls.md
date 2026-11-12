# Voice calls

Status: **requirement + design, not implemented yet.** Builds on the direct IP link of
[connectivity.md](connectivity.md), which is not implemented yet either.

## 1. Rules

- One-to-one voice calls between contacts. Group calls and video are out of scope for v1.
- Audio travels **only over a direct path** between the two devices: the direct IP link
  (any NAT topology, connectivity.md), LAN, or Wi-Fi Direct / Wi-Fi Aware. The path is
  chosen by tested reachability; on a shared local network the call stays local.
- **No relay, no TURN.** No relay, gateway or bridge carries call audio. If no direct path
  is found, the call does not start and the app says so ("no direct connection — send a voice
  message instead").
- Never over LoRa. Bluetooth (BLE) is not used for audio in v1 (throughput and latency vary
  too much between phones); it can still carry the signaling.
- Both devices must be online at the same time; a call is not store-and-forward.

## 2. Signaling

Call setup is ordinary end-to-end-encrypted messages, inner content type **7**
(`ContentCall`), sent over **any** path that works (relay, LAN, BLE, ...). Relays see one
more ciphertext packet and don't know it is a call.

```
call    = call_id(16) | op(1) | body
op 0 OFFER      expires(4) | caps | candidates      caller → callee, rings
op 1 ANSWER     caps | candidates                   callee accepted
op 2 CANDIDATES candidates                          more candidates later (network change)
op 3 REJECT     reason(1)                           0 declined, 1 busy, 2 not a contact
op 4 END        reason(1)                           0 hang-up, 1 no direct path, 2 timeout, 3 error
op 5 CANCEL                                         caller hung up before an answer
caps       = codec list (Opus first) | max bitrate
candidates = n(1) | n × (kind(1) | family(1) | addr | port(2) | priority(4))
```

- An OFFER rings only until `expires` (45 s after sending). One that arrives later (the
  callee was offline and got it from a relay) is shown as a **missed call**, never rings.
- Calls are accepted only from contacts by default (setting: "calls from anyone").
- Busy: an OFFER while in a call gets REJECT busy (the user sees a waiting call).
- After ANSWER both sides run the connectivity checks of connectivity.md §4; the call id
  and the Noise session of the signaling key the probe MACs.

## 3. Media session

- **Key:** a fresh Noise KK handshake over the chosen path (both static keys are known from
  the contact), with new ephemeral keys per call, so a call has forward secrecy and is bound
  to both identities. The UI shows a short code derived from the handshake hash that both
  people can compare by voice.
- **Packets** (UDP; TCP framed with `len(2)` when only TCP worked):

  ```
  frame = seq(4) | timestamp(4) | flags(1) | opus_payload        sealed with ChaCha20-Poly1305,
                                                                 nonce = direction(1) | seq
  ```

  Keepalive frames (empty payload) every 2 s while muted or in silence keep NAT mappings.
- **Codec:** Opus, 16 kHz wideband, 20 ms frames, voice mode; bitrate adapts 6–24 kbps from
  measured loss and RTT; in-band FEC on when loss > 2 %; DTX in silence. ~2–3 KB/s each way.
- **Jitter buffer:** adaptive 40–200 ms; packet loss concealment from Opus.
- **Path changes:** when the network changes (Wi-Fi ↔ mobile, hotspot toggled), candidates
  are sent again (op 2), checks rerun, and audio moves to the new best pair without ending
  the call. If no path is found within 10 s, the call ends with reason 1.

## 4. Platforms

- **Android** (first): `AudioRecord` with `VOICE_COMMUNICATION` (hardware echo
  cancellation / noise suppression where present), `AudioTrack` in voice-call mode, libopus
  through the NDK (minSdk 26, so not MediaCodec), foreground service type
  `phoneCall|microphone`, self-managed `ConnectionService` so calls behave like calls
  (lock screen, Bluetooth headset, other apps pause) without Play Services. Signaling,
  connectivity checks and encryption live in the Go core; Kotlin owns audio and UI.
- **Desktop** (later): `meshd` handles signaling and the path; audio in the web UI through
  WebAudio over the local socket.

## 5. Privacy

- A direct call reveals each side's IP addresses (local and public) to the other person.
  For someone in a censoring country that can point to their network and location. Hence:
  calls only from contacts by default, and the first call screen says that the other person
  will see your network address.
- Relays never see audio. They see the encrypted signaling packets (like any message) and,
  when used as a reflector, the address of the probe.
- No call logs leave the device; the call history is stored like messages.

## 6. Tests that must exist

- Every topology test of connectivity.md §6, with a call: audio flows, relay byte counters
  show zero audio bytes.
- Same LAN / same hotspot: audio stays on the local subnet.
- Symmetric NAT on both sides: call ends with "no direct path", nothing relayed.
- 10 % and 30 % packet loss, 300 ms jitter: call stays up, bitrate drops, FEC on.
- Network change mid-call: audio resumes within 2 s.
- OFFER delivered late (callee was offline): shown as missed, does not ring.
- Busy, reject, cancel, call from a non-contact (rejected by default).

# Voice calls

Status: **requirement + design, not implemented yet.** Builds on the direct IP link of
[connectivity.md](connectivity.md), which is not implemented yet either.

## 1. Rules

- One-to-one voice calls between contacts. **v1 ships audio only, but every format and
  module is built for more streams** (§7): video can be added later without a new protocol,
  and v1 clients keep working with clients that have video. Group calls are out of scope.
- Audio travels **only over a direct path** between the two devices: the direct IP link
  (any NAT topology, connectivity.md), LAN, or Wi-Fi Direct / Wi-Fi Aware. The path is
  chosen by tested reachability; on a shared local network the call stays local.
- **No relay, no TURN** — with one exception: any relay (private or official) can give
  TURN to its own admins only (connectivity.md §7). Otherwise no relay, gateway or
  bridge carries call audio. The apps use every way to a direct path (IPv6, router port
  mapping, hole punching, symmetric-NAT port prediction, connectivity.md §3a–§4a). If none
  works, the call does not start and the app says why ("both of you are behind your
  carriers' NAT — try Wi-Fi") and offers a voice message instead.
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
op 6 UPDATE     caps                                during a call: add / remove streams
                                                    (camera on/off); answered by UPDATE
op 7 CONTROL    stream_id(1) | kind(1) | body       0 keyframe request, 1 bitrate hint,
                                                    2 mute state, 3 stream paused
caps       = version(1) | n(1) | n × stream_cap
stream_cap = stream_id(1) | media(1) | dir(1) | codecs | max_kbps(2) | params_len(1) | params
             media: 0 audio, 1 video, 2 screen, 3 data; dir: send / receive / both
             codecs: n(1) | n × codec_id(1)  (Opus = 1; VP8 = 16, H.264 = 17, VP9 = 18, AV1 = 19)
             video params: max width(2) | max height(2) | max fps(1)
candidates = n(1) | n × (kind(1) | family(1) | addr | port(2) | priority(4))
```

**Upgrade rules** (so old and new clients always interoperate):

- Unknown ops, unknown `media` kinds, unknown codec ids and trailing bytes are ignored,
  never an error. `version` is the highest call version the sender speaks; v1 = 1.
- The ANSWER lists only the streams the callee accepts. A stream missing from the ANSWER is
  off. An audio-only v1 client answering a video call simply leaves video out: the call
  runs as voice, the caller's UI says "video not supported by the other app".
- Stream ids are chosen by the offerer; 0 is always the main audio stream.

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
  frame  = stream_id(1) | seq(4) | timestamp(4) | flags(1) | [frag] | payload
  flags  = keyframe | fragmented | fec | end-of-frame | 4 bits reserved (ignored)
  frag   = frag_index(2) | frag_count(2)                      only when fragmented
  sealed with ChaCha20-Poly1305, nonce = direction(1) | stream_id(1) | 0(6) | seq(4)
  ```

  Each stream has its own sequence numbers. Datagrams stay ≤ 1200 bytes; anything bigger
  (a video frame) is split into fragments. Audio never needs fragments.

  Keepalive frames (empty payload) every 2 s while muted or in silence keep NAT mappings.
- **Codec:** Opus, 16 kHz wideband, 20 ms frames, voice mode; bitrate adapts 6–24 kbps from
  measured loss and RTT; in-band FEC on when loss > 2 %; DTX in silence. ~2–3 KB/s each way.
- **Jitter buffer:** adaptive 40–200 ms; packet loss concealment from Opus.
- **Bandwidth:** one estimator per call (delay- and loss-based, from per-stream receiver
  reports in CONTROL) gives a total budget; audio is served first, whatever is left goes to
  other streams. With audio only the estimator just drives the Opus bitrate.
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

## 4a. Code structure (built for more streams)

```
core/call/
  session.go    signaling state machine, caps negotiation, UPDATE/CONTROL
  transport.go  direct path (from core/direct), encryption, framing, fragmentation
  stream.go     interface Stream { ID; Media; Codec; OnFrame; Send; Stats }
  bwe.go        bandwidth estimator + budget split between streams
  audio/        jitter buffer, Opus parameters (v1)
  video/        empty in v1: frame assembler, keyframe requests, layer for video codecs
```

Platform code only supplies sources and sinks (`MediaSource`, `MediaSink`: microphone,
speaker; later camera, display surface, encoder/decoder). The core handles everything in
between, so adding video means adding a source, a sink and a codec, not changing the
protocol, the transport or the signaling. On Android, video would use hardware
`MediaCodec` (H.264 / VP8) with a software VP8 fallback.

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
- Forward compatibility: a test client that offers an extra video stream, unknown ops,
  unknown codec ids and reserved flag bits against the v1 client: the call runs as voice.
- Fragmentation: frames up to 64 KB split and reassembled under loss (ready for video).

## 7. Adding video later (checklist)

1. Camera source + display sink on Android; `video/` assembler and keyframe logic in core.
2. Advertise a `media = 1` stream in caps; turn it on and off with UPDATE.
3. Budget: video gets what the estimator leaves after audio (typically 150–800 kbps).
4. Privacy note: video uses far more data; warn on mobile data.
No change to signaling ops, frame format, encryption or path discovery is needed.

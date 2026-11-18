# Voice calls

Status: voice calls implemented on Android (alpha); video added in beta (§7a).

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

- **The Mava board** (firmware, voice; video with MJPEG next): `firmware/lib/meshcall` is
  the same protocol in C++ — signals, core/direct's agent (STUN on the relay host's UDP
  3478/3479, NAT type, probes, hole punching, port prediction for symmetric NATs, the sealed
  link), Noise KK, frames and the jitter buffer — host-tested on a simulated network with
  NATs and against the Go code (`TestBoardPeer`, `make -C firmware callpeer`). `src/calls.cpp`
  runs it in its own task with a UDP socket on Wi-Fi (or the board's hotspot) and Opus at
  16 kHz in 20 ms frames (libopus, fixed point); signals travel as content type 7 through the
  board's messaging client (any path: Bluetooth, the relay, USB). The board offers and
  accepts Opus only, so a phone without Opus (Android before 10) can't call it. Audio:
  the WM8960 codec, and before it the Mava Screen app over USB (screen.md "Audio"). Calls
  from saved contacts only; a call needs Wi-Fi and the board's clock (OFFER expiry).
  Screen: incoming (📞 answer, ⛔ decline), calling, talking (code, path, mute, hang up).

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

## 7. Implementation (alpha)

- `core/call`: signals as in §2 (the OFFER also carries the 32-byte path-check key, and
  `candidates` is core/direct's offer: NAT type first, then the candidate list), the state
  machine (outgoing → ringing → connecting → active → ended), the path from core/direct,
  Noise KK on the path (datagram types 1/2 handshake, 3 frames), frames sealed with the
  Noise transport keys (nonce = `stream_id << 32 | seq`), a 6-digit code from the handshake
  hash, keepalive frames every 2 s, end after 20 s without frames, and the jitter buffer
  (40–200 ms target from RFC 3550 inter-arrival jitter, loss → conceal, underrun →
  rebuffer, skip when far behind).
- Codecs: Opus (1) where the phone encodes it through MediaCodec (Android 10+), AMR-WB (2)
  everywhere (every Android phone with a microphone encodes it, both decode everywhere). The
  callee takes the caller's first codec it also has. Lost frames play as silence. The playback
  track takes its sample rate from the decoder's output format (Android's Opus decoder
  always produces 48 kHz).
- `core/node`: `StartCall`, `AnswerCall`, `EndCall`, `MuteCall`, `CallAudio`,
  `PullCallAudio`, `call` events; setting `calls_from_anyone`. `mobile`: the same for Kotlin.
- Android: `CallAudio` (AudioRecord VOICE_COMMUNICATION + echo canceller + noise suppressor,
  MediaCodec, AudioTrack voice mode), `CallService` (foreground, type microphone, while a
  call is active), a ringing notification with a full-screen intent, the call screen (code
  to compare, mute, speaker, "through your relay" note) and a call button in chats. Not
  yet: path changes mid-call (op 2), bitrate adaptation, system call log integration.


## 7a. Video (implemented, beta)

- **Negotiation:** the offer always carries a video stream (id 1, media 1, codec H.264 = 17)
  when the phone has an H.264 encoder and decoder, so the camera can be turned on during a
  voice call too. Params `width(2) | height(2) | fps(1) | flags(1)`, flag bit 0 = the caller
  starts with the camera on ("video call": the callee sees "Incoming video call"). The callee
  keeps the stream only with a common codec; an audio-only app leaves it out and the call
  runs as voice (`TestVideoOfferAnsweredWithoutVideo`). `Info.video`, `video_codec`, `video_call`.
- **Frames** (`core/call/video.go`): each encoded frame goes as datagrams on stream 1 with
  their own seq and the frame's timestamp; payload `frag_index(2) | frag_count(2) | bytes`
  inside the sealed part; flags fragmented, keyframe, end. Up to 512 KB per frame.
- **Receiver:** reassembles by timestamp. An older frame left incomplete when a newer one
  completes, a frame stuck > 800 ms, or a full queue breaks the decoder's chain: frames are
  dropped until the next keyframe, and a keyframe is asked for (at most every 400 ms).
- **In-band control** on stream 255 (`kind(1) | stream(1) | body`), which voice-only apps
  ignore: 0 keyframe request, 1 receiver report (loss ‰ over the last second, every second).
  The sender's target bitrate starts at 500 kbps, ×0.7 above 10 % loss, ×0.9 above 3 %,
  ×1.08 below 2 %, within 120–2000 kbps; the app reads it with `CallVideoNeeds` and sets
  its encoder.
- **Android** (`CallVideo`): CameraX ImageAnalysis (YUV, ~640×480, 20 fps) → hardware H.264
  encoder (CBR, keyframe every 2 s and on request) → `orientation(1) | Annex-B access unit`
  with SPS/PPS before every keyframe. Receive: decoder set up from the first keyframe's
  SPS/PPS, drawing on a TextureView rotated by the sender's orientation and cropped to fill
  the screen; the remote view shows while frames arrive (avatar after 1.5 s without).
  Camera only while the call screen is in front; self-view in the corner; camera on/off and
  front/back buttons; "Video call" button in chats; a data-use note on metered networks.
- Not yet: desktop (web UI), VP8 fallback for phones without H.264, simulcast/resolution
  steps (only the bitrate adapts), sending the camera while the app is in the background.

## 7b. MJPEG video for small devices (the Mava board)

- **Codec 20 = MJPEG**: every frame is one baseline JPEG on its own (`orientation(1) |
  JPEG`, orientation 0: the sender turns the picture upright), sent as a keyframe in the
  usual fragments. Phones list it after H.264 (`17,20`; only `20` without an H.264 codec),
  so phones keep H.264 between themselves and use MJPEG with a board.
- **The board takes video in and sends none.** Its offer and its answer carry a video
  stream with dir = receive, codec 20, params 320 × 240 at 10 fps, camera off: the phone sees
  a voice call and can turn its camera on at any time; a phone's video call to the board is
  answered with the same stream. A video offer without MJPEG is answered as voice.
- **Phone side** (`CallVideo`, MJPEG): CameraX RGBA frames near 320 × 240, turned upright
  and scaled to fit 320 × 240, JPEG at 10 fps; the quality (25–80) follows the bitrate from
  the board's loss reports (frame size against kbps / 8 / fps). JPEGs that come in are drawn
  on the remote view's Surface.
- **Board side**: `lib/meshcall` reassembles one frame at a time into a 64 kB buffer (a
  newer frame or 800 ms ends an unfinished one; bigger frames are dropped), sends a loss
  report every second on stream 255, and hands whole frames to `src/calls.cpp`, which keeps
  the newest for the UI (triple buffer). The call screen decodes it with JPEGDEC into a
  320 × 240 RGB565 image behind the text and buttons; it disappears 2 s after the camera
  stops. Tests: `test/callmgr_test.cpp` (video in, loss, too big, H.264-only offer) and
  `TestBoardPeer` (the Go phone code sends frames to the board, both ways round).

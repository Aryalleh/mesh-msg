# Direct IP connections and path discovery

Status: **requirement + design, not implemented yet.** Today two devices talk over IP in
two ways: the LAN interface (UDP multicast discovery + TCP, one subnet only) and relays
(store-and-forward). This document covers the direct link between two devices across any
IP topology. Messages, media blobs (protocol.md §6b) and later live traffic use it.

## 1. Requirement (binding)

> Network topology must be treated as arbitrary. Clients may exist behind multiple layers of
> NAT, CGNAT, mobile networks, hotspots, routers, or nested private LANs. The implementation
> must not assume that two private IPs are directly reachable, nor that a public IP implies
> direct reachability. Connectivity must be tested dynamically and the most direct reachable
> path must be preferred. If both clients are actually reachable through the same local
> network, traffic must remain local and must never be unnecessarily routed through the
> signaling server. The signaling server must never become a media relay. TURN/relay fallback
> is explicitly out of scope.

Topologies this must handle (examples, not a closed list):

```
Internet ── carrier CGNAT ── Phone A (mobile data) ── hotspot ── Phone B, Phone C
Internet ── CGNAT ── mobile device ── NAT ── hotspot ── client
Internet ── ISP router ── home router (double NAT) ── PC
two unrelated networks that both use 192.168.1.0/24
```

Phone B and Phone C on A's hotspot must talk to each other inside the hotspot. Phone A
must reach B and C on its hotspot interface, not through the internet. None of these
decisions may come from the address class (`10/8`, `172.16/12`, `192.168/16`, `100.64/10`,
public, IPv6 ULA/global): only a successful, authenticated check proves a path.

**Amendment (owner's decision):** the one exception to "no TURN" is a **relay serving its
own admins** (§7). Any relay may offer it — private, regional, global or official (origin) —
but only to the accounts in its own admin list; nobody else gets it. For everyone, every method of §3a–§4 must be built into the apps, so that a
direct path is found as often as possible.

## 2. Roles

- **Signaling** = exchanging candidates. It is an ordinary end-to-end-encrypted message
  (new content type), sent over any path that already works: relay, LAN, BLE, even LoRa if
  small enough. The relay sees ciphertext only and does not know it is signaling.
- **Reflector**: a relay may answer a UDP probe with the source address and port it saw
  (STUN-like, no state), on UDP 3478 and 3479 on every relay (server.md §6d). This only tells a client its outer mapping. It never forwards
  anything to another client.
- **No TURN, no relaying of a direct session.** No component forwards bytes of a direct
  session on behalf of the two peers. The existing store-and-forward message path stays as
  it is; it is not a fallback for a direct session and is not used to carry one.
- **Media has two classes** (protocol.md §6b, server.md §6a). *Normal* media moves only over
  direct paths (this document, LAN, Wi-Fi Direct, Bluetooth), so both devices must be online
  at the same time. *Important* media is the one exception: the sender marks it, it goes
  through a relay, reaches the receiver even if they are offline, and each account has a
  small daily allowance of it. This is store-and-forward of a finished file, not a relay for
  a live session.

## 3. Candidates

Each side gathers, on every network change:

| Kind | Source |
|---|---|
| host | every address of every up interface: Wi-Fi, mobile data, hotspot/tethering, Ethernet, VPN, IPv4 and IPv6 |
| reflexive | the outer address:port a reflector saw (one per interface that reaches one; several reflectors so a mapping that changes per destination is detected) |
| peer-reflexive | an address a peer saw our probe come from during checks |
| local-discovered | addresses heard from multicast/broadcast discovery on each interface |
| mapped | a port the device asked its router to open (UPnP IGD, NAT-PMP or PCP, §3a) |
| predicted | ports guessed for a NAT that changes ports per destination (§4a) |
| relayed | TURN allocation on a relay, **only for that relay's own admins** (§7) |

All candidates are sent; none is dropped because it "looks private" or "looks
unreachable". Port mappings are kept open by the probe socket itself (one UDP socket per
interface for gathering and for the session).

## 3a. Opening and classifying the path

- **IPv6 first.** Mobile carriers often give phones global IPv6 with no NAT, only a
  stateful firewall that the simultaneous probes of §4 open. Global IPv6 host candidates are
  gathered and checked on every interface that has one, alongside IPv4.
- **Router port mapping.** On every interface, ask the gateway router for a port mapping:
  PCP, then NAT-PMP, then UPnP IGD (SSDP discovery). A granted mapping is a `mapped`
  candidate. Mappings have a short lease (the session's length, renewed), are removed when
  the session ends, and are never kept open in the background. Works on home routers, not
  on carrier CGNAT.
- **NAT type.** Each interface asks at least two reflectors on different addresses from the
  same socket. Same outer port for both → endpoint-independent mapping (hole punching
  works). Different ports → symmetric; the difference between them (+1, +2, …) says whether
  ports are allocated in sequence (predictable) or at random. The type is sent with the
  candidates, chooses the method of §4a, and is shown in diagnostics.

**As built:** besides the session socket on all interfaces, `core/direct` opens one UDP
socket per local address and offers each as a host candidate. On Android the app binds
each of these to the network that owns the address (`Network.bindSocket`, through
`mobile.NetworkBinder`): Android routes by network, not by destination, so an unbound socket
sends through the default network (mobile data when the Wi-Fi has no internet) and would
never reach a phone on the same Wi-Fi. Between equal paths the bound socket wins.

## 4. Connectivity checks

- Every local candidate is paired with every remote candidate of the same family. Both sides
  start checks at the same time (this is the UDP hole punch); each sends probes on every
  pair, repeats with backoff for ~10 s, answers every probe it receives.
- Probes are **authenticated**: they carry a session nonce and a MAC keyed from the Noise
  session of the signaling exchange. A reply from the wrong device at the same private
  address (192.168.1.10 on two different networks) fails the MAC and does not count.
- A pair succeeds when an authenticated probe and its reply both went through. The source
  address of a received probe becomes a peer-reflexive candidate and is checked too.
- TCP (simultaneous open) is tried on host candidates only, for networks that drop UDP.

## 4a. Getting through symmetric NAT

Run only when the NAT-type check says a side is symmetric and normal checks found nothing:

- **Sequential ports:** the other side probes the next ports after the last observed
  mapping (observed + k × step, k = 1…64).
- **Random ports (birthday method):** the symmetric side opens 256 UDP sockets and probes
  the other side's reflexive address from all of them; the other side (if it is
  endpoint-independent) sends to random ports of the symmetric side's outer address, up to
  2048 probes paced at ~100/s. With 256 open mappings the chance of a hit within 2048 tries
  is high. If both sides are symmetric, both open 256 sockets and probe random ports of each
  other; success is lower and is attempted for at most 20 s.
- Extra sockets are closed as soon as one pair succeeds. Probes stay authenticated, so a
  stray reply can't be mistaken for the peer. The pacing keeps it from looking like a scan
  to carrier equipment.

## 5. Choosing the path

Among pairs that succeeded, in order:

1. pair where both addresses are host or local-discovered candidates (the traffic stays on
   the shared local network, however many NATs are above it),
2. global IPv6 ↔ global IPv6 (no NAT on the way),
3. pairs involving mapped or peer-reflexive candidates,
4. reflexive ↔ reflexive (crosses the internet through both NATs),
5. predicted ports (§4a),
6. relayed (§7) — last, and only for an admin of a relay with TURN on,

ties broken by measured RTT. Checks keep running for a short time after the first success,
so a better pair found later replaces it. On a network change (interface up/down, hotspot
toggled, IP changed) candidates are gathered again and checks repeat; the session moves to
the new best pair without dropping. Keepalives every 15 s hold NAT and CGNAT mappings.

If no pair succeeds, the result is **no direct path**: the session is not established, and
the UI gives the reason from the NAT types ("both of you are behind your carriers' NAT —
try Wi-Fi", "the other side's network blocks UDP", …). There is no relayed fallback, except
§7 for a relay's own admins.

## 6. Tests that must exist

Simulated with network namespaces (Linux, `sim/` or a script like `scripts/e2e-lora.sh`):

- same LAN: path is host ↔ host, and the relay's byte counters show zero session bytes;
- phone hotspot behind CGNAT, two clients on the hotspot: stays on the hotspot subnet;
- device providing the hotspot ↔ client on it: uses the hotspot interface;
- double and triple NAT, cone mappings: hole punch succeeds over reflexive candidates;
- IPv6-only path between two IPv4-CGNAT networks: chosen over IPv4;
- home router with UPnP / NAT-PMP / PCP: mapped candidate used, mapping removed afterwards;
- NAT type detection: endpoint-independent, sequential symmetric, random symmetric;
- one side symmetric (random ports): birthday method succeeds; sequential: prediction succeeds;
- symmetric NAT on both sides: either connects via §4a or fails cleanly with the reason,
  nothing relayed (except §7);
- admin of a relay with TURN on (private and origin relay alike): relayed only when
  everything else failed; the same call by a non-admin of that relay gets no TURN;
- two separate networks with the same private subnet and same host address: no false match;
- network change mid-session: moves to the new best path;
- the relay never forwards a session byte in any scenario except §7 (counter assertion).

## 7. TURN on relays, for their own admins only

Every relay — private, regional, global or official (origin) — can serve **its own admins**
(the accounts in that relay's admin list, server.md §6b) with TURN, so the people who run a
relay can always reach each other. Ordinary users never get TURN from any relay.

- Off by default; the relay's admins turn it on in the panel.
- The network owner's account is an admin of every relay (server/deploy/README.md), so it
  can use TURN on every relay that has it on.
- Built into `meshmsg-relay` (standard TURN, RFC 8656, UDP and TCP/TLS on 443). Credentials
  are short-lived (10 min) and issued only to an admin: `POST /v1/turn/credentials`, signed
  with the account key. Non-admins get `403`.
- One side being an admin is enough: the admin's app allocates and the other side sends to
  the relayed address. The app uses it only after every method above failed.
- Limits in the panel: `turn_max_sessions`, `turn_max_kbps` per session, and a monthly
  traffic cap; the panel shows live sessions and bytes.
- The relay sees both sides' IP addresses and traffic volume and timing, never content
  (the session is end-to-end encrypted above TURN). The app tells the admin when a call goes
  through their relay.

## 8. Implementation (alpha)

- `core/direct`: gathering (host, reflexive from every relay's 3478/3479, router port mapping
  by PCP → NAT-PMP → UPnP, TURN for admins), NAT type, authenticated probes
  (`"MDP"|1|sid|type|tx|[seen addr]|HMAC-16`, a role bit so a device never matches its own
  probe on a same-subnet network), triggered peer-reflexive checks, port prediction and the
  birthday method, choice by §5 class then RTT, and `Link` (ChaCha20-Poly1305 datagrams ≤
  1200 bytes, key per direction, replay window, keepalive 15 s, follows the peer's new
  address). An in-memory NAT simulator (`simnet_test.go`) tests the cases of §6.
- Signaling is inner content type **8** (`ContentDirect`), from saved contacts only:

  ```
  signal = version(1)=1 | op(1) | sid(16) | body
  op 0 OFFER   expires(4) | key(32) | offer      offer = nat(1) | step(2) | last addr | candidates
  op 1 ANSWER  offer
  op 2 CLOSE   reason(1)                         0 done, 1 no path, 2 refused
  ```

  The 32-byte session key is random and travels inside the end-to-end encrypted message
  (instead of being derived from the Noise session); it keys the probes and the link. An
  OFFER past `expires` (60 s) is ignored. When both sides start at once, the smaller
  account hash's OFFER wins.
- `core/node`: the chosen path is a router link named `direct` to that one contact; router
  packets are split into ≤ 1172-byte datagrams (`id(4) | index(1) | count(1)`). A normal
  file to download opens a link to its sender automatically (at most every 2 min per
  contact); media frames over it are 1100 bytes with 3 s re-requests. Settings:
  `[direct] off`, `no_port_mapping`, `reflectors`.

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

## 2. Roles

- **Signaling** = exchanging candidates. It is an ordinary end-to-end-encrypted message
  (new content type), sent over any path that already works: relay, LAN, BLE, even LoRa if
  small enough. The relay sees ciphertext only and does not know it is signaling.
- **Reflector**: a relay may answer a UDP probe with the source address and port it saw
  (STUN-like, no state). This only tells a client its outer mapping. It never forwards
  anything to another client.
- **No TURN, no relaying of a direct session.** No component forwards bytes of a direct
  session on behalf of the two peers. The existing store-and-forward message path stays as
  it is; it is not a fallback for a direct session and is not used to carry one.

## 3. Candidates

Each side gathers, on every network change:

| Kind | Source |
|---|---|
| host | every address of every up interface: Wi-Fi, mobile data, hotspot/tethering, Ethernet, VPN, IPv4 and IPv6 |
| reflexive | the outer address:port a reflector saw (one per interface that reaches one; several reflectors so a mapping that changes per destination is detected) |
| peer-reflexive | an address a peer saw our probe come from during checks |
| local-discovered | addresses heard from multicast/broadcast discovery on each interface |

All candidates are sent; none is dropped because it "looks private" or "looks
unreachable". Port mappings are kept open by the probe socket itself (one UDP socket per
interface for gathering and for the session).

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

## 5. Choosing the path

Among pairs that succeeded, in order:

1. pair where both addresses are host or local-discovered candidates (the traffic stays on
   the shared local network, however many NATs are above it),
2. pairs involving peer-reflexive candidates,
3. reflexive ↔ reflexive (crosses the internet through both NATs),

ties broken by measured RTT. Checks keep running for a short time after the first success,
so a better pair found later replaces it. On a network change (interface up/down, hotspot
toggled, IP changed) candidates are gathered again and checks repeat; the session moves to
the new best pair without dropping. Keepalives every 15 s hold NAT and CGNAT mappings.

If no pair succeeds (for example symmetric NAT on both sides), the result is **no direct
path**: the session is not established and the UI says so. There is no relayed fallback.

## 6. Tests that must exist

Simulated with network namespaces (Linux, `sim/` or a script like `scripts/e2e-lora.sh`):

- same LAN: path is host ↔ host, and the relay's byte counters show zero session bytes;
- phone hotspot behind CGNAT, two clients on the hotspot: stays on the hotspot subnet;
- device providing the hotspot ↔ client on it: uses the hotspot interface;
- double and triple NAT, cone mappings: hole punch succeeds over reflexive candidates;
- symmetric NAT on both sides: fails cleanly with "no direct path", nothing relayed;
- two separate networks with the same private subnet and same host address: no false match;
- network change mid-session: moves to the new best path;
- the relay never forwards a session byte in any scenario (counter assertion).

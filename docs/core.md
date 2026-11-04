# Shared Core (`core/`, Go)

One implementation of everything above the radio, used by:

| Consumer | How |
|---|---|
| `server/` relay | Go import |
| `desktop/` meshd | Go import |
| `android/` | `gomobile bind` → AAR, called from Kotlin |
| `sim/` simulator | Go import, real router in simulated time |

(Go on Android via gomobile is proven by censorship-circumvention and VPN apps such as
Psiphon and Tailscale.)

## Packages

```
core/
  packet/     outer header codec, fragments (must match spec/test-vectors)
  identity/   X25519 + Ed25519 keys, dest_hash, contact cards / QR URIs
  e2e/        Noise K / X (flynn/noise), inner payload, text encoding
  announce/   announces, receipts, delegations — sign & verify
  router/     route table, dedup, forwarding rules (protocol.md §6), Bloom summaries
  store/      packet store interface + SQLite impl, quotas, expiry
  iface/      Interface abstraction; concrete: relay client, LAN, LoRa-serial
  relayclient/ client / gateway / bridge sessions against server API
  clock/      injectable clock for the simulator
```

## Interface contract

```go
type Interface interface {
    ID() string
    Kind() Kind              // BLE, WIFI_DIRECT, LORA, LAN, RELAY
    MTU() int                // LoRa = 255
    Send(peer PeerID, pkt []byte) error   // peer == Broadcast for flood
    Recv() <-chan Received
    Neighbours() []PeerID
}
```

Android implements BLE and Wi-Fi Direct interfaces in Kotlin and registers them with the Go
router through a small gomobile-friendly callback API (byte slices in, byte slices out).

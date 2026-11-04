// Package wire defines the binary bodies of the relay HTTP API, shared by the
// server and every client (desktop, Android, bridges, other relays).
package wire

import (
	"encoding/binary"
	"errors"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// HTTP headers.
const (
	HeaderOrigin    = "X-Origin"    // caller label (gateway / bridge / relay name)
	HeaderRegion    = "X-Region"    // caller's region (informational)
	HeaderPoW       = "X-PoW"       // proof-of-work nonce (decimal)
	HeaderCursor    = "X-Cursor"    // response: next cursor
	HeaderMore      = "X-More"      // response: "1" if more items are pending
	HeaderNonce     = "X-Nonce"     // mailbox challenge
	HeaderSignature = "X-Signature" // mailbox challenge signature (hex)
	ContentType     = "application/x-meshmsg-frames"
)

// SyncItem is one packet in a federation/bridge sync, with the home region
// the source relay recorded for it (announces only; "" otherwise).
type SyncItem struct {
	Raw    []byte
	Region string
}

// EncodeSync encodes items as frames of region_len(1) || region || raw.
func EncodeSync(items []SyncItem) []byte {
	var out []byte
	for _, it := range items {
		r := it.Region
		if len(r) > 255 {
			r = r[:255]
		}
		body := append([]byte{byte(len(r))}, r...)
		body = append(body, it.Raw...)
		out = packet.AppendFrame(out, body)
	}
	return out
}

// DecodeSync reverses EncodeSync.
func DecodeSync(b []byte) ([]SyncItem, error) {
	frames, err := packet.DecodeFrames(b)
	if err != nil {
		return nil, err
	}
	out := make([]SyncItem, 0, len(frames))
	for _, f := range frames {
		if len(f) < 1 || len(f) < 1+int(f[0]) {
			return nil, errors.New("wire: bad sync item")
		}
		n := int(f[0])
		out = append(out, SyncItem{Region: string(f[1 : 1+n]), Raw: f[1+n:]})
	}
	return out, nil
}

// EncodeGateway encodes a gateway request body: the destinations the gateway
// serves, followed by the packets it uploads.
//
//	count(2) || count × dest_hash(16) || frames
func EncodeGateway(serve []packet.Hash, upload [][]byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, uint16(len(serve)))
	for _, h := range serve {
		out = append(out, h[:]...)
	}
	return append(out, packet.EncodeFrames(upload)...)
}

// DecodeGateway reverses EncodeGateway.
func DecodeGateway(b []byte) (serve []packet.Hash, upload [][]byte, err error) {
	if len(b) < 2 {
		return nil, nil, errors.New("wire: short gateway body")
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < n*16 {
		return nil, nil, errors.New("wire: short gateway serve list")
	}
	serve = make([]packet.Hash, n)
	for i := range serve {
		copy(serve[i][:], b[i*16:])
	}
	upload, err = packet.DecodeFrames(b[n*16:])
	return serve, upload, err
}

// Info is returned by GET /v1/info.
type Info struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	PowBits int    `json:"pow_bits"`
	Version string `json:"version"`
	PubKey  string `json:"pubkey"` // relay identity (hex X||Ed)
}

// RelayEntry is one relay in a relay list.
type RelayEntry struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	URLs   []string `json:"urls"` // direct, fronted, onion …
}

// RelayList is returned by GET /v1/relays, signed by the serving relay.
type RelayList struct {
	Relays    []RelayEntry `json:"relays"`
	Issued    int64        `json:"issued"`
	PubKey    string       `json:"pubkey"`
	Signature string       `json:"signature"` // over canonical JSON of {relays, issued}
}

// Accepted is the JSON response of upload endpoints.
type Accepted struct {
	Accepted int `json:"accepted"`
}

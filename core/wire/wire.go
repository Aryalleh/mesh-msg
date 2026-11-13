// Package wire defines the binary bodies of the relay HTTP API, shared by the
// server and every client (desktop, Android, bridges, other relays).
package wire

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Relay ports: the same on every relay, so one firewall rule set fits all
// (docs/server.md §6d). 3478/3479 are the IANA STUN/TURN ports; the TURN
// range lies in the IANA dynamic range but above Linux's default ephemeral
// ports (32768–60999), so it never collides with the relay's own outgoing
// connections.
const (
	PortHTTP         = 80    // TCP: API (behind a proxy) and admin panel
	PortHTTPS        = 443   // TCP: API over TLS
	PortReflector    = 3478  // UDP: address reflector; UDP+TCP: TURN
	PortReflectorAlt = 3479  // UDP: second reflector port (NAT type detection)
	PortTURNMin      = 61000 // UDP: TURN relayed addresses, first
	PortTURNMax      = 61999 // UDP: TURN relayed addresses, last
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
	HeaderReaders   = "X-Readers"   // media upload: downloads after which the relay deletes the file
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
	// Media (important files only, protocol.md §6b): the largest file this
	// relay holds (0 = no media), how long it keeps one for its readers and
	// how many one account may upload per UTC day.
	MediaMaxMB          int `json:"media_max_mb,omitempty"`
	MediaImportantHours int `json:"media_important_hours,omitempty"`
	MediaPerDay         int `json:"media_important_per_day,omitempty"`
	// MediaAccountsOnly: only listed accounts may upload.
	MediaAccountsOnly bool `json:"media_accounts_only,omitempty"`
	// Reflector: STUN binding on UDP PortReflector and PortReflectorAlt of
	// the relay's host (direct links). TURN: on for the relay's admins.
	Reflector bool `json:"reflector,omitempty"`
	TURN      bool `json:"turn,omitempty"`
}

// Media uploads are always signed by the uploading account (X-Account,
// X-Day, X-Signature over DomainMediaUpload and MediaSigBody), so a relay
// can count each account's important files per day.
const (
	HeaderAccount     = "X-Account" // uploader's public key (hex X||Ed)
	HeaderDay         = "X-Day"     // UTC day number (Unix time / 86400)
	DomainMediaUpload = "MESHMSG-MEDIA-PUT"
	DomainMediaQuota  = "MESHMSG-MEDIA-QUOTA"
	// DomainTurnCredentials: POST /v1/turn/credentials (admins only).
	DomainTurnCredentials = "MESHMSG-TURN-CRED"
)

// Day is the UTC day number of t.
func Day(t time.Time) uint32 { return uint32(t.Unix() / 86400) }

// MediaSigBody is what an upload signature covers: the id (hex) and the day.
func MediaSigBody(id string, day uint32) []byte {
	return binary.BigEndian.AppendUint32([]byte(id), day)
}

// QuotaSigBody is what a quota request signature covers.
func QuotaSigBody(day uint32) []byte { return binary.BigEndian.AppendUint32(nil, day) }

// MediaQuota is the answer of GET /v1/media/quota: an account's important
// files today on this relay.
type MediaQuota struct {
	PerDay  int   `json:"per_day"`
	Used    int   `json:"used"`
	Left    int   `json:"left"`
	Resets  int64 `json:"resets"`  // Unix time the count starts again
	Allowed bool  `json:"allowed"` // false: this account may not upload here (media_accounts)
	// PowBits is the proof-of-work the next upload needs (grows with today's
	// uploads, shrinks with the account's age).
	PowBits int `json:"pow_bits"`
	// AgeDays: days since the network first saw the account (its announce).
	AgeDays int `json:"age_days"`
}

// MediaPoWBody is what the proof of work of a media upload covers: the
// file's id (not its bytes, so big files cost the same), the account (hash)
// and the day, so work can't be reused.
func MediaPoWBody(id, account string, day uint32) []byte {
	return binary.BigEndian.AppendUint32([]byte("mesh-msg media|"+id+"|"+account+"|"), day)
}

// HeaderPoWBits is sent with a 402 on media uploads: the bits required now.
const HeaderPoWBits = "X-PoW-Bits"

// RelayEntry is one relay in a relay list.
type RelayEntry struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	URLs   []string `json:"urls"`             // direct, fronted, onion …
	PubKey string   `json:"pubkey,omitempty"` // hex X||Ed, pins the relay
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

// TurnCredentials: POST /v1/turn/credentials (relay admins only).
type TurnCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Expires  int64  `json:"expires"` // unix seconds
	Port     int    `json:"port"`    // UDP port of the TURN service
}

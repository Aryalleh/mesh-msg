// Package packet implements the outer packet format shared by every tier
// (see docs/protocol.md §2). It is the only part of the protocol that relays,
// bridges and LoRa nodes need to understand.
package packet

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	Version    = 1
	HeaderSize = 26
	// LoRaMTU is the SX126x maximum payload and the hard limit for any packet
	// that may cross the LoRa layer.
	LoRaMTU = 255
	// MaxFragments is the number of fragments one packet may be split into.
	MaxFragments = 16
	// FragBodySize is the body bytes available per fragment on LoRa
	// (MTU − header − fragment byte).
	FragBodySize = LoRaMTU - HeaderSize - 1
	// MaxSize is the largest packet accepted on any interface.
	MaxSize = 64 * 1024
	// DefaultMaxHops is used when the sender has no better idea.
	DefaultMaxHops = 8
)

// Type is the 3-bit packet type.
type Type uint8

const (
	TypeData     Type = 0
	TypeReceipt  Type = 1
	TypeAnnounce Type = 2
	TypePathReq  Type = 3
	TypeSummary  Type = 4
	TypeLink     Type = 5
)

func (t Type) String() string {
	switch t {
	case TypeData:
		return "DATA"
	case TypeReceipt:
		return "RECEIPT"
	case TypeAnnounce:
		return "ANNOUNCE"
	case TypePathReq:
		return "PATH_REQ"
	case TypeSummary:
		return "SUMMARY"
	case TypeLink:
		return "LINK"
	}
	return fmt.Sprintf("TYPE(%d)", uint8(t))
}

// Ctx flags (3 bits).
const (
	CtxPriority   uint8 = 0x1
	CtxNoLoRa     uint8 = 0x2
	CtxFragmented uint8 = 0x4
)

// ID is the random 8-byte packet identifier used for deduplication.
type ID [8]byte

func (id ID) String() string { return hex.EncodeToString(id[:]) }

// NewID returns a random packet ID.
func NewID() ID {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return id
}

// Hash is a 16-byte destination hash.
type Hash [16]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

// IsZero reports whether h is the broadcast/zero hash.
func (h Hash) IsZero() bool { return h == Hash{} }

// ParseHash parses a 32-char hex destination hash.
func ParseHash(s string) (Hash, error) {
	var h Hash
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return h, errors.New("packet: invalid destination hash")
	}
	copy(h[:], b)
	return h, nil
}

// Packet is a decoded packet. Body excludes the fragment byte.
type Packet struct {
	Type      Type
	Ctx       uint8
	HopCount  uint8 // 0..15
	MaxHops   uint8 // 0..15
	ID        ID
	Dest      Hash
	FragIndex uint8 // valid if Fragmented()
	FragCount uint8 // 1..16, valid if Fragmented()
	Body      []byte
}

func (p *Packet) Fragmented() bool { return p.Ctx&CtxFragmented != 0 }
func (p *Packet) NoLoRa() bool     { return p.Ctx&CtxNoLoRa != 0 }
func (p *Packet) Priority() bool   { return p.Ctx&CtxPriority != 0 }

// Key identifies a packet (or one fragment) for deduplication and storage.
type Key struct {
	ID   ID
	Frag uint8
}

func (p *Packet) Key() Key {
	if p.Fragmented() {
		return Key{p.ID, p.FragIndex}
	}
	return Key{p.ID, 0}
}

func (k Key) String() string { return fmt.Sprintf("%s/%d", k.ID, k.Frag) }

// Flags returns the encoded first header byte.
func (p *Packet) Flags() byte {
	return byte(Version)<<6 | byte(p.Type&0x7)<<3 | p.Ctx&0x7
}

// Size returns the encoded length.
func (p *Packet) Size() int {
	n := HeaderSize + len(p.Body)
	if p.Fragmented() {
		n++
	}
	return n
}

var (
	ErrShort    = errors.New("packet: too short")
	ErrVersion  = errors.New("packet: unsupported version")
	ErrTooLarge = errors.New("packet: too large")
	ErrFrag     = errors.New("packet: invalid fragment byte")
)

// Encode serialises p.
func (p *Packet) Encode() ([]byte, error) {
	if p.HopCount > 15 || p.MaxHops > 15 {
		return nil, errors.New("packet: hop fields out of range")
	}
	if p.Size() > MaxSize {
		return nil, ErrTooLarge
	}
	b := make([]byte, 0, p.Size())
	b = append(b, p.Flags(), p.HopCount<<4|p.MaxHops)
	b = append(b, p.ID[:]...)
	b = append(b, p.Dest[:]...)
	if p.Fragmented() {
		if p.FragCount == 0 || p.FragCount > MaxFragments || p.FragIndex >= p.FragCount {
			return nil, ErrFrag
		}
		b = append(b, p.FragIndex<<4|(p.FragCount-1))
	}
	return append(b, p.Body...), nil
}

// MustEncode is Encode for packets built by this process.
func (p *Packet) MustEncode() []byte {
	b, err := p.Encode()
	if err != nil {
		panic(err)
	}
	return b
}

// Decode parses raw bytes. The returned packet's Body aliases b.
func Decode(b []byte) (*Packet, error) {
	if len(b) < HeaderSize {
		return nil, ErrShort
	}
	if len(b) > MaxSize {
		return nil, ErrTooLarge
	}
	if b[0]>>6 != Version {
		return nil, ErrVersion
	}
	p := &Packet{
		Type:     Type(b[0] >> 3 & 0x7),
		Ctx:      b[0] & 0x7,
		HopCount: b[1] >> 4,
		MaxHops:  b[1] & 0xF,
	}
	copy(p.ID[:], b[2:10])
	copy(p.Dest[:], b[10:26])
	body := b[HeaderSize:]
	if p.Fragmented() {
		if len(body) < 1 {
			return nil, ErrShort
		}
		p.FragIndex = body[0] >> 4
		p.FragCount = body[0]&0xF + 1
		if p.FragIndex >= p.FragCount {
			return nil, ErrFrag
		}
		body = body[1:]
	}
	p.Body = body
	return p, nil
}

// Prologue returns the header bytes bound into end-to-end encryption:
// flags (with ctx masked out, since fragmentation is applied after
// encryption) || packet_id || dest_hash. hop fields are excluded because
// they change in transit.
func Prologue(t Type, id ID, dest Hash) []byte {
	b := make([]byte, 0, 25)
	b = append(b, byte(Version)<<6|byte(t&0x7)<<3)
	b = append(b, id[:]...)
	return append(b, dest[:]...)
}

// IncHop returns a copy of raw with the hop count incremented, or false if
// the packet has reached its hop limit.
func IncHop(raw []byte) ([]byte, bool) {
	if len(raw) < HeaderSize {
		return nil, false
	}
	hc, mh := raw[1]>>4, raw[1]&0xF
	if hc >= mh || hc == 15 {
		return nil, false
	}
	out := append([]byte(nil), raw...)
	out[1] = (hc+1)<<4 | mh
	return out, true
}

// Fragment splits an unfragmented logical packet whose body is too large for
// maxSize into fragments sharing ID. If it already fits, it is returned as is.
func Fragment(p *Packet, maxSize int) ([]*Packet, error) {
	if p.Fragmented() {
		return nil, errors.New("packet: already fragmented")
	}
	if p.Size() <= maxSize {
		return []*Packet{p}, nil
	}
	per := maxSize - HeaderSize - 1
	if per <= 0 {
		return nil, errors.New("packet: maxSize too small")
	}
	n := (len(p.Body) + per - 1) / per
	if n > MaxFragments {
		return nil, ErrTooLarge
	}
	out := make([]*Packet, n)
	for i := 0; i < n; i++ {
		end := min((i+1)*per, len(p.Body))
		q := *p
		q.Ctx |= CtxFragmented
		q.FragIndex = uint8(i)
		q.FragCount = uint8(n)
		q.Body = p.Body[i*per : end]
		out[i] = &q
	}
	return out, nil
}

// Reassemble joins a complete set of fragments (any order) into one logical
// unfragmented packet.
func Reassemble(frags []*Packet) (*Packet, error) {
	if len(frags) == 0 {
		return nil, errors.New("packet: no fragments")
	}
	n := int(frags[0].FragCount)
	if len(frags) != n {
		return nil, errors.New("packet: incomplete fragment set")
	}
	ordered := make([]*Packet, n)
	for _, f := range frags {
		if !f.Fragmented() || f.ID != frags[0].ID || int(f.FragCount) != n || ordered[f.FragIndex] != nil {
			return nil, ErrFrag
		}
		ordered[f.FragIndex] = f
	}
	var body []byte
	for _, f := range ordered {
		body = append(body, f.Body...)
	}
	q := *ordered[0]
	q.Ctx &^= CtxFragmented
	q.FragIndex, q.FragCount = 0, 0
	q.Body = body
	return &q, nil
}

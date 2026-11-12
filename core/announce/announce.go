// Package announce builds and verifies the signed control packets that relays,
// bridges and LoRa nodes can check without decrypting anything:
// ANNOUNCE ("this identity exists") and RECEIPT ("this packet was delivered").
package announce

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

const (
	domainAnnounce = "MESHMSG-ANNC"
	domainReceipt  = "MESHMSG-RCPT"
	domainFetch    = "MESHMSG-FETCH"
	sigSize        = 64
	maxAppData     = 64
)

// App-data TLV tags.
const (
	TagName  byte = 1
	TagFlags byte = 2
	// TagRelay is a relay the announcer reaches right now, so the mesh
	// learns working relays: scheme(1: 0 http, 1 https) | host[:port][/path].
	// Receivers must check it before use (it is only the announcer's word).
	TagRelay byte = 3
)

// Flags carried in app data.
const (
	// FlagDelegate allows any gateway to fetch this identity's mailbox from
	// relays and carry it into the local mesh.
	FlagDelegate byte = 1 << 0
	FlagNode     byte = 1 << 1
	FlagRelay    byte = 1 << 2
)

// AppData is the optional metadata inside an announce.
type AppData struct {
	Name  string
	Flags byte
	Relay string // http(s)://host[:port][/path]; left out if it doesn't fit
}

func (a AppData) encode() []byte {
	var b []byte
	if a.Flags != 0 {
		b = append(b, TagFlags, 1, a.Flags)
	}
	if a.Name != "" {
		name := []byte(a.Name)
		if len(name) > 32 {
			name = name[:32]
		}
		b = append(b, TagName, byte(len(name)))
		b = append(b, name...)
	}
	if v := encodeRelay(a.Relay); v != nil && len(b)+2+len(v) <= maxAppData {
		b = append(b, TagRelay, byte(len(v)))
		b = append(b, v...)
	}
	return b
}

func encodeRelay(u string) []byte {
	switch {
	case strings.HasPrefix(u, "http://"):
		return append([]byte{0}, u[len("http://"):]...)
	case strings.HasPrefix(u, "https://"):
		return append([]byte{1}, u[len("https://"):]...)
	}
	return nil
}

func decodeRelay(v []byte) string {
	if len(v) < 2 || v[0] > 1 {
		return ""
	}
	return [...]string{"http://", "https://"}[v[0]] + string(v[1:])
}

func decodeAppData(b []byte) (AppData, error) {
	var a AppData
	for len(b) > 0 {
		if len(b) < 2 || len(b) < 2+int(b[1]) {
			return a, errors.New("announce: bad app data")
		}
		tag, val := b[0], b[2:2+int(b[1])]
		switch tag {
		case TagName:
			a.Name = string(val)
		case TagFlags:
			if len(val) > 0 {
				a.Flags = val[0]
			}
		case TagRelay:
			a.Relay = decodeRelay(val)
		}
		b = b[2+int(b[1]):]
	}
	return a, nil
}

// Announce is a verified announce.
type Announce struct {
	Public identity.Public
	Time   time.Time
	App    AppData
}

// Build creates a signed ANNOUNCE packet for id.
func Build(id *identity.Identity, now time.Time, app AppData, maxHops uint8) *packet.Packet {
	ad := app.encode()
	body := make([]byte, 0, 64+4+1+len(ad)+sigSize)
	body = append(body, id.X[:]...)
	body = append(body, id.Ed[:]...)
	body = binary.BigEndian.AppendUint32(body, uint32(now.Unix()))
	body = append(body, byte(len(ad)))
	body = append(body, ad...)
	dest := id.Hash()
	sig := id.SignDomain(domainAnnounce, append(dest[:], body...))
	return &packet.Packet{
		Type:    packet.TypeAnnounce,
		MaxHops: maxHops,
		ID:      packet.NewID(),
		Dest:    dest,
		Body:    append(body, sig...),
	}
}

// Parse verifies an ANNOUNCE packet: the keys must hash to the destination
// and the signature must be valid.
func Parse(p *packet.Packet) (Announce, error) {
	var a Announce
	if p.Type != packet.TypeAnnounce {
		return a, errors.New("announce: not an announce")
	}
	b := p.Body
	if len(b) < 64+4+1+sigSize {
		return a, errors.New("announce: too short")
	}
	adLen := int(b[68])
	if adLen > maxAppData || len(b) != 69+adLen+sigSize {
		return a, errors.New("announce: bad length")
	}
	pub, _ := identity.PublicFromBytes(b[:64])
	if pub.Hash() != p.Dest {
		return a, errors.New("announce: keys do not match destination")
	}
	signed, sig := b[:69+adLen], b[69+adLen:]
	if !pub.Verify(domainAnnounce, append(p.Dest[:], signed...), sig) {
		return a, errors.New("announce: bad signature")
	}
	app, err := decodeAppData(b[69 : 69+adLen])
	if err != nil {
		return a, err
	}
	return Announce{Public: pub, Time: time.Unix(int64(binary.BigEndian.Uint32(b[64:68])), 0), App: app}, nil
}

// ReceiptID is the deterministic packet ID of the receipt for acked, so that
// receipts re-sent by the recipient deduplicate everywhere.
func ReceiptID(acked packet.ID) packet.ID {
	sum := sha256.Sum256(append([]byte("MESHMSG-RCPT-ID"), acked[:]...))
	var id packet.ID
	copy(id[:], sum[:8])
	return id
}

// BuildReceipt creates the signed RECEIPT for a packet delivered to id.
func BuildReceipt(id *identity.Identity, acked packet.ID, maxHops uint8) *packet.Packet {
	dest := id.Hash()
	sig := id.SignDomain(domainReceipt, append(acked[:], dest[:]...))
	return &packet.Packet{
		Type:    packet.TypeReceipt,
		MaxHops: maxHops,
		ID:      ReceiptID(acked),
		Dest:    dest,
		Body:    append(append([]byte(nil), acked[:]...), sig...),
	}
}

// ReceiptAcked returns the acknowledged packet ID without verifying.
func ReceiptAcked(p *packet.Packet) (packet.ID, error) {
	var id packet.ID
	if p.Type != packet.TypeReceipt || len(p.Body) != 8+sigSize {
		return id, errors.New("announce: malformed receipt")
	}
	copy(id[:], p.Body[:8])
	if ReceiptID(id) != p.ID {
		return id, errors.New("announce: receipt id mismatch")
	}
	return id, nil
}

// VerifyReceipt checks a receipt against the recipient's public identity
// (known from an announce or contact list).
func VerifyReceipt(p *packet.Packet, signer identity.Public) (packet.ID, error) {
	acked, err := ReceiptAcked(p)
	if err != nil {
		return acked, err
	}
	if signer.Hash() != p.Dest {
		return acked, errors.New("announce: signer does not match receipt destination")
	}
	if !signer.Verify(domainReceipt, append(acked[:], p.Dest[:]...), p.Body[8:]) {
		return acked, errors.New("announce: bad receipt signature")
	}
	return acked, nil
}

// SignFetch signs a relay mailbox-fetch challenge.
func SignFetch(id *identity.Identity, nonce []byte) []byte {
	dest := id.Hash()
	return id.SignDomain(domainFetch, append(append([]byte(nil), nonce...), dest[:]...))
}

// VerifyFetch checks a mailbox-fetch signature.
func VerifyFetch(pub identity.Public, nonce, sig []byte) bool {
	dest := pub.Hash()
	return pub.Verify(domainFetch, append(append([]byte(nil), nonce...), dest[:]...), sig)
}

package group

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/textenc"
)

// Private groups: a member list kept by every member. Traffic travels as
// ordinary one-to-one messages (Noise X, so the sender is authenticated and
// strangers' keys need not be known) with inner content type
// e2e.ContentGroup and this content:
//
//	group_id(16) | op(1) | body
//
//	op 0 TEXT     text_type(1) | text            (textenc, as content types 0/1)
//	op 1 ROSTER   version(4) | name_len(1) | name | n(1) | n × (x(32) | ed(32) | name_len(1) | name)
//	op 2 LEAVE    (empty)  the sender left
//	op 3 REMOVED  (empty)  the owner removed the recipient
//	op 4 MEDIA    a core/media reference (photo, file, voice or video message)
//
// The owner is whoever sent the first ROSTER (the invite) a member received;
// only the owner's later ROSTERs, with a higher version, are accepted. The
// owner sends the full roster to every member whenever it changes.

// Op is a private-group operation.
type Op uint8

const (
	OpText    Op = 0
	OpRoster  Op = 1
	OpLeave   Op = 2
	OpRemoved Op = 3
	OpMedia   Op = 4
)

// MaxMembers keeps a roster (≈ 70 bytes per member) within one message.
const MaxMembers = 30

// IDSize is the size of a private group's ID (used as its chat hash).
const IDSize = len(packet.Hash{})

// Member is one entry of a roster.
type Member struct {
	Public identity.Public
	Name   string // the name the owner gave (may be empty)
}

// Roster is a private group's name and members (the owner included).
type Roster struct {
	Version uint32
	Name    string
	Members []Member
}

// Has tells whether h is a member.
func (r *Roster) Has(h packet.Hash) bool {
	for _, m := range r.Members {
		if m.Public.Hash() == h {
			return true
		}
	}
	return false
}

// Envelope is decoded private-group content.
type Envelope struct {
	ID     packet.Hash
	Op     Op
	Text   string  // OpText
	Roster *Roster // OpRoster
	Media  []byte  // OpMedia: encoded media reference
}

// NewID makes a random group ID.
func NewID(rand io.Reader) (packet.Hash, error) {
	var id packet.Hash
	_, err := io.ReadFull(rand, id[:])
	return id, err
}

func head(id packet.Hash, op Op) []byte {
	b := make([]byte, 0, IDSize+1)
	b = append(b, id[:]...)
	return append(b, byte(op))
}

// EncodeText encodes a text for the group.
func EncodeText(id packet.Hash, text string) []byte {
	ct, content := textenc.Encode(text)
	return append(append(head(id, OpText), ct), content...)
}

// EncodeMedia encodes a media reference for the group.
func EncodeMedia(id packet.Hash, ref []byte) []byte {
	return append(head(id, OpMedia), ref...)
}

// EncodeOp encodes LEAVE or REMOVED.
func EncodeOp(id packet.Hash, op Op) []byte { return head(id, op) }

func shortName(s string, max int) string {
	for len(s) > max {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// EncodeRoster encodes the group's roster.
func EncodeRoster(id packet.Hash, r Roster) ([]byte, error) {
	if len(r.Members) > MaxMembers {
		return nil, errors.New("group: too many members")
	}
	b := head(id, OpRoster)
	b = binary.BigEndian.AppendUint32(b, r.Version)
	name := shortName(r.Name, 64)
	b = append(append(b, byte(len(name))), name...)
	b = append(b, byte(len(r.Members)))
	for _, m := range r.Members {
		b = append(b, m.Public.X[:]...)
		b = append(b, m.Public.Ed[:]...)
		n := shortName(m.Name, MaxNameLen)
		b = append(append(b, byte(len(n))), n...)
	}
	return b, nil
}

var errShort = errors.New("group: truncated group content")

// Decode parses private-group content.
func Decode(b []byte) (*Envelope, error) {
	if len(b) < IDSize+1 {
		return nil, errShort
	}
	e := &Envelope{Op: Op(b[IDSize])}
	copy(e.ID[:], b[:IDSize])
	body := b[IDSize+1:]
	switch e.Op {
	case OpText:
		if len(body) < 1 {
			return nil, errShort
		}
		t, err := textenc.Decode(body[0], body[1:])
		if err != nil {
			return nil, err
		}
		e.Text = t
	case OpRoster:
		r, err := decodeRoster(body)
		if err != nil {
			return nil, err
		}
		e.Roster = r
	case OpMedia:
		if len(body) == 0 {
			return nil, errShort
		}
		e.Media = append([]byte(nil), body...)
	case OpLeave, OpRemoved:
	default:
		return nil, errors.New("group: unknown operation")
	}
	return e, nil
}

func decodeRoster(b []byte) (*Roster, error) {
	str := func() (string, bool) {
		if len(b) < 1 || len(b) < 1+int(b[0]) {
			return "", false
		}
		n := int(b[0])
		s := string(b[1 : 1+n])
		b = b[1+n:]
		return strings.ToValidUTF8(s, ""), true
	}
	if len(b) < 4 {
		return nil, errShort
	}
	r := &Roster{Version: binary.BigEndian.Uint32(b)}
	b = b[4:]
	var ok bool
	if r.Name, ok = str(); !ok || len(b) < 1 {
		return nil, errShort
	}
	n := int(b[0])
	b = b[1:]
	if n > MaxMembers {
		return nil, errors.New("group: too many members")
	}
	for i := 0; i < n; i++ {
		if len(b) < 64 {
			return nil, errShort
		}
		var m Member
		copy(m.Public.X[:], b[:32])
		copy(m.Public.Ed[:], b[32:64])
		b = b[64:]
		if m.Name, ok = str(); !ok {
			return nil, errShort
		}
		r.Members = append(r.Members, m)
	}
	return r, nil
}

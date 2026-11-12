// Package group implements group chats: private groups (a random secret
// shared by invite link) and public channels (the secret is derived from a
// public name: the global channel and regional channels).
//
// A group is addressed like a person: its secret derives an identity (X25519
// + Ed25519), and dest_hash is that identity's hash. Every member holds the
// group identity, so members announce it, fetch its relay mailbox and let
// gateways fetch it for them exactly as for their own account; relays and
// LoRa nodes need no changes and can't tell a group from a person.
//
// Message body (packet type DATA, dest = group hash):
//
//	ChaCha20-Poly1305(key, nonce = 0^4 || packet_id, ad = prologue,
//	  sender_x(32) | sender_ed(32) | name_len(1) | name | content_type(1) | timestamp(4) | content | sig(64))
//
// sig is the sender's Ed25519 signature (domain "mesh-msg group v1") over
// dest || packet_id || everything before sig, so members (and, in public
// channels, anyone) cannot post in someone else's name. Groups never send
// delivery receipts: a receipt would purge the copies other members still
// need. Copies expire with the store TTL. Global channel packets carry
// NO_LORA.
package group

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/textenc"
)

// Kind of a group.
type Kind string

const (
	Private  Kind = "private"  // random secret, joined by invite link
	Regional Kind = "regional" // public channel of a region; anyone can read
	Global   Kind = "global"   // the one public channel for everyone
)

// GlobalName is the global channel's name.
const GlobalName = "global"

const (
	signDomain  = "mesh-msg group v1"
	uriScheme   = "meshgroup"
	SecretSize  = 32
	MaxNameLen  = 32 // sender display name bytes
	fixedHeader = 32 + 32 + 1 + 1 + 4
	sigSize     = 64
	// Overhead is the body bytes besides name and content.
	Overhead = fixedHeader + sigSize + chacha20poly1305.Overhead
)

// MaxTextBytes is the largest encoded text that fits in MaxFragments LoRa
// frames with a full-length sender name.
const MaxTextBytes = packet.MaxFragments*packet.FragBodySize - Overhead - MaxNameLen

// Group is a joined group.
type Group struct {
	Name   string
	Kind   Kind
	Secret [SecretSize]byte
	id     *identity.Identity
	key    [32]byte
}

// New makes a private group with a fresh secret.
func New(name string, rand io.Reader) (*Group, error) {
	var s [SecretSize]byte
	if _, err := io.ReadFull(rand, s[:]); err != nil {
		return nil, err
	}
	return FromSecret(name, Private, s)
}

// Channel returns the public channel with this name: kind Global for
// "global", otherwise Regional. Names are compared case-insensitively.
func Channel(name string) (*Group, error) {
	norm := NormalizeChannel(name)
	if norm == "" {
		return nil, errors.New("group: empty channel name")
	}
	kind := Regional
	if norm == GlobalName {
		kind = Global
	}
	s := sha256.Sum256([]byte("mesh-msg public channel v1|" + norm))
	return FromSecret(norm, kind, s)
}

// NormalizeChannel lower-cases a channel name and trims spaces and a
// leading '#'.
func NormalizeChannel(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "#")))
}

// FromSecret rebuilds a group from its secret.
func FromSecret(name string, kind Kind, s [SecretSize]byte) (*Group, error) {
	g := &Group{Name: name, Kind: kind, Secret: s}
	seed := make([]byte, 64)
	if _, err := io.ReadFull(hkdf.New(sha256.New, s[:], nil, []byte("mesh-msg group identity")), seed); err != nil {
		return nil, err
	}
	id, err := identity.FromSeed(seed)
	if err != nil {
		return nil, err
	}
	g.id = id
	if _, err := io.ReadFull(hkdf.New(sha256.New, s[:], nil, []byte("mesh-msg group key")), g.key[:]); err != nil {
		return nil, err
	}
	return g, nil
}

// Identity is the group's shared identity (announced, used to fetch its
// relay mailbox).
func (g *Group) Identity() *identity.Identity { return g.id }

// Hash is the group's address.
func (g *Group) Hash() packet.Hash { return g.id.Hash() }

// Public tells whether anyone can derive the group (a channel).
func (g *Group) Public() bool { return g.Kind != Private }

// Title is how UIs show the group: "#name" for channels.
func (g *Group) Title() string {
	if g.Public() {
		return "#" + g.Name
	}
	return g.Name
}

// URI is the invite link: meshgroup:<base64url secret>?n=<name> for
// private groups, meshgroup:#<name> for channels.
func (g *Group) URI() string {
	if g.Public() {
		return uriScheme + ":#" + url.PathEscape(g.Name)
	}
	s := uriScheme + ":" + base64.RawURLEncoding.EncodeToString(g.Secret[:])
	if g.Name != "" {
		s += "?n=" + url.QueryEscape(g.Name)
	}
	return s
}

// ParseURI parses an invite link (or a bare "#channel").
func ParseURI(s string) (*Group, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return Channel(s)
	}
	rest, ok := strings.CutPrefix(s, uriScheme+":")
	if !ok {
		return nil, errors.New("group: not a group link (meshgroup:…)")
	}
	if strings.HasPrefix(rest, "#") {
		name, err := url.PathUnescape(rest[1:])
		if err != nil {
			return nil, err
		}
		return Channel(name)
	}
	enc, query, _ := strings.Cut(rest, "?")
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || len(b) != SecretSize {
		return nil, errors.New("group: bad group link")
	}
	name := ""
	if q, err := url.ParseQuery(query); err == nil {
		name = q.Get("n")
	}
	if name == "" {
		name = "group"
	}
	var sec [SecretSize]byte
	copy(sec[:], b)
	return FromSecret(name, Private, sec)
}

// Message is a decrypted group message.
type Message struct {
	Group       packet.Hash
	PacketID    packet.ID
	From        identity.Public
	FromName    string // the sender's own display name (unverified label)
	Sent        time.Time
	ContentType uint8
	Text        string
}

func nonce(id packet.ID) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	copy(n[4:], id[:])
	return n
}

// BuildText encrypts and signs text from sender (shown as senderName) and
// returns LoRa-sized packets.
func (g *Group) BuildText(sender *identity.Identity, senderName, text string, maxHops uint8, priority bool, now time.Time) ([]*packet.Packet, error) {
	ct, content := textenc.Encode(text)
	if len(content) > MaxTextBytes {
		return nil, errors.New("group: text too long")
	}
	senderName = truncName(senderName)
	if maxHops == 0 {
		maxHops = packet.DefaultMaxHops
	}
	id := packet.NewID()
	dest := g.Hash()
	pt := make([]byte, 0, Overhead+len(senderName)+len(content))
	pt = append(pt, sender.X[:]...)
	pt = append(pt, sender.Ed[:]...)
	pt = append(pt, byte(len(senderName)))
	pt = append(pt, senderName...)
	pt = append(pt, ct)
	pt = binary.BigEndian.AppendUint32(pt, uint32(now.Unix()))
	pt = append(pt, content...)
	pt = append(pt, sender.SignDomain(signDomain, signed(dest, id, pt))...)

	aead, _ := chacha20poly1305.New(g.key[:])
	body := aead.Seal(nil, nonce(id), pt, packet.Prologue(packet.TypeData, id, dest))
	p := &packet.Packet{Type: packet.TypeData, MaxHops: maxHops, ID: id, Dest: dest, Body: body}
	if priority {
		p.Ctx |= packet.CtxPriority
	}
	if g.Kind == Global {
		// The global channel would swamp LoRa airtime; it travels over
		// relays, LAN and phone-to-phone links only.
		p.Ctx |= packet.CtxNoLoRa
	}
	return packet.Fragment(p, packet.LoRaMTU)
}

func signed(dest packet.Hash, id packet.ID, pt []byte) []byte {
	m := make([]byte, 0, 16+8+len(pt))
	m = append(m, dest[:]...)
	m = append(m, id[:]...)
	return append(m, pt...)
}

func truncName(s string) string {
	for len(s) > MaxNameLen {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// ErrNotForGroup means the packet is not a message of this group (wrong key,
// tampered, or bad signature).
var ErrNotForGroup = errors.New("group: not a message of this group")

// Open decrypts and verifies a complete (reassembled) DATA packet.
func (g *Group) Open(p *packet.Packet) (*Message, error) {
	if p.Dest != g.Hash() {
		return nil, ErrNotForGroup
	}
	aead, _ := chacha20poly1305.New(g.key[:])
	pt, err := aead.Open(nil, nonce(p.ID), p.Body, packet.Prologue(packet.TypeData, p.ID, p.Dest))
	if err != nil || len(pt) < fixedHeader+sigSize {
		return nil, ErrNotForGroup
	}
	msg, sig := pt[:len(pt)-sigSize], pt[len(pt)-sigSize:]
	var from identity.Public
	copy(from.X[:], msg[0:32])
	copy(from.Ed[:], msg[32:64])
	nl := int(msg[64])
	if nl > MaxNameLen || len(msg) < fixedHeader+nl {
		return nil, ErrNotForGroup
	}
	if !from.Verify(signDomain, signed(p.Dest, p.ID, msg), sig) {
		return nil, fmt.Errorf("%w: bad signature", ErrNotForGroup)
	}
	name := string(msg[65 : 65+nl])
	rest := msg[65+nl:]
	m := &Message{
		Group: p.Dest, PacketID: p.ID, From: from, FromName: strings.ToValidUTF8(name, ""),
		ContentType: rest[0], Sent: time.Unix(int64(binary.BigEndian.Uint32(rest[1:5])), 0),
	}
	if m.Text, err = textenc.Decode(rest[0], rest[5:]); err != nil {
		return nil, err
	}
	return m, nil
}

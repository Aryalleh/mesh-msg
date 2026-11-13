// Package identity holds a user's (or node's / relay's) keys and the public
// form used as an address (docs/protocol.md §1).
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/crypto/curve25519"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Public is the public half of an identity: an X25519 key for encryption and
// an Ed25519 key for signatures.
type Public struct {
	X  [32]byte
	Ed [32]byte
}

// Hash returns the 16-byte destination hash: SHA-256(x25519 || ed25519)[:16].
func (p Public) Hash() packet.Hash {
	sum := sha256.Sum256(append(p.X[:], p.Ed[:]...))
	var h packet.Hash
	copy(h[:], sum[:16])
	return h
}

// Bytes returns X || Ed (64 bytes).
func (p Public) Bytes() []byte { return append(append([]byte(nil), p.X[:]...), p.Ed[:]...) }

// PublicFromBytes parses X || Ed.
func PublicFromBytes(b []byte) (Public, error) {
	var p Public
	if len(b) != 64 {
		return p, errors.New("identity: public key must be 64 bytes")
	}
	copy(p.X[:], b[:32])
	copy(p.Ed[:], b[32:])
	return p, nil
}

// Verify checks an Ed25519 signature made with SignDomain.
func (p Public) Verify(domain string, msg, sig []byte) bool {
	return ed25519.Verify(ed25519.PublicKey(p.Ed[:]), domainMsg(domain, msg), sig)
}

// Identity is a full keypair set.
type Identity struct {
	Public
	xPriv  [32]byte
	edPriv ed25519.PrivateKey
}

// Generate creates a fresh identity.
func Generate() (*Identity, error) {
	var seed [64]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}
	return FromSeed(seed[:])
}

// FromSeed rebuilds an identity from its 64-byte secret (x25519 private ||
// ed25519 seed). Keep this secret.
func FromSeed(seed []byte) (*Identity, error) {
	if len(seed) != 64 {
		return nil, errors.New("identity: seed must be 64 bytes")
	}
	id := &Identity{}
	copy(id.xPriv[:], seed[:32])
	pub, err := curve25519.X25519(id.xPriv[:], curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	copy(id.X[:], pub)
	id.edPriv = ed25519.NewKeyFromSeed(seed[32:])
	copy(id.Ed[:], id.edPriv.Public().(ed25519.PublicKey))
	return id, nil
}

// Seed returns the 64-byte secret.
func (id *Identity) Seed() []byte {
	return append(append([]byte(nil), id.xPriv[:]...), id.edPriv.Seed()...)
}

// XPrivate returns the X25519 private key (for Noise).
func (id *Identity) XPrivate() []byte { return append([]byte(nil), id.xPriv[:]...) }

// SignDomain signs msg under a domain-separation tag.
func (id *Identity) SignDomain(domain string, msg []byte) []byte {
	return ed25519.Sign(id.edPriv, domainMsg(domain, msg))
}

func domainMsg(domain string, msg []byte) []byte {
	return append([]byte(domain), msg...)
}

// Contact is a public identity with an optional display name, exchanged via
// QR code or link.
type Contact struct {
	Public
	Name string
}

const uriScheme = "meshmsg"

// URI encodes the contact as meshmsg:<base64url(X||Ed)>?n=<name>.
func (c Contact) URI() string {
	s := uriScheme + ":" + base64.RawURLEncoding.EncodeToString(c.Bytes())
	if c.Name != "" {
		s += "?n=" + url.QueryEscape(c.Name)
	}
	return s
}

// LinkBase is where shared contact links point: a web page there opens the
// app (or explains how to get it). The contact goes in the fragment, so it
// never reaches the web server.
const LinkBase = "https://eu-1.mfadem.ir/add#"

// Link is the contact as a web link (https://…/add#<key>?n=<name>) that
// messengers make clickable; ParseURI accepts it like the meshmsg: form.
func (c Contact) Link() string {
	return LinkBase + strings.TrimPrefix(c.URI(), uriScheme+":")
}

// fromLink turns an http(s) contact link (any host, path ending in /add)
// into the meshmsg: form; other strings are returned unchanged.
func fromLink(s string) string {
	if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
		return s
	}
	page, frag, ok := strings.Cut(s, "#")
	if !ok || !strings.HasSuffix(strings.TrimRight(page, "/"), "/add") {
		return s
	}
	return uriScheme + ":" + frag
}

// ParseURI parses a contact URI (meshmsg:… or a contact web link).
func ParseURI(s string) (Contact, error) {
	var c Contact
	rest, ok := strings.CutPrefix(fromLink(strings.TrimSpace(s)), uriScheme+":")
	if !ok {
		return c, fmt.Errorf("identity: not a %s: URI", uriScheme)
	}
	keyPart, query, _ := strings.Cut(rest, "?")
	b, err := base64.RawURLEncoding.DecodeString(keyPart)
	if err != nil {
		return c, fmt.Errorf("identity: bad key encoding: %w", err)
	}
	if c.Public, err = PublicFromBytes(b); err != nil {
		return c, err
	}
	if query != "" {
		v, err := url.ParseQuery(query)
		if err != nil {
			return c, err
		}
		c.Name = v.Get("n")
	}
	return c, nil
}

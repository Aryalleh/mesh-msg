// Package network holds the official relay list (relays.json), shipped in
// every build and refreshed from the repository at run time.
package network

import (
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/Aryalleh/mesh-msg/core/update"
)

//go:embed relays.json
var bootstrap []byte

// bootstrapSig is relays.json.sig: the project key's signature over
// relays.json (see Sign). Empty until the list has been signed.
//
//go:embed relays.json.sig
var bootstrapSig []byte

// SourceURL is where clients refresh the list from (the repository's main
// branch). Relays also share their lists (GET /v1/relays), so the list still
// spreads where GitHub is blocked.
const SourceURL = "https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/network/relays.json"

// SigSuffix is appended to a list URL to fetch its signature file.
const SigSuffix = ".sig"

// Relay is one entry of the list.
type Relay struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	URLs   []string `json:"urls"`
	PubKey string   `json:"pubkey,omitempty"` // hex X||Ed (128 bytes) — pins the relay
	Origin bool     `json:"origin,omitempty"` // the original project relay
}

// List is relays.json.
type List struct {
	Version int    `json:"version"`
	Updated string `json:"updated"`
	// Serial grows with every edit of the list; between two signed lists
	// the higher serial wins, so an old list can't push out a newer one.
	Serial int64   `json:"serial,omitempty"`
	Relays []Relay `json:"relays"`
}

// Parse decodes and checks a relay list.
func Parse(b []byte) (*List, error) {
	var l List
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if l.Version != 1 {
		return nil, errors.New("network: unsupported relay list version")
	}
	if len(l.Relays) == 0 || len(l.Relays) > 200 {
		return nil, errors.New("network: bad relay list")
	}
	for _, r := range l.Relays {
		if r.Name == "" || len(r.URLs) == 0 {
			return nil, errors.New("network: relay without name or URL")
		}
		if r.PubKey != "" {
			if k, err := hex.DecodeString(r.PubKey); err != nil || len(k) != 64 {
				return nil, errors.New("network: bad relay pubkey")
			}
		}
		for _, u := range r.URLs {
			if _, err := CheckURL(u, true); err != nil {
				return nil, err
			}
		}
	}
	return &l, nil
}

// Bootstrap is the list compiled into this build.
func Bootstrap() *List {
	l, err := Parse(bootstrap)
	if err != nil {
		panic("network: embedded relays.json: " + err.Error())
	}
	return l
}

// BootstrapSigned is the list compiled into this build as a Signed value
// (Sig is nil if relays.json.sig is missing or doesn't match).
func BootstrapSigned() Signed {
	s := Signed{JSON: bootstrap}
	if sig, err := hex.DecodeString(strings.TrimSpace(string(bootstrapSig))); err == nil && len(sig) == ed25519.SignatureSize {
		s.Sig = sig
		if !s.Verify(nil) {
			s.Sig = nil
		}
	}
	return s
}

// SignContext separates relay-list signatures from release manifests, which
// the same project key (core/update.ReleaseKey) signs.
const SignContext = "MESHMSG-RELAYS-v1\x00"

// Signed is a relay list as published: the exact relays.json bytes and the
// project key's signature over them (nil if unsigned). Only signed lists
// are passed between nearby devices; the signature is what lets a phone
// trust a list a stranger hands it during an Internet blackout.
type Signed struct {
	JSON []byte
	Sig  []byte
}

// Sign signs relays.json bytes with the project key.
func Sign(json []byte, priv ed25519.PrivateKey) []byte {
	return ed25519.Sign(priv, append([]byte(SignContext), json...))
}

// Verify checks the signature (pub nil = core/update.ReleaseKey).
func (s Signed) Verify(pub ed25519.PublicKey) bool {
	if pub == nil {
		pub = update.ReleaseKey()
	}
	return len(s.Sig) == ed25519.SignatureSize && ed25519.Verify(pub, append([]byte(SignContext), s.JSON...), s.Sig)
}

// Encode is the form carried in the mesh: signature || relays.json.
func (s Signed) Encode() []byte {
	return append(append([]byte(nil), s.Sig...), s.JSON...)
}

// DecodeSigned reverses Encode, verifies the signature and parses the list.
func DecodeSigned(b []byte, pub ed25519.PublicKey) (Signed, *List, error) {
	if len(b) <= ed25519.SignatureSize {
		return Signed{}, nil, errors.New("network: signed relay list too short")
	}
	s := Signed{Sig: append([]byte(nil), b[:ed25519.SignatureSize]...), JSON: append([]byte(nil), b[ed25519.SignatureSize:]...)}
	if !s.Verify(pub) {
		return Signed{}, nil, errors.New("network: relay list signature does not verify")
	}
	l, err := Parse(s.JSON)
	if err != nil {
		return Signed{}, nil, err
	}
	return s, l, nil
}

// Hash identifies a list's exact bytes.
func (s Signed) Hash() [32]byte { return sha256.Sum256(s.JSON) }

// CheckURL normalises a relay URL (http/https, host[:port][/path], no user
// info, query or fragment). Unless allowPrivate, it refuses loopback,
// private, link-local and unspecified addresses, so a stranger's hint can't
// point phones at their own local network.
func CheckURL(raw string, allowPrivate bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 200 {
		return "", errors.New("network: bad relay URL")
	}
	host := strings.ToLower(u.Hostname())
	if !allowPrivate {
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
			return "", errors.New("network: local relay URL")
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
			return "", errors.New("network: private relay address")
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

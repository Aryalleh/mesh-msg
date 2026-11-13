// Package update implements the update check (roadmap B7, decisions D20).
//
// Every release tag publishes a signed release.json. Apps carry the release
// public key (ReleaseKey) and ignore any manifest that doesn't verify, so a
// relay, mirror or attacker cannot push a fake "update now" prompt. The file
// is an envelope:
//
//	{"manifest": "<base64 of the manifest JSON>", "sig": "<base64 Ed25519 signature>"}
//
// The signature covers SignContext followed by the exact manifest bytes, so the
// bytes that were signed are the bytes that are parsed.
package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReleaseKeyHex is the Ed25519 public key that signs release manifests. The
// private half is kept outside the repository (a GitHub Actions secret and the
// owner's offline copy).
const ReleaseKeyHex = "4a89de4b477138723c6f8a2084c4f52dd843471b3869d465e9a9e402d7395626"

// SignContext is prepended to the manifest bytes before signing (domain separation).
const SignContext = "meshmsg-release-manifest-v1\n"

// Channels.
const (
	ChannelBeta   = "beta"
	ChannelStable = "stable"
)

// ReleaseKey returns ReleaseKeyHex decoded.
func ReleaseKey() ed25519.PublicKey {
	b, err := hex.DecodeString(ReleaseKeyHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("update: bad ReleaseKeyHex")
	}
	return b
}

// File is one download of a release.
type File struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest describes one release.
type Manifest struct {
	Version string `json:"version"` // without "v", e.g. 0.1.1-beta1
	Channel string `json:"channel"` // beta or stable
	Date    string `json:"date"`    // RFC 3339
	// MinSupported: installs below this version are told to update (banner can't
	// be dismissed). Empty means every version is still supported.
	MinSupported string            `json:"min_supported,omitempty"`
	Notes        map[string]string `json:"notes"` // language ("en", "fa") → release notes
	Page         string            `json:"page,omitempty"`
	Files        []File            `json:"files"`
}

// Envelope is the release.json file.
type Envelope struct {
	Manifest string `json:"manifest"`
	Sig      string `json:"sig"`
}

// Sign builds release.json for m.
func Sign(m *Manifest, priv ed25519.PrivateKey) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, append([]byte(SignContext), body...))
	return json.MarshalIndent(Envelope{
		Manifest: base64.StdEncoding.EncodeToString(body),
		Sig:      base64.StdEncoding.EncodeToString(sig),
	}, "", "  ")
}

// Verify checks release.json against pub and returns its manifest.
func Verify(data []byte, pub ed25519.PublicKey) (*Manifest, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("release manifest: %w", err)
	}
	body, err := base64.StdEncoding.DecodeString(e.Manifest)
	if err != nil {
		return nil, fmt.Errorf("release manifest: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(e.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("release manifest: bad signature encoding")
	}
	if !ed25519.Verify(pub, append([]byte(SignContext), body...), sig) {
		return nil, errors.New("release manifest: signature does not verify")
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("release manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if _, err := parse(m.Version); err != nil {
		return fmt.Errorf("release manifest: version: %w", err)
	}
	if m.MinSupported != "" {
		if _, err := parse(m.MinSupported); err != nil {
			return fmt.Errorf("release manifest: min_supported: %w", err)
		}
	}
	if m.Channel != ChannelBeta && m.Channel != ChannelStable {
		return fmt.Errorf("release manifest: unknown channel %q", m.Channel)
	}
	if m.Channel == ChannelStable && IsPrerelease(m.Version) {
		return fmt.Errorf("release manifest: stable channel with pre-release version %s", m.Version)
	}
	if _, err := time.Parse(time.RFC3339, m.Date); err != nil {
		return fmt.Errorf("release manifest: date: %w", err)
	}
	for _, f := range m.Files {
		if f.Name == "" || f.URL == "" || len(f.SHA256) != 64 {
			return fmt.Errorf("release manifest: bad file entry %q", f.Name)
		}
	}
	return nil
}

// File returns the file whose name has the given suffix, or nil.
func (m *Manifest) File(suffix string) *File {
	for i := range m.Files {
		if strings.HasSuffix(m.Files[i].Name, suffix) {
			return &m.Files[i]
		}
	}
	return nil
}

// ChannelOf is the channel an install of version v follows: stable versions
// see stable releases only, every pre-release (alpha, beta, rc) sees both.
func ChannelOf(v string) string {
	if IsPrerelease(v) {
		return ChannelBeta
	}
	return ChannelStable
}

// IsRelease reports whether v is a tagged release version (not a development
// build such as 0.0.0-dev+abc1234 or "dev").
func IsRelease(v string) bool {
	_, err := parse(v)
	return err == nil && !strings.HasPrefix(v, "0.0.0")
}

// IsPrerelease reports whether v has a pre-release part (0.2.0-beta1).
func IsPrerelease(v string) bool {
	p, err := parse(v)
	return err == nil && len(p.pre) > 0
}

// Accepts reports whether an install on channel ch is offered a release on channel rel.
func Accepts(ch, rel string) bool { return ch == ChannelBeta || rel == ChannelStable }

type semver struct {
	core [3]int
	pre  []string
}

func parse(v string) (semver, error) {
	var s semver
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i] // build metadata is ignored
	}
	main, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return s, fmt.Errorf("%q is not a version", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, fmt.Errorf("%q is not a version", v)
		}
		s.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return s, fmt.Errorf("%q is not a version", v)
		}
		s.pre = splitPre(pre)
	}
	return s, nil
}

// splitPre splits "beta10" into "beta", "10" as well as on dots, so that
// beta2 < beta10 (tags here are written without a dot).
func splitPre(pre string) []string {
	var out []string
	for _, id := range strings.Split(pre, ".") {
		start := 0
		for i := 1; i <= len(id); i++ {
			if i == len(id) || isDigit(id[i]) != isDigit(id[i-1]) {
				out = append(out, id[start:i])
				start = i
			}
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Compare returns -1, 0 or +1 as a is older, equal or newer than b (semver
// precedence; 0.2.0-beta2 < 0.2.0). Unparseable versions sort oldest.
func Compare(a, b string) int {
	pa, ea := parse(a)
	pb, eb := parse(b)
	switch {
	case ea != nil && eb != nil:
		return 0
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	}
	for i := range 3 {
		if c := cmpInt(pa.core[i], pb.core[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(pa.pre) == 0 && len(pb.pre) == 0:
		return 0
	case len(pa.pre) == 0:
		return 1
	case len(pb.pre) == 0:
		return -1
	}
	for i := 0; i < len(pa.pre) && i < len(pb.pre); i++ {
		x, y := pa.pre[i], pb.pre[i]
		nx, errx := strconv.Atoi(x)
		ny, erry := strconv.Atoi(y)
		var c int
		switch {
		case errx == nil && erry == nil:
			c = cmpInt(nx, ny)
		case errx == nil:
			c = -1
		case erry == nil:
			c = 1
		default:
			c = strings.Compare(x, y)
		}
		if c != 0 {
			return c
		}
	}
	return cmpInt(len(pa.pre), len(pb.pre))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

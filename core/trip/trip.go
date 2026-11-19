// Package trip is location sharing for a group trip (docs/trip.md).
//
// A trip has its own identity (X25519 + Ed25519, like an account). Only the
// trip's leaders hold its secret; members get the public half in a signed
// trip card from the trip's relay (GET /v1/trip) or a meshtrip: link, and
// agree before anything is sent. A member's phone then sends a location
// report every few minutes as an ordinary DATA packet to the trip's identity
// (content type e2e.ContentTrip, Noise X, so the member is known only
// inside the ciphertext). Reports travel like any message: Bluetooth to
// nearby phones, LoRa, relays; every device on the way carries them without
// being able to read them, the way AirTag reports travel. Leaders fetch the
// trip's relay mailbox as the trip identity and open the reports; nobody
// else can, the relay included. No receipts: every leader (and the relay's
// web map) needs every copy.
//
// Content (e2e.ContentTrip), first byte the kind:
//
//	1 report (member → trip):  flags(1) lat(4) lon(4) accuracy_m(2) fix_age_s(2) battery(1) name_len(1) name
//	                           lat/lon: signed 1e-7 degrees; battery 255 = unknown
//	2 locate (leader → member): trip_hash(16) minutes(1)
//	3 lead (leader → leader):  seed(64) signed trip card (JSON)
package trip

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Content kinds.
const (
	KindReport = 1
	KindLocate = 2
	KindLead   = 3
)

// Report flags.
const (
	FlagSOS     = 1 // the member tapped "I'm lost"
	FlagStopped = 2 // the member stopped sharing (last report)
	FlagAnswer  = 4 // sent because a leader asked
	FlagNoFix   = 8 // no position yet (lat/lon meaningless)
)

const (
	signDomain = "mesh-msg trip v1"
	uriScheme  = "meshtrip"
	// MaxName is the longest member name a report carries (bytes), so a
	// report stays one LoRa frame.
	MaxName = 24
	// MaxTripName is the longest trip name (bytes).
	MaxTripName = 64
	// MaxLeaders bounds the leader list.
	MaxLeaders = 16
	// MinEvery and MaxEvery bound the report interval.
	MinEvery = 60
	MaxEvery = 3600
	// MaxDays is the longest a trip may run.
	MaxDays = 90
)

// Leader is one leader in the trip card.
type Leader struct {
	ID   string `json:"id"` // account hash (hex)
	Name string `json:"name,omitempty"`
	// Key is the leader's public key (hex X||Ed): members' phones can write
	// to the leaders (a lost member's location message). Optional.
	Key string `json:"key,omitempty"`
}

// Card describes a trip: what a member agrees to.
type Card struct {
	V       int      `json:"v"`
	Name    string   `json:"name"`
	Key     string   `json:"key"` // the trip's public key, hex X||Ed (128 chars)
	Leaders []Leader `json:"leaders"`
	Every   int      `json:"every"` // seconds between reports
	Ends    int64    `json:"ends"`  // unix seconds; sharing stops by itself then
	Relay   string   `json:"relay,omitempty"`
	Made    int64    `json:"made"` // unix seconds; a newer card replaces an older one
}

// Public returns the trip's public identity.
func (c Card) Public() (identity.Public, error) {
	b, err := hex.DecodeString(c.Key)
	if err != nil {
		return identity.Public{}, fmt.Errorf("trip: key: %w", err)
	}
	return identity.PublicFromBytes(b)
}

// Hash is the trip's destination hash ("" if the key is bad).
func (c Card) Hash() string {
	p, err := c.Public()
	if err != nil {
		return ""
	}
	return p.Hash().String()
}

// IsLeader reports whether account (hash hex) is a leader.
func (c Card) IsLeader(account string) bool {
	for _, l := range c.Leaders {
		if strings.EqualFold(l.ID, account) {
			return true
		}
	}
	return false
}

// Ended reports whether the trip is over at now.
func (c Card) Ended(now time.Time) bool { return now.Unix() >= c.Ends }

// Check validates the card's fields (not its signature).
func (c Card) Check() error {
	switch {
	case c.V != 1:
		return errors.New("trip: unknown card version")
	case strings.TrimSpace(c.Name) == "" || len(c.Name) > MaxTripName || !utf8.ValidString(c.Name):
		return errors.New("trip: name must be 1–64 bytes")
	case len(c.Leaders) == 0 || len(c.Leaders) > MaxLeaders:
		return fmt.Errorf("trip: 1–%d leaders", MaxLeaders)
	case c.Every < MinEvery || c.Every > MaxEvery:
		return fmt.Errorf("trip: report interval must be %d–%d s", MinEvery, MaxEvery)
	case c.Ends <= c.Made || c.Ends-c.Made > MaxDays*86400:
		return fmt.Errorf("trip: must end after it starts, within %d days", MaxDays)
	}
	if _, err := c.Public(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range c.Leaders {
		if _, err := packet.ParseHash(l.ID); err != nil || seen[strings.ToLower(l.ID)] {
			return errors.New("trip: bad or repeated leader id")
		}
		if len(l.Name) > 64 {
			return errors.New("trip: leader name too long")
		}
		if l.Key != "" {
			b, err := hex.DecodeString(l.Key)
			pub, err2 := identity.PublicFromBytes(b)
			if err != nil || err2 != nil || !strings.EqualFold(pub.Hash().String(), l.ID) {
				return errors.New("trip: a leader's key does not match their id")
			}
		}
		seen[strings.ToLower(l.ID)] = true
	}
	return nil
}

// Signed is a card with the trip key's signature: only someone holding the
// trip's secret (a leader) can make one.
type Signed struct {
	Card json.RawMessage `json:"card"`
	Sig  string          `json:"sig"` // hex Ed25519 over domain || card bytes
}

// Sign signs c with the trip identity id (whose public key must be c.Key).
func Sign(id *identity.Identity, c Card) (Signed, error) {
	if c.Key != hex.EncodeToString(id.Bytes()) {
		return Signed{}, errors.New("trip: card key is not this trip's")
	}
	if err := c.Check(); err != nil {
		return Signed{}, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Signed{}, err
	}
	return Signed{Card: b, Sig: hex.EncodeToString(id.SignDomain(signDomain, b))}, nil
}

// Open verifies the signature and returns the card.
func (s Signed) Open() (Card, error) {
	var c Card
	if err := json.Unmarshal(s.Card, &c); err != nil {
		return c, fmt.Errorf("trip: card: %w", err)
	}
	if err := c.Check(); err != nil {
		return c, err
	}
	pub, _ := c.Public()
	sig, err := hex.DecodeString(s.Sig)
	if err != nil || !pub.Verify(signDomain, s.Card, sig) {
		return c, errors.New("trip: bad card signature")
	}
	return c, nil
}

// JSON is the signed card as JSON (what GET /v1/trip serves).
func (s Signed) JSON() []byte {
	b, _ := json.Marshal(s)
	return b
}

// Digest identifies this exact signed card (for signed uploads).
func (s Signed) Digest() [32]byte { return sha256.Sum256(s.JSON()) }

// ParseSigned reads a signed card from JSON or a meshtrip: link.
func ParseSigned(s string) (Signed, error) {
	var out Signed
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, uriScheme+":"); ok {
		b, err := base64.RawURLEncoding.DecodeString(rest)
		if err != nil {
			return out, fmt.Errorf("trip: link: %w", err)
		}
		s = string(b)
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return out, fmt.Errorf("trip: card: %w", err)
	}
	return out, nil
}

// URI is the signed card as a meshtrip: link (for a QR code).
func (s Signed) URI() string {
	return uriScheme + ":" + base64.RawURLEncoding.EncodeToString(s.JSON())
}

// Report is one location report.
type Report struct {
	Flags    uint8
	Lat, Lon float64 // degrees
	Accuracy int     // meters (0 unknown)
	FixAge   int     // seconds since the fix
	Battery  int     // percent, -1 unknown
	Name     string  // the member's name, as they set it
}

// Encode returns the report content (kind 1).
func (r Report) Encode() []byte {
	b := make([]byte, 0, 16+MaxName)
	b = append(b, KindReport, r.Flags)
	b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(clamp(r.Lat, -90, 90)*1e7))))
	b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(clamp(r.Lon, -180, 180)*1e7))))
	b = binary.BigEndian.AppendUint16(b, uint16(min(max(r.Accuracy, 0), 65535)))
	b = binary.BigEndian.AppendUint16(b, uint16(min(max(r.FixAge, 0), 65535)))
	bat := byte(255)
	if r.Battery >= 0 && r.Battery <= 100 {
		bat = byte(r.Battery)
	}
	b = append(b, bat)
	name := truncate(r.Name, MaxName)
	b = append(b, byte(len(name)))
	return append(b, name...)
}

// DecodeReport parses kind-1 content.
func DecodeReport(b []byte) (Report, error) {
	var r Report
	if len(b) < 16 || b[0] != KindReport {
		return r, errors.New("trip: not a report")
	}
	n := int(b[15])
	if len(b) < 16+n {
		return r, errors.New("trip: report truncated")
	}
	r.Flags = b[1]
	r.Lat = float64(int32(binary.BigEndian.Uint32(b[2:]))) / 1e7
	r.Lon = float64(int32(binary.BigEndian.Uint32(b[6:]))) / 1e7
	r.Accuracy = int(binary.BigEndian.Uint16(b[10:]))
	r.FixAge = int(binary.BigEndian.Uint16(b[12:]))
	r.Battery = int(b[14])
	if r.Battery == 255 {
		r.Battery = -1
	}
	r.Name = strings.ToValidUTF8(string(b[16:16+n]), "")
	return r, nil
}

// Locate asks a member to report now and often for minutes (kind 2).
func Locate(tripHash packet.Hash, minutes int) []byte {
	b := append([]byte{KindLocate}, tripHash[:]...)
	return append(b, byte(min(max(minutes, 1), 255)))
}

// DecodeLocate parses kind-2 content.
func DecodeLocate(b []byte) (tripHash packet.Hash, minutes int, err error) {
	if len(b) < 18 || b[0] != KindLocate {
		return tripHash, 0, errors.New("trip: not a locate request")
	}
	copy(tripHash[:], b[1:17])
	return tripHash, int(b[17]), nil
}

// Lead hands a trip's secret to another leader (kind 3).
func Lead(id *identity.Identity, s Signed) []byte {
	b := append([]byte{KindLead}, id.Seed()...)
	return append(b, s.JSON()...)
}

// DecodeLead parses kind-3 content and checks that the secret belongs to the
// card.
func DecodeLead(b []byte) (*identity.Identity, Signed, Card, error) {
	if len(b) < 65 || b[0] != KindLead {
		return nil, Signed{}, Card{}, errors.New("trip: not a lead message")
	}
	id, err := identity.FromSeed(b[1:65])
	if err != nil {
		return nil, Signed{}, Card{}, err
	}
	var s Signed
	if err := json.Unmarshal(b[65:], &s); err != nil {
		return nil, Signed{}, Card{}, fmt.Errorf("trip: lead card: %w", err)
	}
	c, err := s.Open()
	if err != nil {
		return nil, Signed{}, Card{}, err
	}
	if c.Key != hex.EncodeToString(id.Bytes()) {
		return nil, Signed{}, Card{}, errors.New("trip: lead secret does not match the card")
	}
	return id, s, c, nil
}

// Kind returns the kind byte of trip content (0 if empty).
func Kind(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return int(b[0])
}

// MapKey is what the relay's web map needs to open reports: the trip's
// X25519 private key (hex). It can read reports but not sign cards or fetch
// the mailbox.
func MapKey(id *identity.Identity) string { return hex.EncodeToString(id.XPrivate()) }

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(math.Max(v, lo), hi)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Distance returns the great-circle distance in meters.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Bearing returns the initial bearing from point 1 to point 2 in degrees
// (0 = north, clockwise).
func Bearing(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dl := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(dl) * math.Cos(p2)
	x := math.Cos(p1)*math.Sin(p2) - math.Sin(p1)*math.Cos(p2)*math.Cos(dl)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

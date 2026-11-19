package trip

import (
	"encoding/hex"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

func testCard(t *testing.T) (*identity.Identity, Card) {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	lead, _ := identity.Generate()
	now := time.Unix(1_800_000_000, 0)
	return id, Card{
		V: 1, Name: "اربعین ۱۴۰۵", Key: hex.EncodeToString(id.Bytes()),
		Leaders: []Leader{{ID: lead.Hash().String(), Name: "Ali"}},
		Every:   300, Made: now.Unix(), Ends: now.Add(10 * 24 * time.Hour).Unix(),
	}
}

func TestSignOpen(t *testing.T) {
	id, c := testCard(t)
	s, err := Sign(id, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Open()
	if err != nil || got.Name != c.Name || got.Hash() != id.Hash().String() {
		t.Fatalf("open: %v %+v", err, got)
	}
	// Through a link and back.
	s2, err := ParseSigned(s.URI())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Open(); err != nil {
		t.Fatal(err)
	}
	// A changed card no longer verifies.
	bad := s
	bad.Card = []byte(strings.Replace(string(s.Card), "300", "60", 1))
	if _, err := bad.Open(); err == nil {
		t.Fatal("tampered card verified")
	}
	// Someone else's key can't sign it.
	other, _ := identity.Generate()
	if _, err := Sign(other, c); err == nil {
		t.Fatal("signed with a foreign key")
	}
}

func TestCheck(t *testing.T) {
	_, c := testCard(t)
	for name, f := range map[string]func(*Card){
		"no leaders": func(c *Card) { c.Leaders = nil },
		"too fast":   func(c *Card) { c.Every = 10 },
		"too long":   func(c *Card) { c.Ends = c.Made + 100*86400 },
		"ended":      func(c *Card) { c.Ends = c.Made },
		"no name":    func(c *Card) { c.Name = " " },
		"dup leader": func(c *Card) { c.Leaders = append(c.Leaders, c.Leaders[0]) },
		"bad key":    func(c *Card) { c.Key = "zz" },
	} {
		cc := c
		cc.Leaders = append([]Leader(nil), c.Leaders...)
		f(&cc)
		if cc.Check() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestReport(t *testing.T) {
	r := Report{Flags: FlagSOS | FlagAnswer, Lat: 32.6160567, Lon: 44.0248793, Accuracy: 12, FixAge: 40, Battery: 63,
		Name: "Mohammad Reza Hosseini Tabatabaei"}
	b := r.Encode()
	if len(b) > 16+MaxName {
		t.Fatalf("report %d bytes", len(b))
	}
	got, err := DecodeReport(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != r.Flags || math.Abs(got.Lat-r.Lat) > 1e-7 || math.Abs(got.Lon-r.Lon) > 1e-7 ||
		got.Accuracy != 12 || got.FixAge != 40 || got.Battery != 63 || got.Name != r.Name[:MaxName] {
		t.Fatalf("got %+v", got)
	}
	// Unknown battery, a multi-byte name cut on a character boundary.
	r = Report{Lat: -33.9, Lon: -151.2, Battery: -1, Name: strings.Repeat("ح", 20)}
	got, _ = DecodeReport(r.Encode())
	if got.Battery != -1 || len(got.Name) != 24 || got.Lon > -151 {
		t.Fatalf("got %+v", got)
	}
	if _, err := DecodeReport([]byte{KindReport, 0, 1}); err == nil {
		t.Fatal("short report accepted")
	}
}

func TestLocateLead(t *testing.T) {
	id, c := testCard(t)
	h := id.Hash()
	got, min, err := DecodeLocate(Locate(h, 30))
	if err != nil || got != h || min != 30 {
		t.Fatalf("locate: %v %v %d", err, got, min)
	}
	s, _ := Sign(id, c)
	id2, _, c2, err := DecodeLead(Lead(id, s))
	if err != nil || id2.Hash() != h || c2.Name != c.Name {
		t.Fatalf("lead: %v", err)
	}
	// A secret that doesn't belong to the card is refused.
	other, _ := identity.Generate()
	if _, _, _, err := DecodeLead(Lead(other, s)); err == nil {
		t.Fatal("foreign secret accepted")
	}
	var zero packet.Hash
	if _, _, err := DecodeLocate([]byte{KindLocate}); err == nil || zero != (packet.Hash{}) {
		t.Fatal("short locate accepted")
	}
}

func TestDistanceBearing(t *testing.T) {
	// Najaf → Karbala, about 77 km roughly north-north-west.
	d := Distance(32.0283, 44.3466, 32.6160, 44.0249)
	b := Bearing(32.0283, 44.3466, 32.6160, 44.0249)
	if d < 70000 || d > 80000 || b < 320 || b > 345 {
		t.Fatalf("distance %.0f m bearing %.0f°", d, b)
	}
}

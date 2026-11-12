package announce

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

func TestAnnounce(t *testing.T) {
	id, _ := identity.Generate()
	p := Build(id, time.Unix(1700000000, 0), AppData{Name: "Sara", Flags: FlagDelegate}, 8)
	raw := p.MustEncode()
	if len(raw) > packet.LoRaMTU {
		t.Fatalf("announce does not fit LoRa: %d", len(raw))
	}
	q, _ := packet.Decode(raw)
	a, err := Parse(q)
	if err != nil {
		t.Fatal(err)
	}
	if a.Public != id.Public || a.App.Name != "Sara" || a.App.Flags != FlagDelegate || a.Time.Unix() != 1700000000 {
		t.Fatalf("bad announce %+v", a)
	}
	raw[40] ^= 1 // flip a key bit
	q, _ = packet.Decode(raw)
	if _, err := Parse(q); err == nil {
		t.Fatal("tampered announce accepted")
	}
}

func TestReceipt(t *testing.T) {
	id, _ := identity.Generate()
	other, _ := identity.Generate()
	acked := packet.NewID()
	p := BuildReceipt(id, acked, 8)
	q, _ := packet.Decode(p.MustEncode())
	got, err := VerifyReceipt(q, id.Public)
	if err != nil || got != acked {
		t.Fatal("valid receipt rejected", err)
	}
	if _, err := VerifyReceipt(q, other.Public); err == nil {
		t.Fatal("receipt verified with wrong key")
	}
	if BuildReceipt(id, acked, 8).ID != p.ID {
		t.Fatal("receipt ID must be deterministic")
	}
}

func TestFetch(t *testing.T) {
	id, _ := identity.Generate()
	nonce := []byte("0123456789abcdef")
	if !VerifyFetch(id.Public, nonce, SignFetch(id, nonce)) {
		t.Fatal("fetch signature rejected")
	}
}

func TestRelayHint(t *testing.T) {
	id, _ := identity.Generate()
	for _, tc := range []struct{ name, relay, want string }{
		{"Sara", "http://87.106.5.192", "http://87.106.5.192"},
		{"", "https://relay.example.org:8443/m", "https://relay.example.org:8443/m"},
		{"ن ن ن ن ن ن ن ن ن ن ن", "http://87.106.5.192", "http://87.106.5.192"}, // 32-byte name still fits
		{"x", "ftp://nope", ""},
		{"Sara", "https://" + string(make([]byte, 60)), ""}, // too long: left out, announce still valid
	} {
		p := Build(id, time.Unix(1700000000, 0), AppData{Name: tc.name, Flags: FlagDelegate, Relay: tc.relay}, 8)
		raw := p.MustEncode()
		if len(raw) > packet.LoRaMTU {
			t.Fatalf("announce does not fit LoRa: %d", len(raw))
		}
		q, _ := packet.Decode(raw)
		a, err := Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		if a.App.Relay != tc.want || a.App.Flags != FlagDelegate {
			t.Fatalf("%q: got %+v", tc.relay, a.App)
		}
	}
}

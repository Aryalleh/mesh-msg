package group

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// reassemble decodes packets from the wire and rejoins fragments.
func reassemble(t *testing.T, pkts []*packet.Packet) *packet.Packet {
	t.Helper()
	r := message.NewReassembler(time.Hour)
	var full *packet.Packet
	for _, p := range pkts {
		raw := p.MustEncode()
		if len(raw) > packet.LoRaMTU {
			t.Fatalf("packet %d bytes > LoRa MTU", len(raw))
		}
		q, err := packet.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if f, err := r.Add(q, time.Now()); err != nil {
			t.Fatal(err)
		} else if f != nil {
			full = f
		}
	}
	if full == nil {
		t.Fatal("not reassembled")
	}
	return full
}

func TestPrivateGroupRoundTrip(t *testing.T) {
	g, err := New("Family", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// A member who joined by link derives the same group.
	m, err := ParseURI(g.URI())
	if err != nil {
		t.Fatal(err)
	}
	if m.Hash() != g.Hash() || m.Name != "Family" || m.Kind != Private {
		t.Fatalf("joined %+v", m)
	}
	alice, _ := identity.Generate()
	now := time.Unix(1_800_000_000, 0)
	for _, text := range []string{"سلام به همه", strings.Repeat("long message ", 60)} {
		pkts, err := g.BuildText(alice, "Alice", text, 0, false, now)
		if err != nil {
			t.Fatal(err)
		}
		got, err := m.Open(reassemble(t, pkts))
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != text || got.From.Hash() != alice.Hash() || got.FromName != "Alice" || !got.Sent.Equal(now) || got.Group != g.Hash() {
			t.Fatalf("got %+v", got)
		}
	}
	// A short Persian message fits one LoRa frame.
	pkts, _ := g.BuildText(alice, "Alice", "سلام، کجایی؟ ما در پارک هستیم", 0, false, now)
	if len(pkts) != 1 {
		t.Fatalf("short message took %d frames", len(pkts))
	}
}

func TestOutsidersAndForgery(t *testing.T) {
	g, _ := New("A", rand.Reader)
	other, _ := New("A", rand.Reader) // same name, other secret
	alice, _ := identity.Generate()
	pkts, _ := g.BuildText(alice, "Alice", "hi", 0, false, time.Now())
	p := reassemble(t, pkts)
	if _, err := other.Open(p); !errors.Is(err, ErrNotForGroup) {
		t.Fatal("other group opened it")
	}
	// Retargeted to the other group's address: wrong key and prologue.
	q := *p
	q.Dest = other.Hash()
	if _, err := other.Open(&q); err == nil {
		t.Fatal("retargeted packet opened")
	}
	// Tampered ciphertext.
	q = *p
	q.Body = append([]byte(nil), p.Body...)
	q.Body[10] ^= 1
	if _, err := g.Open(&q); err == nil {
		t.Fatal("tampered packet opened")
	}
	// A member re-encrypts a message claiming to be Alice, with a bad signature.
	mallory, _ := identity.Generate()
	pkts, _ = g.BuildText(mallory, "Alice", "send money", 0, false, time.Now())
	forged := reassemble(t, pkts)
	pt := openRaw(t, g, forged)
	copy(pt[0:64], append(alice.X[:], alice.Ed[:]...)) // claim Alice's keys
	if _, err := g.Open(sealRaw(g, forged, pt)); err == nil {
		t.Fatal("forged sender accepted")
	}
}

func TestChannels(t *testing.T) {
	a, _ := Channel("Tehran")
	b, _ := ParseURI("#tehran ")
	c, _ := ParseURI(a.URI())
	if a.Hash() != b.Hash() || a.Hash() != c.Hash() || a.Kind != Regional || a.Title() != "#tehran" {
		t.Fatalf("channel mismatch %v %v %v", a.Hash(), b.Hash(), c.Hash())
	}
	gl, _ := Channel("#Global")
	if gl.Kind != Global || gl.Hash() == a.Hash() {
		t.Fatal("global channel")
	}
	if _, err := Channel("  # "); err == nil {
		t.Fatal("empty channel accepted")
	}
	if _, err := ParseURI("meshmsg:abc"); err == nil {
		t.Fatal("contact link accepted as group")
	}
	// Channel derivation is fixed: changing it splits every channel.
	if got := gl.Hash().String(); got != globalHash {
		t.Fatalf("global channel hash changed: %s", got)
	}
}

func TestLongNameTruncated(t *testing.T) {
	g, _ := New("x", rand.Reader)
	alice, _ := identity.Generate()
	pkts, _ := g.BuildText(alice, strings.Repeat("ن", 40), "hi", 0, false, time.Now())
	m, err := g.Open(reassemble(t, pkts))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.FromName) > MaxNameLen || !strings.HasPrefix(m.FromName, "ن") {
		t.Fatalf("name %q", m.FromName)
	}
	if _, err := g.BuildText(alice, "", strings.Repeat("x", MaxTextBytes+1), 0, false, time.Now()); err == nil {
		t.Fatal("over-long text accepted")
	}
}

const globalHash = "22f95cf373ea4676fa73900e1e8d1f38"

func openRaw(t *testing.T, g *Group, p *packet.Packet) []byte {
	aead, _ := chacha20poly1305.New(g.key[:])
	pt, err := aead.Open(nil, nonce(p.ID), p.Body, packet.Prologue(packet.TypeData, p.ID, p.Dest))
	if err != nil {
		t.Fatal(err)
	}
	return pt
}

func sealRaw(g *Group, p *packet.Packet, pt []byte) *packet.Packet {
	aead, _ := chacha20poly1305.New(g.key[:])
	q := *p
	q.Body = aead.Seal(nil, nonce(p.ID), pt, packet.Prologue(packet.TypeData, p.ID, p.Dest))
	return &q
}

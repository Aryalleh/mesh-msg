package e2e

import (
	"bytes"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

func ids(t *testing.T, n int) []*identity.Identity {
	out := make([]*identity.Identity, n)
	for i := range out {
		var err error
		if out[i], err = identity.Generate(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestKnownContact(t *testing.T) {
	p := ids(t, 4)
	ali, sara, other1, other2 := p[0], p[1], p[2], p[3]
	id := packet.NewID()
	in := Inner{ContentType: ContentText, Time: time.Unix(1700000000, 0), Content: []byte("salam")}
	body, err := Seal(ali, sara.Public, true, id, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != OverheadK+InnerHeader+5 {
		t.Fatalf("unexpected size %d", len(body))
	}
	sender, first, got, err := Open(sara, []identity.Public{other1.Public, ali.Public, other2.Public}, id, body)
	if err != nil {
		t.Fatal(err)
	}
	if first || sender != ali.Public || !bytes.Equal(got.Content, in.Content) || !got.Time.Equal(in.Time) {
		t.Fatalf("bad open: first=%v sender ok=%v", first, sender == ali.Public)
	}
	// Unknown sender with K cannot be opened.
	if _, _, _, err := Open(sara, []identity.Public{other1.Public}, id, body); err != ErrNotForMe {
		t.Fatal("expected ErrNotForMe")
	}
	// Wrong recipient.
	if _, _, _, err := Open(other1, []identity.Public{ali.Public}, id, body); err != ErrNotForMe {
		t.Fatal("expected ErrNotForMe for wrong recipient")
	}
	// Header binding: a different packet ID must fail.
	if _, _, _, err := Open(sara, []identity.Public{ali.Public}, packet.NewID(), body); err != ErrNotForMe {
		t.Fatal("prologue not bound")
	}
}

func TestFirstContact(t *testing.T) {
	p := ids(t, 2)
	ali, sara := p[0], p[1]
	id := packet.NewID()
	body, err := Seal(ali, sara.Public, false, id, Inner{ContentType: ContentText, Time: time.Now(), Content: []byte("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != OverheadX+32+InnerHeader+2 {
		t.Fatalf("size %d", len(body))
	}
	sender, first, got, err := Open(sara, nil, id, body)
	if err != nil {
		t.Fatal(err)
	}
	if !first || sender != ali.Public || string(got.Content) != "hi" {
		t.Fatal("first contact open failed")
	}
}

func TestTamper(t *testing.T) {
	p := ids(t, 2)
	id := packet.NewID()
	body, _ := Seal(p[0], p[1].Public, true, id, Inner{Time: time.Now(), Content: []byte("x")})
	body[len(body)-1] ^= 1
	if _, _, _, err := Open(p[1], []identity.Public{p[0].Public}, id, body); err == nil {
		t.Fatal("tampered message accepted")
	}
}

func TestSingleLoRaFrameBudget(t *testing.T) {
	budget := packet.LoRaMTU - packet.HeaderSize - OverheadK - InnerHeader
	if budget != 176 {
		t.Fatalf("content budget per LoRa frame = %d", budget)
	}
}

package message

import (
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

func TestShortPersianFitsOneFrame(t *testing.T) {
	ali, _ := identity.Generate()
	sara, _ := identity.Generate()
	// ~170 Persian characters must fit one LoRa frame for a known contact.
	text := strings.Repeat("سلامدوستمن", 17) // 170 runes
	pkts, err := BuildText(ali, sara.Public, text, Options{KnownContact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkts) != 1 {
		t.Fatalf("%d runes became %d frames", len([]rune(text)), len(pkts))
	}
	got, err := Open(sara, []identity.Public{ali.Public}, pkts[0])
	if err != nil || got.Text != text || got.From != ali.Public {
		t.Fatal("open failed", err)
	}
}

func TestLongMessageFragments(t *testing.T) {
	ali, _ := identity.Generate()
	sara, _ := identity.Generate()
	text := strings.Repeat("A long message over LoRa. ", 30)
	pkts, err := BuildText(ali, sara.Public, text, Options{KnownContact: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkts) < 2 {
		t.Fatal("expected fragments")
	}
	r := NewReassembler(time.Hour)
	var full *packet.Packet
	for i := len(pkts) - 1; i >= 0; i-- {
		raw := pkts[i].MustEncode()
		if len(raw) > packet.LoRaMTU {
			t.Fatal("fragment exceeds LoRa MTU")
		}
		p, _ := packet.Decode(raw)
		full, err = r.Add(p, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && full != nil {
			t.Fatal("completed early")
		}
	}
	got, err := Open(sara, nil, full)
	if err != nil || got.Text != text || !got.FirstContact {
		t.Fatal("open failed", err)
	}
}

func TestTooLong(t *testing.T) {
	ali, _ := identity.Generate()
	if _, err := BuildText(ali, ali.Public, strings.Repeat("x", MaxTextBytes+1), Options{}); err == nil {
		t.Fatal("expected error")
	}
}

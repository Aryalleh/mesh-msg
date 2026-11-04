package sim

import (
	"testing"
	"time"
)

func TestAirtimeFormula(t *testing.T) {
	// SF9/125 kHz/4:5 ≈ 1758 bit/s: 255 B ≈ 1.16 s of payload + preamble/header → ~1.25 s.
	if got := Balanced.Airtime(255); got < 1200*time.Millisecond || got > 1300*time.Millisecond {
		t.Fatalf("airtime(255) = %v", got)
	}
	// 200 B ≈ 1 s (the figure used in docs/firmware.md).
	if got := Balanced.Airtime(200); got < 950*time.Millisecond || got > 1050*time.Millisecond {
		t.Fatalf("airtime(200) = %v", got)
	}
}

func TestLoRaJoinsRelaysShort(t *testing.T) {
	w := NewWorld(3)
	for _, s := range Scenarios {
		if s.Name == "lora-joins-relays" {
			s.Build(w)
		}
	}
	w.E.RunFor(4 * time.Hour)
	sent, delivered := 0, 0
	for _, c := range w.Stats.Categories {
		sent += c.Sent
		delivered += c.Delivered
	}
	// Messages sent in the last ~15 min may still be in flight.
	if sent == 0 || float64(delivered) < 0.8*float64(sent) {
		t.Fatalf("delivered %d of %d", delivered, sent)
	}
}

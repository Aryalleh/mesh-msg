package packet

import (
	"bytes"
	"testing"
)

func sample() *Packet {
	return &Packet{
		Type:    TypeData,
		Ctx:     CtxPriority,
		MaxHops: DefaultMaxHops,
		ID:      ID{1, 2, 3, 4, 5, 6, 7, 8},
		Dest:    Hash{0xaa, 0xbb},
		Body:    []byte("hello"),
	}
}

func TestRoundTrip(t *testing.T) {
	p := sample()
	raw := p.MustEncode()
	if len(raw) != HeaderSize+5 {
		t.Fatalf("size %d", len(raw))
	}
	if raw[0] != 0x41 { // version 1, type 0, ctx 1
		t.Fatalf("flags %#x", raw[0])
	}
	q, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != p.Type || q.Ctx != p.Ctx || q.MaxHops != p.MaxHops || q.ID != p.ID || q.Dest != p.Dest || !bytes.Equal(q.Body, p.Body) {
		t.Fatalf("mismatch: %+v", q)
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := Decode(make([]byte, 10)); err != ErrShort {
		t.Fatal(err)
	}
	raw := sample().MustEncode()
	raw[0] = 0x00 // version 0
	if _, err := Decode(raw); err != ErrVersion {
		t.Fatal(err)
	}
}

func TestIncHop(t *testing.T) {
	p := sample()
	p.MaxHops = 2
	raw := p.MustEncode()
	raw, ok := IncHop(raw)
	if !ok || raw[1] != 0x12 {
		t.Fatalf("first inc: %v %#x", ok, raw[1])
	}
	raw, ok = IncHop(raw)
	if !ok || raw[1] != 0x22 {
		t.Fatalf("second inc: %v %#x", ok, raw[1])
	}
	if _, ok = IncHop(raw); ok {
		t.Fatal("should stop at max hops")
	}
}

func TestFragmentReassemble(t *testing.T) {
	p := sample()
	p.Body = bytes.Repeat([]byte("x"), 600)
	frags, err := Fragment(p, LoRaMTU)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 3 {
		t.Fatalf("want 3 fragments, got %d", len(frags))
	}
	var decoded []*Packet
	for i := len(frags) - 1; i >= 0; i-- { // reverse order
		raw := frags[i].MustEncode()
		if len(raw) > LoRaMTU {
			t.Fatalf("fragment %d too big: %d", i, len(raw))
		}
		d, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, d)
	}
	q, err := Reassemble(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(q.Body, p.Body) || q.Fragmented() {
		t.Fatal("reassembly mismatch")
	}
}

func TestFrames(t *testing.T) {
	a, b := []byte("one"), []byte("second")
	got, err := DecodeFrames(EncodeFrames([][]byte{a, b}))
	if err != nil || len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
		t.Fatal("frames mismatch", err)
	}
	if _, err := DecodeFrames([]byte{0, 0, 0, 9, 1}); err == nil {
		t.Fatal("expected truncation error")
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(sample().MustEncode())
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := Decode(b)
		if err != nil {
			return
		}
		raw, err := p.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, b) {
			t.Fatalf("re-encode mismatch")
		}
	})
}

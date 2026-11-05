package hostlink

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestCRC16(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x29B1 { // CRC-16/CCITT-FALSE check value
		t.Fatalf("crc %04x", got)
	}
}

func TestCOBS(t *testing.T) {
	cases := [][]byte{
		{}, {0}, {0, 0}, {1}, {1, 0, 2}, bytes.Repeat([]byte{7}, 253), bytes.Repeat([]byte{7}, 254),
		bytes.Repeat([]byte{7}, 255), append(bytes.Repeat([]byte{7}, 254), 0), bytes.Repeat([]byte{0}, 300),
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		b := make([]byte, r.Intn(700))
		for j := range b {
			if r.Intn(4) == 0 {
				b[j] = 0
			} else {
				b[j] = byte(r.Intn(256))
			}
		}
		cases = append(cases, b)
	}
	for _, c := range cases {
		e := cobsEncode(c)
		if bytes.IndexByte(e, 0) >= 0 {
			t.Fatalf("zero in encoding of %x", c)
		}
		d, ok := cobsDecode(e)
		if !ok || !bytes.Equal(d, c) {
			t.Fatalf("round trip %x → %x → %x", c, e, d)
		}
	}
	for _, bad := range [][]byte{{0}, {5, 1}, {2}} {
		if _, ok := cobsDecode(bad); ok {
			t.Fatalf("accepted %x", bad)
		}
	}
}

func TestStream(t *testing.T) {
	var stream []byte
	var want []Frame
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 200; i++ {
		p := make([]byte, r.Intn(MaxPayload+1))
		r.Read(p)
		f := Frame{Kind: byte(r.Intn(4)), Payload: p}
		e, err := Encode(f.Kind, f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, e...)
		want = append(want, f)
	}
	// Garbage before the first frame is discarded at the first delimiter.
	stream = append([]byte{9, 9, 9, 0}, stream...)
	var d Decoder
	var got []Frame
	for len(stream) > 0 { // arbitrary chunking
		n := 1 + r.Intn(64)
		if n > len(stream) {
			n = len(stream)
		}
		got = append(got, d.Feed(stream[:n])...)
		stream = stream[n:]
	}
	if len(got) != len(want) || d.Errors != 1 {
		t.Fatalf("got %d frames, %d errors", len(got), d.Errors)
	}
	for i := range got {
		if got[i].Kind != want[i].Kind || !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("frame %d differs", i)
		}
	}
	if _, err := Encode(0, make([]byte, MaxPayload+1)); err != ErrTooLarge {
		t.Fatal("oversize accepted")
	}
}

func TestCorruptionResync(t *testing.T) {
	a, _ := Encode(KindPacket, []byte("first"))
	b, _ := Encode(KindEvent, []byte("second"))
	bad := append([]byte(nil), a...)
	bad[3] ^= 0x40 // CRC fails
	var d Decoder
	got := d.Feed(append(append(bad, b...), make([]byte, 2000)...))
	if len(got) != 1 || string(got[0].Payload) != "second" || d.Errors != 1 {
		t.Fatalf("got %v errors %d", got, d.Errors)
	}
	// A run without delimiters longer than any frame is dropped as one error.
	d = Decoder{}
	got = d.Feed(append(bytes.Repeat([]byte{1}, 3000), append([]byte{0}, b...)...))
	if len(got) != 1 || d.Errors != 1 {
		t.Fatalf("overflow: got %d frames, %d errors", len(got), d.Errors)
	}
}

func TestConfigTLV(t *testing.T) {
	c := DefaultNodeConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Name = "roof-1"
	c.Role = RoleGateway
	var d NodeConfig = DefaultNodeConfig()
	if err := d.ApplyTLV(c.EncodeTLV()); err != nil || d != c {
		t.Fatalf("round trip %+v %v", d, err)
	}
	// Partial update.
	if err := d.ApplyTLV([]byte{TagPreset, 1, PresetLong}); err != nil || d.Preset != PresetLong || d.Name != "roof-1" {
		t.Fatal("partial")
	}
	before := d
	for _, bad := range [][]byte{
		{TagRole, 1, 9}, {TagRegion, 1, 99}, {TagPreset, 1, 3}, {TagTxDBm, 1, 40},
		{TagFreqHz, 4, 0x33, 0x00, 0x00, 0x00}, {TagRole, 2, 0, 0}, {TagRole}, {TagRole, 5, 1},
		append([]byte{TagName, 33}, bytes.Repeat([]byte{'a'}, 33)...),
	} {
		if err := d.ApplyTLV(bad); err == nil {
			t.Fatalf("accepted %x", bad)
		}
		if d != before {
			t.Fatal("config changed on error")
		}
	}
	if err := d.ApplyTLV([]byte{99, 1, 0}); err != nil {
		t.Fatal("unknown tag must be skipped")
	}
	for _, r := range Regions {
		if r.FreqHz < r.MinHz || r.FreqHz > r.MaxHz {
			t.Errorf("%s default outside its range", r.Name)
		}
		if _, ok := RegionByName(r.Name); !ok || Regions[r.ID].Name != r.Name {
			t.Errorf("%s index", r.Name)
		}
	}
}

func TestStatus(t *testing.T) {
	s := Status{1, RoleRepeater, 3, PresetLong, 906875000, -3, 0xab, 123456, 64, 3, 5, 1000, 36000, 77, 66, 3700}
	b := s.Encode()
	if len(b) != StatusSize {
		t.Fatal(len(b))
	}
	got, err := ParseStatus(b)
	if err != nil || got != s {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := ParseStatus(b[1:]); err == nil {
		t.Fatal("short status accepted")
	}
	m, err := ParseMessage(Command(OpWipe, 7, WipeConfirm))
	if err != nil || m.Op != OpWipe || m.Req != 7 || string(m.Data) != "WIPE" {
		t.Fatal("message")
	}
	if _, err := ParseMessage([]byte{1}); err == nil {
		t.Fatal("short message accepted")
	}
}

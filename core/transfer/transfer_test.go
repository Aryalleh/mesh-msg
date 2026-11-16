package transfer

import (
	"bytes"
	"testing"
	"time"
)

func TestSplitAndReassembleLargeBody(t *testing.T) {
	body := bytes.Repeat([]byte("mava-transfer-"), 1000)
	var id ID
	copy(id[:], []byte("transfer1"))
	chunks, err := Split(id, body, 216)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 16 {
		t.Fatalf("expected >16 chunks, got %d", len(chunks))
	}
	r := NewReceiver(time.Unix(1, 0))
	for _, c := range chunks {
		if _, err := r.Add(c, time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("reassembled body differs")
	}
}
func TestEnvelopeRoundTrip(t *testing.T) {
	var id ID
	copy(id[:], []byte("12345678"))
	c := Chunk{ID: id, Index: 7, Total: 100, Flags: 1, Body: []byte("hello")}
	b, err := c.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID || got.Index != c.Index || got.Total != c.Total || got.Flags != c.Flags || !bytes.Equal(got.Body, c.Body) {
		t.Fatal("round trip failed")
	}
}
func TestSelectiveAck(t *testing.T) {
	var id ID
	a := Ack{ID: id, Base: 10, Bitmap: (1 << 0) | (1 << 3) | (1 << 15)}
	if !a.Has(10) || !a.Has(13) || !a.Has(25) || a.Has(11) {
		t.Fatal("ack bitmap incorrect")
	}
}

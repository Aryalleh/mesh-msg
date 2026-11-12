package media

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestSealOpen(t *testing.T) {
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 77} {
		file := make([]byte, n)
		rand.Read(file)
		blob, key, id, err := Seal(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(blob) != SealedSize(n) || PlainSize(len(blob)) != n {
			t.Fatalf("%d: sizes %d %d", n, len(blob), PlainSize(len(blob)))
		}
		got, err := Open(blob, key, id)
		if err != nil || !bytes.Equal(got, file) {
			t.Fatalf("%d: %v", n, err)
		}
		if n > ChunkSize {
			// Cut after a whole chunk: id no longer matches; even with a
			// matching id the missing last-chunk flag is caught.
			cut := blob[:ChunkSize+16]
			if _, err := Open(cut, key, BlobID(cut)); err == nil {
				t.Fatal("truncated blob opened")
			}
		}
		blob[len(blob)-1] ^= 1
		if _, err := Open(blob, key, BlobID(blob)); err == nil {
			t.Fatal("tampered blob opened")
		}
	}
}

func TestRef(t *testing.T) {
	r := Ref{Size: 12345, Mime: "image/jpeg", Name: "../عکس.jpg", URLs: []string{"http://87.106.5.192", "https://r.example"}, Caption: "شام دیشب"}
	rand.Read(r.Key[:])
	rand.Read(r.ID[:])
	b := r.Encode()
	if len(b) > 200 {
		t.Fatalf("reference %d bytes", len(b))
	}
	got, err := DecodeRef(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != r.ID || got.Key != r.Key || got.Size != r.Size || got.Mime != r.Mime || got.Name != ".._عکس.jpg" ||
		len(got.URLs) != 2 || got.URLs[1] != "https://r.example" || got.Caption != r.Caption {
		t.Fatalf("got %+v", got)
	}
	for i := 0; i < len(b); i++ {
		DecodeRef(b[:i]) // no panics
	}
}

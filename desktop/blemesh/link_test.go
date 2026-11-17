package blemesh

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/router"
)

type fakeHost struct {
	mu     sync.Mutex
	in     [][]byte
	up     []router.PeerID
	down   []router.PeerID
	status string
}

func (h *fakeHost) Inbound(_ string, _ router.PeerID, raw []byte) {
	h.mu.Lock()
	h.in = append(h.in, raw)
	h.mu.Unlock()
}
func (h *fakeHost) PeerUp(p router.PeerID)   { h.mu.Lock(); h.up = append(h.up, p); h.mu.Unlock() }
func (h *fakeHost) PeerDown(p router.PeerID) { h.mu.Lock(); h.down = append(h.down, p); h.mu.Unlock() }
func (h *fakeHost) Status(s string)          { h.mu.Lock(); h.status = s; h.mu.Unlock() }

// writes collects what a peer receives, in order.
type writes struct {
	mu   sync.Mutex
	got  [][]byte
	fail bool
}

func (w *writes) write(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return errors.New("gone")
	}
	w.got = append(w.got, append([]byte(nil), b...))
	return nil
}

func (w *writes) all() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.got...)
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHelloFirstThenChunkedFrames(t *testing.T) {
	h := &fakeHost{}
	l := New(h)
	w := &writes{}
	l.Up("aa", 23, Hello([4]byte{1, 2, 3, 4}), w.write, nil)
	if h.status != "1 peer" || len(h.up) != 1 {
		t.Fatalf("up: %q %v", h.status, h.up)
	}
	raw := bytes.Repeat([]byte{7}, 50)
	l.Send("aa", raw)
	waitFor(t, "writes", func() bool { return len(w.all()) == 4 })
	got := w.all()
	if !bytes.Equal(got[0], []byte{0x4D, 1, 2, 3, 4}) {
		t.Fatalf("first write %x is not the hello", got[0])
	}
	// 3 + 50 bytes in 20-byte writes (MTU 23): 20, 20, 13.
	stream := append(append(append([]byte{}, got[1]...), got[2]...), got[3]...)
	if len(got[1]) != 20 || len(got[3]) != 13 || !bytes.Equal(stream, append([]byte{0, 0, 50}, raw...)) {
		t.Fatalf("stream %x", stream)
	}
}

func TestReceiveAcrossNotificationsAndBadFrames(t *testing.T) {
	h := &fakeHost{}
	l := New(h)
	l.Up("bb", 247, Hello([4]byte{}), (&writes{}).write, nil)
	a, b := []byte("first packet"), []byte("second")
	stream := append(append([]byte{0, 0, byte(len(a))}, a...), append([]byte{0, 0, byte(len(b))}, b...)...)
	for i := 0; i < len(stream); i += 5 {
		if err := l.Receive("bb", stream[i:min(i+5, len(stream))]); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.in) != 2 || !bytes.Equal(h.in[0], a) || !bytes.Equal(h.in[1], b) {
		t.Fatalf("inbound %q", h.in)
	}
	if err := l.Receive("bb", []byte{0, 0, 0}); err == nil || l.Has("bb") {
		t.Fatal("zero-length frame accepted")
	}
	if len(h.down) != 1 || h.status != "no peers" {
		t.Fatalf("down %v status %q", h.down, h.status)
	}
	if l.Receive("zz", []byte{1}) == nil {
		t.Fatal("unknown peer accepted")
	}
}

func TestWriteFailureDropsPeer(t *testing.T) {
	h := &fakeHost{}
	l := New(h)
	closed := false
	w := &writes{fail: true}
	l.Up("cc", 100, Hello([4]byte{}), w.write, func() { closed = true })
	waitFor(t, "drop", func() bool { return !l.Has("cc") })
	if !closed || len(h.down) != 1 {
		t.Fatalf("closed %v down %v", closed, h.down)
	}
	l.Send("cc", []byte{1}) // gone: ignored
}

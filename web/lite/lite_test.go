package lite

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

type memStore struct {
	mu sync.Mutex
	b  []byte
}

func (m *memStore) Load() ([]byte, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.b, nil }
func (m *memStore) Save(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.b = append([]byte(nil), b...)
	return nil
}

func startRelay(t *testing.T) string {
	t.Helper()
	receiptPoll = 200 * time.Millisecond
	key, _ := identity.Generate()
	r := relay.New(relay.Config{Name: "t", Region: "global"})
	h := api.New(api.Config{Relay: r, Identity: key, PowBits: 8, RateLimit: 1000,
		Relays: []wire.RelayEntry{{Name: "t", Region: "global", URLs: []string{"https://t"}}}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

type events struct {
	mu sync.Mutex
	l  []Event
}

func (e *events) add(ev Event) { e.mu.Lock(); e.l = append(e.l, ev); e.mu.Unlock() }
func (e *events) has(kind, detail string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, x := range e.l {
		if x.Kind == kind && (detail == "" || x.Detail == detail) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newClient(t *testing.T, name string) (*Client, *memStore, *events) {
	t.Helper()
	st := &memStore{}
	c, err := Open(st, name)
	if err != nil {
		t.Fatal(err)
	}
	ev := &events{}
	c.OnEvent(ev.add)
	return c, st, ev
}

// Two browsers on a relay: text both ways, Persian included, receipts mark
// messages delivered, the second message uses the known-contact form, and
// the state survives a reload.
func TestTwoBrowsersThroughRelay(t *testing.T) {
	url := startRelay(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ali, aliStore, _ := newClient(t, "Ali")
	sara, _, saraEv := newClient(t, "Sara")
	for _, c := range []*Client{ali, sara} {
		r, err := NewRelay(c, url)
		if err != nil {
			t.Fatal(err)
		}
		c.AddTransport(r)
		go r.Run(ctx)
	}
	if _, err := ali.AddContact(sara.URI(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ali.AddContact(ali.URI(), ""); err == nil {
		t.Fatal("added ourselves")
	}
	m, err := ali.Send(sara.Hash(), "سلام سارا، از مرورگر")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Sara receives", func() bool { return len(sara.Messages(ali.Hash())) == 1 })
	got := sara.Messages(ali.Hash())[0]
	if got.Text != "سلام سارا، از مرورگر" || got.Mine || got.Status != "received" || !saraEv.has("message", "") {
		t.Fatalf("Sara got %+v", got)
	}
	// Sara learned Ali as a new contact (first contact). Relays do not put
	// announces in a mailbox, so his name shows once she names him (or an
	// announce arrives over the board).
	if cs := sara.Contacts(); len(cs) != 1 || cs[0].Hash != ali.Hash() || cs[0].Known != true {
		t.Fatalf("Sara's contacts: %+v", cs)
	}
	waitFor(t, "delivered", func() bool { l := ali.Messages(sara.Hash()); return l[0].ID == m.ID && l[0].Status == "delivered" })

	if _, err := sara.Send(ali.Hash(), "hi Ali"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ali receives", func() bool { return len(ali.Messages(sara.Hash())) == 2 })
	if !ali.Contacts()[0].Known {
		t.Fatal("Sara wrote to Ali, so she has his key")
	}
	if _, err := ali.Send(sara.Hash(), strings.Repeat("یک پیام بلند ", 40)); err != nil { // several fragments, Noise K
		t.Fatal(err)
	}
	waitFor(t, "long message", func() bool { return len(sara.Messages(ali.Hash())) == 3 })

	// Reload: same identity, contacts and messages.
	again, err := Open(aliStore, "")
	if err != nil || again.Hash() != ali.Hash() || len(again.Messages(sara.Hash())) != 3 || again.Cursor(url) == 0 {
		t.Fatalf("reload: %v %d", err, len(again.Messages(sara.Hash())))
	}
}

// fakeBoard is a Mava board seen from /api/link: it passes every packet one
// browser session sends to the other sessions, as LoRa would.
type fakeBoard struct {
	mu   sync.Mutex
	dec  map[string]*hostlink.Decoder
	out  map[string][]byte
	seen map[string]bool
}

func (b *fakeBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s := r.URL.Query().Get("s")
	body, _ := io.ReadAll(r.Body)
	in, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dec[s] == nil {
		b.dec[s] = &hostlink.Decoder{}
	}
	for _, f := range b.dec[s].Feed(in) {
		if f.Kind != hostlink.KindPacket {
			continue
		}
		for other := range b.dec {
			if other != s {
				enc, _ := hostlink.Encode(hostlink.KindPacket, f.Payload)
				b.out[other] = append(b.out[other], enc...)
			}
		}
	}
	io.WriteString(w, base64.StdEncoding.EncodeToString(b.out[s]))
	b.out[s] = nil
}

func TestTwoBrowsersThroughBoard(t *testing.T) {
	fb := &fakeBoard{dec: map[string]*hostlink.Decoder{}, out: map[string][]byte{}, seen: map[string]bool{}}
	srv := httptest.NewServer(fb)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _, aEv := newClient(t, "A")
	b, _, _ := newClient(t, "B")
	ba := NewBoard(a, srv.URL, "a")
	bb := NewBoard(b, srv.URL, "b")
	a.AddTransport(ba)
	b.AddTransport(bb)
	go ba.Run(ctx)
	go bb.Run(ctx)
	waitFor(t, "board online", func() bool { return aEv.has("link", "board: online") })
	if _, err := b.AddContact(a.URI(), "A"); err != nil {
		t.Fatal(err)
	}
	// B's first poll may come after A's announce; send until it arrives.
	if _, err := b.Send(a.Hash(), "over LoRa"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "A receives over the board", func() bool { return len(a.Messages(b.Hash())) == 1 })
	waitFor(t, "receipt over the board", func() bool { return b.Messages(a.Hash())[0].Status == "delivered" })
}

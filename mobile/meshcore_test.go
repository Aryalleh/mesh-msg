package meshcore

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

// ---- helpers ----

type events struct {
	mu sync.Mutex
	ch chan node.Event
}

func (e *events) OnEvent(s string) {
	var ev node.Event
	if json.Unmarshal([]byte(s), &ev) == nil {
		select {
		case e.ch <- ev:
		default:
		}
	}
}

func (e *events) wait(t *testing.T, typ string, d time.Duration) node.Event {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-e.ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", typ)
		}
	}
}

func phone(t *testing.T, cfg string) (*Core, *events) {
	t.Helper()
	c, err := Open(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ev := &events{ch: make(chan node.Event, 256)}
	c.SetListener(ev)
	c.Start()
	t.Cleanup(c.Stop)
	return c, ev
}

// ble is one end of a fake BLE connection: chunks are delivered to the other
// phone in order, on another goroutine, as GATT would.
type ble struct {
	to   *Core
	self string // how the other side names us
	q    chan []byte
}

func (b *ble) Send(_ string, chunk []byte) { b.q <- append([]byte(nil), chunk...) }

// connectBLE joins two phones with a chunk size of mtu.
func connectBLE(t *testing.T, a, b *Core, mtu int) {
	t.Helper()
	ab := &ble{to: b, self: "phone-a", q: make(chan []byte, 4096)}
	ba := &ble{to: a, self: "phone-b", q: make(chan []byte, 4096)}
	if err := a.AddTransport("ble", mtu, ab); err != nil {
		t.Fatal(err)
	}
	if err := b.AddTransport("ble", mtu, ba); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	for _, e := range []*ble{ab, ba} {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case c := <-e.q:
					e.to.Receive("ble", e.self, c)
				}
			}
		}()
	}
	if err := a.PeerUp("ble", "phone-b"); err != nil {
		t.Fatal(err)
	}
	if err := b.PeerUp("ble", "phone-a"); err != nil {
		t.Fatal(err)
	}
}

func startRelay(t *testing.T) string {
	key, _ := identity.Generate()
	srv := httptest.NewServer(api.New(api.Config{
		Relay: relay.New(relay.Config{Name: "ir-1", Region: "ir"}), Identity: key, PowBits: 4, RateLimit: 1000,
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func mustAdd(t *testing.T, c *Core, uri, name string) {
	t.Helper()
	if _, err := c.AddContact(uri, name); err != nil {
		t.Fatal(err)
	}
}

// ---- tests ----

// Two phones with no internet at all chat over the BLE transport, with a
// small GATT-sized chunk so packets are split and reassembled.
func TestChatOverBLE(t *testing.T) {
	a, aev := phone(t, "")
	b, bev := phone(t, "")
	mustAdd(t, a, b.URI(), "b")
	mustAdd(t, b, a.URI(), "a")
	connectBLE(t, a, b, 20)

	long := strings.Repeat("سلام از شبکه‌ی مش ", 40) // several hundred bytes
	if _, err := a.Send("b", long); err != nil {
		t.Fatal(err)
	}
	if e := bev.wait(t, "message", 20*time.Second); e.Message.Text != long || e.Message.Peer != a.Hash() {
		t.Fatalf("got %+v", e.Message)
	}
	aev.wait(t, "delivered", 20*time.Second)

	// Chat list: one unread on b until it is marked read.
	var chats []node.Chat
	s, err := b.Chats()
	if err != nil || json.Unmarshal([]byte(s), &chats) != nil {
		t.Fatal(err, s)
	}
	if len(chats) != 1 || chats[0].Unread != 1 || chats[0].Last == nil || chats[0].Last.Text != long {
		t.Fatalf("chats: %s", s)
	}
	if err := b.MarkRead("a"); err != nil {
		t.Fatal(err)
	}
	s, _ = b.Chats()
	json.Unmarshal([]byte(s), &chats)
	if chats[0].Unread != 0 {
		t.Fatalf("still unread: %s", s)
	}
	var st node.Status
	json.Unmarshal([]byte(a.Status()), &st)
	if st.Links["ble"] != "1 peer" {
		t.Fatalf("link status: %v", st.Links)
	}
	var ms []node.Message
	s, _ = a.Messages("b", 10)
	json.Unmarshal([]byte(s), &ms)
	if len(ms) != 1 || !ms[0].Delivered {
		t.Fatalf("history: %s", s)
	}
}

// The M5 exit criterion (docs/roadmap.md): a phone with no internet sends over
// BLE to a phone that is a gateway; it reaches a desktop through a relay, and
// the reply comes back the same way.
func TestPhoneBLEToGatewayPhoneToRelayToDesktop(t *testing.T) {
	url := startRelay(t)
	offline, offEv := phone(t, `{"name":"offline phone","delegate":true}`)
	gateway, _ := phone(t, `{"name":"gateway phone","delegate":true,
		"relays":[{"name":"ir-1","url":"`+url+`"}],
		"gateway":{"enabled":true,"mode":"targeted","quota_kb_per_hour":512}}`)

	desk, err := node.Open(node.Config{DataDir: t.TempDir(), Name: "desktop", Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { desk.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; desk.Close() })
	deskEv, unsub := desk.Subscribe()
	defer unsub()

	mustAdd(t, offline, desk.Identity().URI(), "desk")
	if _, err := desk.AddContact(offline.URI(), "phone"); err != nil {
		t.Fatal(err)
	}
	connectBLE(t, offline, gateway, 180)

	if _, err := offline.Send("desk", "from the blackout"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(40 * time.Second)
	for got := false; !got; {
		select {
		case e := <-deskEv:
			got = e.Type == "message" && e.Message.Text == "from the blackout"
		case <-deadline:
			t.Fatal("desktop never got the phone's message")
		}
	}
	offEv.wait(t, "delivered", 40*time.Second)

	if _, err := desk.Send("phone", "reply through the gateway"); err != nil {
		t.Fatal(err)
	}
	for {
		if e := offEv.wait(t, "message", 40*time.Second); e.Message.Text == "reply through the gateway" {
			break
		}
	}
}

func TestChunkFraming(t *testing.T) {
	var sent [][]byte
	l := &chunkLink{name: "x", chunk: 23, rx: map[string][]byte{}}
	l.t = transportFunc(func(_ string, c []byte) { sent = append(sent, append([]byte(nil), c...)) })
	l.up("p")
	rng := rand.New(rand.NewSource(1))
	var want [][]byte
	for _, n := range []int{1, 2, 20, 23, 24, 300, 4096, packet.MaxSize} {
		p := make([]byte, n)
		rng.Read(p)
		want = append(want, p)
		l.Send("p", p)
	}
	l.Send("p", nil)                               // ignored
	l.Send("p", make([]byte, packet.MaxSize+1))    // ignored
	l.Send(router.PeerID("gone"), []byte("hello")) // unknown peer: ignored
	var got [][]byte
	for _, c := range sent {
		if len(c) > 23 {
			t.Fatalf("chunk of %d bytes", len(c))
		}
		ps, err := l.feed("p", c)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ps...)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d packets, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("packet %d differs", i)
		}
	}
	// A corrupt length resets the stream and is reported.
	if _, err := l.feed("p", []byte{0, 0, 0, 1}); err == nil {
		t.Fatal("zero length accepted")
	}
	if _, err := l.feed("p", []byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("oversize length accepted")
	}
	if ps, err := l.feed("p", []byte{0, 0, 2, 7, 8}); err != nil || len(ps) != 1 || !bytes.Equal(ps[0], []byte{7, 8}) {
		t.Fatalf("stream did not recover: %v %v", ps, err)
	}
	if _, err := l.feed("stranger", []byte{1}); err == nil {
		t.Fatal("chunk from unknown peer accepted")
	}
	// Reconnecting discards a half-received packet.
	l.feed("p", []byte{0, 0, 9, 1, 2})
	l.down("p")
	l.up("p")
	if ps, _ := l.feed("p", []byte{0, 0, 1, 5}); len(ps) != 1 || ps[0][0] != 5 {
		t.Fatalf("stale bytes survived a reconnect: %v", ps)
	}
}

type transportFunc func(string, []byte)

func (f transportFunc) Send(p string, c []byte) { f(p, c) }

func TestWipe(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	other, _ := identity.Generate()
	mustAdd(t, c, identity.Contact{Public: other.Public}.URI(), "x")
	if _, err := c.Send("x", "secret"); err != nil {
		t.Fatal(err)
	}
	hash := c.Hash()
	if err := c.Wipe(); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("%d files left after wipe", len(left))
	}
	c2, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Stop()
	if c2.Hash() == hash {
		t.Fatal("identity survived the wipe")
	}
	if s, _ := c2.Chats(); s != "[]" {
		t.Fatalf("contacts survived the wipe: %s", s)
	}
}

func TestBadInput(t *testing.T) {
	if _, err := Open(t.TempDir(), "{not json"); err == nil {
		t.Fatal("bad config accepted")
	}
	c, _ := phone(t, "")
	if err := c.AddTransport("ble", 5, transportFunc(func(string, []byte) {})); err == nil {
		t.Fatal("tiny chunk accepted")
	}
	if err := c.PeerUp("nope", "p"); err == nil {
		t.Fatal("unknown transport accepted")
	}
	if _, err := c.AddContact("not a link", ""); err == nil {
		t.Fatal("bad contact link accepted")
	}
	if err := c.LoRaConfigure("lora", `{"role":"client"}`); err == nil {
		t.Fatal("configured a node that is not attached")
	}
	if s, err := c.Messages("nobody", 10); err == nil {
		t.Fatalf("messages of unknown contact: %s", s)
	}
}

func TestTextSize(t *testing.T) {
	if OnePacketBytes() < 100 || OnePacketBytes() >= MaxTextBytes() {
		t.Fatalf("one packet %d, max %d", OnePacketBytes(), MaxTextBytes())
	}
	fa, en := "سلام، حالت چطوره؟", "hello, how are you?"
	if TextBytes(fa) >= len(fa) { // compact Persian beats UTF-8
		t.Fatalf("persian %d bytes encoded, %d in UTF-8", TextBytes(fa), len(fa))
	}
	if TextBytes(en) > len(en) || TextBytes("") != 0 {
		t.Fatalf("english %d, empty %d", TextBytes(en), TextBytes(""))
	}
	// A text of exactly OnePacketBytes is one packet; one more byte is not.
	a, _ := phone(t, "")
	other, _ := identity.Generate()
	mustAdd(t, a, identity.Contact{Public: other.Public}.URI(), "x")
	if _, err := a.Send("x", strings.Repeat("a", MaxTextBytes()+1)); err == nil {
		t.Fatal("over-long text accepted")
	}
}

func TestExportRestoreIdentity(t *testing.T) {
	old := t.TempDir()
	c, err := Open(old, "")
	if err != nil {
		t.Fatal(err)
	}
	hash := c.Hash()
	if _, err := c.ExportIdentity("short"); err == nil {
		t.Fatal("short passphrase accepted")
	}
	backup, err := c.ExportIdentity("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()

	// New phone: restore before Open, then the core has the same identity.
	fresh := t.TempDir()
	if _, err := RestoreIdentity(fresh, backup, "wrong passphrase", false); err == nil || !strings.HasPrefix(err.Error(), "passphrase:") {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := RestoreIdentity(fresh, "meshmsg:not-a-backup", "correct horse", false); err == nil || !strings.HasPrefix(err.Error(), "format:") {
		t.Fatalf("not a backup: %v", err)
	}
	got, err := RestoreIdentity(fresh, backup, "correct horse", false)
	if err != nil || got != hash {
		t.Fatal(got, err)
	}
	c2, err := Open(fresh, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Stop()
	if c2.Hash() != hash {
		t.Fatal("restored core has another identity")
	}

	// A phone that already made its own identity needs replace.
	other := t.TempDir()
	c3, _ := Open(other, "")
	c3.Stop()
	if _, err := RestoreIdentity(other, backup, "correct horse", false); err == nil {
		t.Fatal("overwrote an existing identity")
	}
	if _, err := RestoreIdentity(other, backup, "correct horse", true); err != nil {
		t.Fatal(err)
	}
}

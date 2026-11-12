package node_test

import (
	"bytes"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func startMediaRelay(t *testing.T, name string, perDay ...int) (url, dir string) {
	key, _ := identity.Generate()
	dir = t.TempDir()
	srv := httptest.NewServer(api.New(api.Config{
		Relay: relay.New(relay.Config{Name: name, Region: "global"}), Identity: key, PowBits: 4, RateLimit: 1000,
		Media: api.MediaConfig{Dir: dir, MaxMB: 4, Policy: easyPolicy(append(perDay, 5)[0])},
	}))
	t.Cleanup(srv.Close)
	return srv.URL, dir
}

// easyPolicy: little proof-of-work and no new-account limit, for tests.
func easyPolicy(perDay int) *api.MediaPolicy {
	p := api.DefaultMediaPolicy()
	p.PerDay, p.NewAccountDays, p.PowBaseBits, p.AgeTiers = perDay, 0, 4, nil
	return &p
}

// The test relays run on 127.0.0.1, which clients normally refuse.
var local = node.DiscoveryConfig{AllowPrivate: true, ListURL: "none"}

func randomFile(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// waitMedia waits until n has the file of message text/caption ready.
func waitMediaReady(t *testing.T, n *node.Node, id string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if mi, ok := n.MediaInfo(id); ok && mi.State == "ready" {
			b, _, err := n.MediaFile(id)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
		if time.Now().After(deadline) {
			mi, _ := n.MediaInfo(id)
			t.Fatalf("file %s not ready: %+v", id, mi)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func blobsIn(dir string) int {
	ents, _ := os.ReadDir(dir)
	n := 0
	for _, e := range ents {
		if filepath.Ext(e.Name()) == "" {
			n++
		}
	}
	return n
}

func TestPhotoThroughRelay(t *testing.T) {
	url, dir := startMediaRelay(t, "m-1")
	relays := []node.RelayConfig{{Name: "m-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays, Discovery: local})
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays, Discovery: local})
	befriend(t, ali, sara, "ali", "sara")
	ev, cancel := sara.Subscribe()
	defer cancel()
	time.Sleep(time.Second) // relay info (media limit) known

	photo := randomFile(300_000)
	m, err := ali.SendMedia("sara", photo, "image/jpeg", "dinner.jpg", "شام", true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Media == nil || m.Media.Kind() != "image" || m.Media.Size != len(photo) || m.Media.Relays != 1 || !m.Media.Important || m.Media.Note != "" {
		t.Fatalf("sent %+v", m.Media)
	}
	got := waitText(t, ev, "شام", 30*time.Second)
	if got.Media == nil || got.Media.Name != "dinner.jpg" || got.Media.Mime != "image/jpeg" || !got.Media.Important {
		t.Fatalf("received %+v", got.Media)
	}
	// Small: fetched without asking, identical, and the relay forgot it.
	if b := waitMediaReady(t, sara, got.Media.ID, 30*time.Second); !bytes.Equal(b, photo) {
		t.Fatal("file differs")
	}
	if n := blobsIn(dir); n != 0 {
		t.Fatalf("relay still holds %d files after the only reader fetched", n)
	}
	// Public chats take no files.
	ali.JoinGroup("global")
	if _, err := ali.SendMedia("#global", photo, "image/jpeg", "x.jpg", "", true); err == nil {
		t.Fatal("file sent to a public chat")
	}
}

// Normal files never touch a relay: with only a relay between them the
// receiver learns of the file but can't get it until a direct path appears.
// An important file over the daily allowance goes as a normal one.
func TestNormalFileSkipsRelayAndAllowance(t *testing.T) {
	url, dir := startMediaRelay(t, "m-1", 1)
	relays := []node.RelayConfig{{Name: "m-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays, Discovery: local})
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays, Discovery: local})
	befriend(t, ali, sara, "ali", "sara")
	ev, cancel := sara.Subscribe()
	defer cancel()
	time.Sleep(time.Second)

	m, err := ali.SendMedia("sara", randomFile(50_000), "image/jpeg", "normal.jpg", "normal", false)
	if err != nil || m.Media.Relays != 0 || m.Media.Important {
		t.Fatal(err, m.Media)
	}
	got := waitText(t, ev, "normal", 30*time.Second)
	if got.Media == nil || got.Media.Important {
		t.Fatalf("received %+v", got.Media)
	}
	time.Sleep(2 * time.Second)
	if mi, _ := sara.MediaInfo(got.Media.ID); mi.State == "ready" {
		t.Fatal("normal file arrived without a direct path")
	}
	if blobsIn(dir) != 0 {
		t.Fatal("normal file uploaded to the relay")
	}

	// Allowance 1: the first important file goes through, the second not.
	if m, err := ali.SendMedia("sara", randomFile(1000), "image/jpeg", "a.jpg", "first", true); err != nil || !m.Media.Important {
		t.Fatal(err, m.Media)
	}
	waitText(t, ev, "first", 30*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for blobsIn(dir) == 0 && !hasReady(sara, ev) {
		if time.Now().After(deadline) {
			t.Fatal("first important file never uploaded")
		}
		time.Sleep(100 * time.Millisecond)
	}
	m, err = ali.SendMedia("sara", randomFile(1000), "image/jpeg", "b.jpg", "second", true)
	if err != nil || m.Media.Important || m.Media.Relays != 0 || m.Media.Note == "" {
		t.Fatalf("over the allowance: %v %+v", err, m.Media)
	}
}

// hasReady drains events; true once any media became ready.
func hasReady(n *node.Node, ev <-chan node.Event) bool {
	for {
		select {
		case e := <-ev:
			if e.Type == "media" {
				if mi, ok := n.MediaInfo(e.Info); ok && mi.State == "ready" {
					return true
				}
			}
		default:
			return false
		}
	}
}

func TestBigFileOnRequestAndGroupReaders(t *testing.T) {
	url, dir := startMediaRelay(t, "m-1")
	relays := []node.RelayConfig{{Name: "m-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays, Discovery: local})
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays, Discovery: local})
	reza := startNode(t, node.Config{Name: "Reza", Relays: relays, Discovery: local, Media: node.MediaConfig{AutoDownloadMB: -1}})
	befriend(t, ali, sara, "ali", "sara")
	befriend(t, ali, reza, "ali", "reza")
	g, err := ali.CreateGroup("Family", []string{"sara", "reza"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*node.Node{sara, reza} {
		waitGroup(t, n, g.Hash, func(g node.Group) bool { return len(g.Members) == 3 }, 20*time.Second)
	}
	sev, c1 := sara.Subscribe()
	defer c1()
	rev, c2 := reza.Subscribe()
	defer c2()
	time.Sleep(time.Second)

	voice := randomFile(3_000_000) // over the 2 MB auto limit
	if _, err := ali.SendMedia("Family", voice, "audio/ogg", "voice.ogg", "", true); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, ev := range []<-chan node.Event{sev, rev} {
		deadline := time.After(30 * time.Second)
	wait:
		for {
			select {
			case e := <-ev:
				if e.Type == "message" && e.Message.Media != nil {
					id = e.Message.Media.ID
					if e.Message.Media.Kind() != "audio" || !e.Message.Group {
						t.Fatalf("%+v", e.Message)
					}
					break wait
				}
			case <-deadline:
				t.Fatal("no voice message")
			}
		}
	}
	time.Sleep(2 * time.Second)
	if mi, _ := sara.MediaInfo(id); mi.State != "remote" {
		t.Fatalf("big file fetched without asking: %+v", mi)
	}
	// Both ask; the relay keeps it until the second member has it.
	sara.FetchMedia(id)
	if !bytes.Equal(waitMediaReady(t, sara, id, 60*time.Second), voice) {
		t.Fatal("sara's copy differs")
	}
	if blobsIn(dir) != 1 {
		t.Fatal("relay dropped the file before every member fetched it")
	}
	reza.FetchMedia(id)
	if !bytes.Equal(waitMediaReady(t, reza, id, 60*time.Second), voice) {
		t.Fatal("reza's copy differs")
	}
	deadline := time.Now().Add(10 * time.Second)
	for blobsIn(dir) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("relay kept the file after every member fetched it")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// No relay at all: the file goes straight from phone to phone over the LAN.
func TestVideoOverLANWithoutRelay(t *testing.T) {
	a := startNode(t, node.Config{Name: "A", LAN: node.LANConfig{Enabled: true}})
	time.Sleep(100 * time.Millisecond)
	b := startNode(t, node.Config{Name: "B", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	befriend(t, a, b, "a", "b")
	ev, cancel := b.Subscribe()
	defer cancel()
	time.Sleep(time.Second)
	video := randomFile(1_500_000)
	m, err := a.SendMedia("b", video, "video/mp4", "clip.mp4", "ویدیو", false)
	if err != nil || m.Media.Relays != 0 {
		t.Fatal(err, m.Media)
	}
	got := waitText(t, ev, "ویدیو", 20*time.Second)
	if !bytes.Equal(waitMediaReady(t, b, got.Media.ID, 30*time.Second), video) {
		t.Fatal("video differs")
	}
	// A text right after still arrives promptly.
	start := time.Now()
	if _, err := a.Send("b", "still fast"); err != nil {
		t.Fatal(err)
	}
	waitText(t, ev, "still fast", 10*time.Second)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("text took %v", time.Since(start))
	}
}

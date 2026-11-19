// Package edgetest runs the edge relay (edge/dist, JavaScript) in Node and
// talks to it with the Go clients: phones' and desktops' node.Node, relay
// federation and the trip API. Skipped without Node 22.5+ (node:sqlite).
package edgetest

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func nodeJS(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if exec.Command(p, "-e", "require('node:sqlite')").Run() != nil {
		t.Skip("node without node:sqlite (needs 22.5+)")
	}
	return p
}

// startEdge runs edge/test/server.mjs and returns its URL.
func startEdge(t *testing.T, store, entry string, cfg map[string]any) string {
	t.Helper()
	js := nodeJS(t)
	b, _ := json.Marshal(cfg)
	cmd := exec.Command(js, "--no-warnings", filepath.Join("..", "test", "server.mjs"), "--store", store, "--entry", entry, "--config", string(b))
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "listening ") {
				line <- strings.TrimPrefix(sc.Text(), "listening ")
			}
		}
	}()
	select {
	case u := <-line:
		return u
	case <-time.After(20 * time.Second):
		t.Fatal("edge relay did not start")
	}
	return ""
}

func startNode(t *testing.T, name, relayURL string) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		DataDir: t.TempDir(), Name: name, Relays: []node.RelayConfig{{Name: "edge", URL: relayURL}},
		Discovery: node.DiscoveryConfig{ListURL: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; n.Close() })
	return n
}

func befriend(t *testing.T, a, b *node.Node, aName, bName string) {
	t.Helper()
	if _, err := a.AddContact(b.Identity().URI(), bName); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddContact(a.Identity().URI(), aName); err != nil {
		t.Fatal(err)
	}
}

func waitEvent(t *testing.T, ch <-chan node.Event, ok func(node.Event) bool, timeout time.Duration) node.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			if ok(e) {
				return e
			}
		case <-deadline:
			t.Fatal("timed out waiting for an event")
		}
	}
}

func chat(t *testing.T, url string) {
	ali := startNode(t, "Ali", url)
	sara := startNode(t, "Sara", url)
	befriend(t, ali, sara, "ali", "sara")
	saraEv, c1 := sara.Subscribe()
	defer c1()
	aliEv, c2 := ali.Subscribe()
	defer c2()
	if _, err := ali.Send("sara", "سلام از لبه"); err != nil {
		t.Fatal(err)
	}
	e := waitEvent(t, saraEv, func(e node.Event) bool { return e.Type == "message" }, 40*time.Second)
	if e.Message.Text != "سلام از لبه" || e.Message.Peer != ali.Identity().Hash().String() {
		t.Fatalf("got %+v", e.Message)
	}
	waitEvent(t, aliEv, func(e node.Event) bool { return e.Type == "delivered" }, 40*time.Second)
}

func TestChatThroughEdgeD1(t *testing.T) {
	chat(t, startEdge(t, "d1", "cloudflare", map[string]any{"RELAY_NAME": "edge-d1", "POW_BITS": 4}))
}

func TestChatThroughEdgeS3(t *testing.T) {
	chat(t, startEdge(t, "s3", "cloudflare", map[string]any{"RELAY_NAME": "edge-s3", "POW_BITS": 4}))
}

func TestChatThroughArvanBundle(t *testing.T) {
	chat(t, startEdge(t, "s3", "arvan", map[string]any{"RELAY_NAME": "edge-arvan", "POW_BITS": 4}))
}

func TestRelayListSigned(t *testing.T) {
	url := startEdge(t, "d1", "cloudflare", map[string]any{"RELAY_NAME": "edge-list", "RELAY_REGION": "ir"})
	c, _ := relayclient.New(url, relayclient.Options{})
	l, err := c.Relays(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !relayclient.VerifyRelayList(*l) || len(l.Relays) != 1 || l.Relays[0].Name != "edge-list" || l.Relays[0].Region != "ir" {
		t.Fatalf("relay list %+v (verifies: %v)", l, relayclient.VerifyRelayList(*l))
	}
	in, err := c.Info(context.Background())
	if err != nil || in.PubKey != l.PubKey || in.PubKey != l.Relays[0].PubKey {
		t.Fatalf("info %+v %v", in, err)
	}
}

// The admin panel: the login code arrives in the admin's Mava as a message.
func TestAdminPanelLogin(t *testing.T) {
	// The account must exist before the relay's config names it.
	dir := t.TempDir()
	seedNode, err := node.Open(node.Config{DataDir: dir, Discovery: node.DiscoveryConfig{ListURL: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	id := seedNode.Identity().Hash().String()
	seedNode.Close()
	url := startEdge(t, "d1", "cloudflare", map[string]any{"RELAY_NAME": "edge-admin", "ADMINS": id, "POW_BITS": 4})
	ali, err := node.Open(node.Config{DataDir: dir, Name: "Ali", Relays: []node.RelayConfig{{Name: "edge", URL: url}}, Discovery: node.DiscoveryConfig{ListURL: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ali.Run(ctx); close(done) }()
	defer func() { cancel(); <-done; ali.Close() }()
	ev, c := ali.Subscribe()
	defer c()

	post := func(path, body, cookie string) (*http.Response, string) {
		req, _ := http.NewRequest("POST", url+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mava-Panel", "1")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r, string(b)
	}
	// Ask until the relay has Ali's announce.
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, body := post("/admin/api/code", `{"id":"`+id+`"}`, "")
		if r.StatusCode == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("code: %d %s", r.StatusCode, body)
		}
		time.Sleep(500 * time.Millisecond)
		if r.StatusCode == 429 {
			time.Sleep(61 * time.Second)
		}
	}
	e := waitEvent(t, ev, func(e node.Event) bool { return e.Type == "message" && strings.Contains(e.Message.Text, "Admin code") }, 40*time.Second)
	code := regexp.MustCompile(`\d{8}`).FindString(e.Message.Text)
	if r, _ := post("/admin/api/login", `{"id":"`+id+`","code":"00000000"}`, ""); r.StatusCode != 401 {
		t.Fatalf("wrong code accepted: %d", r.StatusCode)
	}
	r, body := post("/admin/api/login", `{"id":"`+id+`","code":"`+code+`"}`, "")
	if r.StatusCode != 200 {
		t.Fatalf("login: %d %s", r.StatusCode, body)
	}
	cookie := strings.Split(r.Header.Get("Set-Cookie"), ";")[0]
	req, _ := http.NewRequest("GET", url+"/admin/api/state", nil)
	req.Header.Set("Cookie", cookie)
	sr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		You    string
		Admins []string
		Info   struct{ Name string }
	}
	json.NewDecoder(sr.Body).Decode(&st)
	sr.Body.Close()
	if st.You != id || st.Info.Name != "edge-admin" {
		t.Fatalf("state %+v", st)
	}
	// Settings and admins.
	if r, body := post("/admin/api/settings", `{"pow_bits":6}`, cookie); r.StatusCode != 200 {
		t.Fatalf("settings: %d %s", r.StatusCode, body)
	}
	other, _ := identity.Generate()
	if r, body := post("/admin/api/admins", `{"add":"`+other.Hash().String()+`"}`, cookie); r.StatusCode != 200 || !strings.Contains(body, other.Hash().String()) {
		t.Fatalf("admins: %d %s", r.StatusCode, body)
	}
	c2, _ := relayclient.New(url, relayclient.Options{})
	if in, _ := c2.Info(context.Background()); in.PowBits != 6 {
		t.Fatalf("pow bits after settings: %d", in.PowBits)
	}
}

// Trips on an edge relay, end to end (core/node/trips_test.go's scenario, shortened).
func TestTripOnEdge(t *testing.T) {
	dir := t.TempDir()
	seed, err := node.Open(node.Config{DataDir: dir, Discovery: node.DiscoveryConfig{ListURL: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	aliID := seed.Identity().Hash().String()
	seed.Close()
	url := startEdge(t, "d1", "cloudflare", map[string]any{"RELAY_NAME": "edge-trip", "ADMINS": aliID, "POW_BITS": 4})
	ali, err := node.Open(node.Config{DataDir: dir, Name: "Ali", Relays: []node.RelayConfig{{Name: "edge", URL: url}}, Discovery: node.DiscoveryConfig{ListURL: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ali.Run(ctx); close(done) }()
	defer func() { cancel(); <-done; ali.Close() }()
	sara := startNode(t, "Sara", url)

	tr, err := ali.CreateTrip(ctx, "edge", "Arbaeen", 5*time.Minute, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(sara.Trips()) == 0 && time.Now().Before(deadline) {
		sara.RefreshTrips(ctx)
		time.Sleep(300 * time.Millisecond)
	}
	if len(sara.Trips()) != 1 {
		t.Fatal("trip not offered through the edge relay")
	}
	if err := sara.TripConsent(tr.Hash, true); err != nil {
		t.Fatal(err)
	}
	if sent, err := sara.TripReport(tr.Hash, 32.6, 44.02, 8, 3, 90, true, false); !sent || err != nil {
		t.Fatalf("report: %v %v", sent, err)
	}
	deadline = time.Now().Add(45 * time.Second)
	for {
		ms, _ := ali.TripMembers(tr.Hash)
		if len(ms) == 1 && ms[0].Name == "Sara" && ms[0].Lat == 32.6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("members %+v", ms)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err := ali.EndTrip(ctx, tr.Hash); err != nil {
		t.Fatal(err)
	}
	c, _ := relayclient.New(url, relayclient.Options{})
	if _, err := c.Trip(ctx); err != relayclient.ErrNoTrip {
		t.Fatalf("after end: %v", err)
	}
}

// Federation both ways between a Go relay and the edge relay: the edge pulls
// and pushes on its schedule; a Go relay can also sync with it.
func TestFederationWithGoRelay(t *testing.T) {
	key, _ := identity.Generate()
	goRelay := relay.New(relay.Config{Name: "go-1", Region: "global"})
	srv := httptest.NewServer(api.New(api.Config{Relay: goRelay, Identity: key, PowBits: 4, RateLimit: 1000, SyncTokens: []string{"go-token"}}))
	defer srv.Close()
	url := startEdge(t, "d1", "cloudflare", map[string]any{
		"RELAY_NAME": "edge-fed", "POW_BITS": 4, "SYNC_TOKENS": "edge-token",
		"PEERS": []map[string]string{{"name": "go-1", "url": srv.URL, "token": "go-token"}},
	})
	// A packet on the Go relay reaches the edge relay, and one on the edge reaches the Go relay.
	mk := func() []byte {
		p := &packet.Packet{Type: packet.TypeData, MaxHops: 8, ID: packet.NewID(), Body: []byte("sealed")}
		copy(p.Dest[:], []byte("0123456789abcdef"))
		return p.MustEncode()
	}
	onGo, onEdge := mk(), mk()
	goRelay.Ingest([][]byte{onGo}, "client:test", "global")
	ec, _ := relayclient.New(url, relayclient.Options{})
	if _, err := ec.Upload(context.Background(), [][]byte{onEdge}); err != nil {
		t.Fatal(err)
	}
	r, err := http.Post(url+"/__test/maintenance", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	var rep struct {
		Peers map[string]struct{ Pulled, Pushed int }
	}
	json.Unmarshal(b, &rep)
	if rep.Peers["go-1"].Pulled != 1 || rep.Peers["go-1"].Pushed != 1 {
		t.Fatalf("maintenance: %s", b)
	}
	var d packet.Hash
	copy(d[:], []byte("0123456789abcdef"))
	if raws, _ := goRelay.Mailbox(d, 0, 10); len(raws) != 2 {
		t.Fatalf("go relay holds %d packets", len(raws))
	}
	// A Go relay (or bridge) syncing with the edge relay through its API.
	other := relay.New(relay.Config{Name: "go-2"})
	cl, _ := relayclient.New(url, relayclient.Options{Origin: "go-2", Token: "edge-token"})
	cur := &relayclient.Cursors{}
	ab, ba, err := relayclient.SyncOnce(context.Background(), relay.LocalSide{R: other, Peer: "edge-fed"}, relayclient.RemoteSide{C: cl, Label: "edge-fed"}, cur, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ab != 0 || ba != 2 {
		t.Fatalf("sync with edge: sent %d, received %d", ab, ba)
	}
}

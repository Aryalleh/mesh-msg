package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/desktop/daemon"
)

// startMeshd runs a real meshd (no relays, no LAN discovery, no web UI) and
// points the CLI at its socket. Command output goes to the returned buffer.
func startMeshd(t *testing.T) *bytes.Buffer {
	t.Helper()
	dir, err := os.MkdirTemp("", "mesh") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg, err := daemon.LoadConfig(filepath.Join(dir, "config.toml"), dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Relays, cfg.UI, cfg.LAN.Discovery = nil, "", false
	d, err := daemon.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	dataDir = dir
	sock := daemon.SocketPath(dir)
	client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	for i := 0; !meshdRunning(); i++ {
		if i > 100 {
			t.Fatal("meshd did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var buf bytes.Buffer
	out = &buf
	t.Cleanup(func() { out = os.Stdout })
	return &buf
}

func TestConfigRelayBridgeCommands(t *testing.T) {
	buf := startMeshd(t)
	if err := run([]string{"config", "set", "name=Ali", "gateway=off", "announce=15", "lan.peers=10.0.0.2:7000, 10.0.0.3:7000", "lora.port=/dev/ttyUSB0"}); err != nil {
		t.Fatal(err)
	}
	c, err := getConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Ali" || c.Gateway.Enabled || c.AnnounceMinutes != 15 || len(c.LAN.StaticPeers) != 2 || c.LoRa.Port != "/dev/ttyUSB0" {
		t.Fatalf("not applied: %+v", c)
	}
	for _, bad := range [][]string{{"config", "set", "nope=1"}, {"config", "set", "gateway=maybe"}, {"config", "set", "gateway.mode=x"}, {"config", "set", "announce=-1"}} {
		if err := run(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}

	must := func(args ...string) {
		t.Helper()
		if err := run(args); err != nil {
			t.Fatal(args, err)
		}
	}
	must("relay", "add", "r1", "http://127.0.0.1:9", "direct")
	must("relay", "add", "r2", "http://127.0.0.1:10", "socks5://127.0.0.1:1080")
	must("relay", "add", "r1", "http://127.0.0.1:11") // replaces
	must("bridge", "add", "r1", "r2", "20")
	c, _ = getConfig()
	if len(c.Relays) != 2 || c.Relays[0].URL != "http://127.0.0.1:11" || c.Relays[1].Via != "socks5://127.0.0.1:1080" {
		t.Fatalf("relays: %+v", c.Relays)
	}
	if len(c.Bridges) != 1 || c.Bridges[0].BudgetMBPerDay != 20 {
		t.Fatalf("bridges: %+v", c.Bridges)
	}
	if err := run([]string{"relay", "add", "bad", "not a url"}); err == nil {
		t.Error("bad relay url accepted")
	}
	must("bridge", "rm", "r2", "r1")
	must("relay", "rm", "r2")
	if err := run([]string{"relay", "rm", "r2"}); err == nil {
		t.Error("removing a missing relay succeeded")
	}
	buf.Reset()
	must("config")
	for _, want := range []string{"name          Ali", "gateway       off", "relay         r1", "10.0.0.2:7000"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("config output lacks %q:\n%s", want, buf)
		}
	}
}

func TestApp(t *testing.T) {
	buf := startMeshd(t)
	answer, pass := "y", "correct horse battery"
	a := &app{
		w:          buf,
		unread:     map[string]int{},
		readLine:   func(string) (string, error) { return answer, nil },
		readSecret: func(string) (string, error) { return pass, nil },
	}
	expect := func(line string, wants ...string) {
		t.Helper()
		buf.Reset()
		a.handle(line)
		for _, w := range wants {
			if !strings.Contains(buf.String(), w) {
				t.Fatalf("%q: output lacks %q:\n%s", line, w, buf)
			}
		}
	}
	expect("/chats", "No chats yet")

	sara, _ := identity.Generate()
	uri := identity.Contact{Public: sara.Public}.URI()
	expect("/add "+uri+" Sara", "added Sara", "── Sara")
	if a.cur == nil || a.cur.Name != "Sara" {
		t.Fatal("chat not opened after /add")
	}
	expect("سلام Sara", "you: سلام Sara ✓")
	var ms []node.Message
	if err := call("GET", "/messages?with=Sara", nil, &ms); err != nil || len(ms) != 1 || ms[0].Text != "سلام Sara" {
		t.Fatalf("message not stored: %v %+v", err, ms)
	}

	reza, _ := identity.Generate()
	expect("/add "+identity.Contact{Public: reza.Public}.URI()+" Reza", "added Reza")
	expect("/back", "Chats", "1  Sara", "2  Reza", "→ سلام Sara")
	expect("1", "── Sara", "you: سلام Sara")
	expect("/rename Sarah", "renamed to Sarah")
	expect("/back", "Sarah")
	expect("sar", "── Sarah") // name prefix
	expect("/open zzz", `no contact "zzz"`)
	expect("/nope", "unknown command")
	expect("/settings", "gateway", "/set key=value")
	expect("/set name=Me", "saved and applied")
	expect("/status", "identity")

	// An incoming message for another chat counts as unread.
	a.event(node.Event{Type: "message", Message: &node.Message{Peer: reza.Hash().String(), Text: "hi", Received: time.Now()}})
	if a.unread[reza.Hash().String()] != 1 {
		t.Fatal("unread not counted")
	}
	expect("/back", "● 1 new")

	expect("/backup", "meshkey1:")
	backup := ""
	for _, f := range strings.Fields(buf.String()) {
		if strings.HasPrefix(f, "meshkey1:") {
			backup = f
		}
	}
	if id, err := identity.FromBackup(backup, pass); err != nil {
		t.Fatal(err)
	} else {
		var s node.Status
		call("GET", "/status", nil, &s)
		if id.Hash().String() != s.Hash {
			t.Fatal("backup is another account")
		}
	}
	file := filepath.Join(t.TempDir(), "b.txt")
	expect("/backup "+file, "written to")

	answer = "n"
	expect("/rm Reza", "kept")
	answer = "y"
	expect("/rm Reza", "removed Reza")

	// Restore another account into the running meshd.
	other, _ := identity.Generate()
	ob, _ := other.Backup(pass)
	answer = ob
	expect("/restore", "restored identity "+other.Hash().String())
	var s node.Status
	call("GET", "/status", nil, &s)
	if s.Hash != other.Hash().String() {
		t.Fatal("meshd still runs the old account")
	}

	expect("/quit")
	if !a.quit {
		t.Fatal("/quit did not quit")
	}
}

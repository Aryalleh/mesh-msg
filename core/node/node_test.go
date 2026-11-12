package node_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func startRelay(t *testing.T, name, region string) string {
	key, _ := identity.Generate()
	srv := httptest.NewServer(api.New(api.Config{
		Relay: relay.New(relay.Config{Name: name, Region: region}), Identity: key, PowBits: 4, RateLimit: 1000,
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func startNode(t *testing.T, cfg node.Config) *node.Node {
	cfg.DataDir = t.TempDir()
	if cfg.Discovery.ListURL == "" {
		cfg.Discovery.ListURL = "none" // tests never reach the real relays
	}
	n, err := node.Open(cfg)
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
	if _, err := a.AddContact(b.Identity().URI(), bName); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddContact(a.Identity().URI(), aName); err != nil {
		t.Fatal(err)
	}
}

func waitEvent(t *testing.T, ch <-chan node.Event, typ string, timeout time.Duration) node.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			if e.Type == typ {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", typ)
		}
	}
}

func TestChatThroughRelay(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	ali := startNode(t, node.Config{Name: "Ali", Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	sara := startNode(t, node.Config{Name: "Sara", Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	befriend(t, ali, sara, "ali", "sara")

	saraEv, cancel := sara.Subscribe()
	defer cancel()
	aliEv, cancel2 := ali.Subscribe()
	defer cancel2()

	if _, err := ali.Send("sara", "سلام سارا"); err != nil {
		t.Fatal(err)
	}
	e := waitEvent(t, saraEv, "message", 20*time.Second)
	if e.Message.Text != "سلام سارا" || e.Message.Peer != ali.Identity().Hash().String() {
		t.Fatalf("got %+v", e.Message)
	}
	waitEvent(t, aliEv, "delivered", 20*time.Second)
	msgs, _ := ali.Messages("sara", 10)
	if len(msgs) != 1 || !msgs[0].Delivered {
		t.Fatalf("ali history: %+v", msgs)
	}
}

func TestLANWithoutAnyServer(t *testing.T) {
	a := startNode(t, node.Config{LAN: node.LANConfig{Enabled: true}})
	// b dials a directly (e.g. two PCs on a private network, no internet).
	time.Sleep(100 * time.Millisecond)
	b := startNode(t, node.Config{LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	befriend(t, a, b, "a", "b")
	ev, cancel := a.Subscribe()
	defer cancel()
	if _, err := b.Send("a", "hello over LAN"); err != nil {
		t.Fatal(err)
	}
	if e := waitEvent(t, ev, "message", 20*time.Second); e.Message.Text != "hello over LAN" {
		t.Fatal("wrong text")
	}
}

func TestBridgeNode(t *testing.T) {
	irURL := startRelay(t, "ir-1", "ir")
	euURL := startRelay(t, "eu-1", "global")
	ali := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: irURL}}})
	sara := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "eu-1", URL: euURL}}})
	// A PC with Starlink reaches both relays and bridges them.
	startNode(t, node.Config{
		Relays:  []node.RelayConfig{{Name: "ir-1", URL: irURL}, {Name: "eu-1", URL: euURL}},
		Bridges: []node.BridgeConfig{{A: "ir-1", B: "eu-1", EverySecs: 1, BudgetMBPerDay: 5}},
	})
	befriend(t, ali, sara, "ali", "sara")
	ev, cancel := sara.Subscribe()
	defer cancel()
	if _, err := ali.Send("sara", "across the blackout"); err != nil {
		t.Fatal(err)
	}
	if e := waitEvent(t, ev, "message", 30*time.Second); e.Message.Text != "across the blackout" {
		t.Fatal("wrong text")
	}
}

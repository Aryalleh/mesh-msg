package node_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func startRelayListing(t *testing.T, name string, list []wire.RelayEntry) string {
	key, _ := identity.Generate()
	srv := httptest.NewServer(api.New(api.Config{
		Relay: relay.New(relay.Config{Name: name, Region: "global"}), Identity: key, PowBits: 4, RateLimit: 1000, Relays: list,
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func waitKnown(t *testing.T, n *node.Node, url string, ok func(node.KnownRelay) bool) node.KnownRelay {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for {
		for _, k := range n.KnownRelays() {
			if k.URL == url && ok(k) {
				return k
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay %s never reached the expected state: %+v", url, n.KnownRelays())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A nearby device shares the relay it uses; this node checks it, starts
// using it, and a message to someone only on that relay gets through.
func TestRelayLearnedFromNearbyDevice(t *testing.T) {
	far := startRelay(t, "far-1", "global")
	disc := node.DiscoveryConfig{AllowPrivate: true, ListURL: "none"}
	// Carrier uses "far-1" and is on our LAN.
	carrier := startNode(t, node.Config{Name: "carrier", Relays: []node.RelayConfig{{Name: "far-1", URL: far}},
		LAN: node.LANConfig{Enabled: true}, Discovery: disc})
	time.Sleep(100 * time.Millisecond)
	// We have no relay at all.
	me := startNode(t, node.Config{Name: "me", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{carrier.LANAddr()}}, Discovery: disc})
	// Sara is only on far-1 and not on our LAN.
	sara := startNode(t, node.Config{Name: "sara", Relays: []node.RelayConfig{{Name: "far-1", URL: far}}, Discovery: disc})
	befriend(t, me, sara, "me", "sara")

	k := waitKnown(t, me, far, func(k node.KnownRelay) bool { return k.Active })
	if k.Source != "mesh" || k.Name != "far-1" || k.State != "ok" || len(k.PubKey) != 128 {
		t.Fatalf("learned relay %+v", k)
	}
	ev, cancel := sara.Subscribe()
	defer cancel()
	if _, err := me.Send("sara", "found you through a relay a neighbour told me about"); err != nil {
		t.Fatal(err)
	}
	waitText(t, ev, "found you through a relay a neighbour told me about", 40*time.Second)

	// Forgetting stops it for good.
	if err := me.ForgetRelay("far-1"); err != nil {
		t.Fatal(err)
	}
	waitKnown(t, me, far, func(k node.KnownRelay) bool { return !k.Active && k.State == "forgotten" })
}

// Relays share their signed lists; a listed relay is checked and used.
func TestRelayLearnedFromRelayList(t *testing.T) {
	other := startRelay(t, "other-1", "global")
	home := startRelayListing(t, "home-1", []wire.RelayEntry{{Name: "other-1", Region: "global", URLs: []string{other}}})
	me := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "home-1", URL: home}},
		Discovery: node.DiscoveryConfig{AllowPrivate: true, ListURL: "none"}})
	k := waitKnown(t, me, other, func(k node.KnownRelay) bool { return k.Active })
	if k.Source != "relay" {
		t.Fatalf("%+v", k)
	}
	// The configured relay is listed too (from our own hint? no: not learned), and never duplicated as automatic.
	for _, k := range me.KnownRelays() {
		if k.URL == home && k.Active {
			t.Fatal("configured relay also started as automatic")
		}
	}
}

// Hints pointing at private addresses are ignored unless allowed.
func TestPrivateRelayHintIgnored(t *testing.T) {
	far := startRelay(t, "far-1", "global")
	carrier := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "far-1", URL: far}}, LAN: node.LANConfig{Enabled: true},
		Discovery: node.DiscoveryConfig{AllowPrivate: true, ListURL: "none"}})
	time.Sleep(100 * time.Millisecond)
	me := startNode(t, node.Config{LAN: node.LANConfig{Enabled: true, StaticPeers: []string{carrier.LANAddr()}}})
	time.Sleep(8 * time.Second)
	if ks := me.KnownRelays(); len(ks) != 0 {
		t.Fatalf("learned a loopback relay: %+v", ks)
	}
}

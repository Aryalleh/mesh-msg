package node_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/network"
)

// serveList is a stand-in for GitHub serving relays.json (+ .sig).
func serveList(t *testing.T, json []byte, sig []byte) string {
	mux := http.NewServeMux()
	mux.HandleFunc("/relays.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(json) })
	mux.HandleFunc("/relays.json.sig", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, hex.EncodeToString(sig)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/relays.json"
}

// openNode runs a node on a given data dir until stop is called.
func openNode(t *testing.T, cfg node.Config) (*node.Node, func()) {
	n, err := node.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
			n.Close()
		}
	}
	t.Cleanup(stop)
	return n, stop
}

// A phone that can't reach GitHub gets the signed relay list from a nearby
// phone, keeps it across a restart and hands it on while still offline.
func TestRelayListFromNearbyDevice(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	relay := startRelay(t, "near-1", "global")
	list := []byte(fmt.Sprintf(`{"version":1,"updated":"2026-11-16","serial":5,"relays":[{"name":"near-1","region":"global","urls":[%q]}]}`, relay))
	online := serveList(t, list, network.Sign(list, priv))
	blocked := httptest.NewServer(http.NotFoundHandler()) // GitHub blocked
	t.Cleanup(blocked.Close)
	disc := func(u string) node.DiscoveryConfig {
		return node.DiscoveryConfig{ListURL: u, ListKey: pub, NoBuiltin: true, AllowPrivate: true}
	}
	hasList := func(k node.KnownRelay) bool { return k.Source == "list" }

	a := startNode(t, node.Config{Name: "a", LAN: node.LANConfig{Enabled: true}, Discovery: disc(online)})
	dirB := t.TempDir()
	b, stopB := openNode(t, node.Config{Name: "b", DataDir: dirB, Discovery: disc(blocked.URL + "/relays.json"),
		LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	waitKnown(t, b, relay, hasList)
	waitKnown(t, b, relay, func(k node.KnownRelay) bool { return k.Active })
	stopB()

	// b restarts with neither GitHub nor a: the list comes from its cache,
	// and c, near b only, learns it from b.
	b, _ = openNode(t, node.Config{Name: "b", DataDir: dirB, Discovery: disc(blocked.URL + "/relays.json"),
		LAN: node.LANConfig{Enabled: true}})
	c := startNode(t, node.Config{Name: "c", Discovery: disc(blocked.URL + "/relays.json"),
		LAN: node.LANConfig{Enabled: true, StaticPeers: []string{b.LANAddr()}}})
	waitKnown(t, c, relay, hasList)
}

// A list signed by someone else is not taken from neighbours.
func TestRelayListWrongKeyIgnored(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, evil, _ := ed25519.GenerateKey(nil)
	relay := startRelay(t, "evil-1", "global")
	list := []byte(fmt.Sprintf(`{"version":1,"updated":"2026-11-16","serial":9,"relays":[{"name":"evil-1","region":"global","urls":[%q]}]}`, relay))
	src := serveList(t, list, network.Sign(list, evil))
	blocked := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(blocked.Close)
	// a trusts the key the list is signed with; b trusts the real one.
	evilPub := evil.Public().(ed25519.PublicKey)
	a := startNode(t, node.Config{Name: "a", LAN: node.LANConfig{Enabled: true},
		Discovery: node.DiscoveryConfig{ListURL: src, ListKey: evilPub, NoBuiltin: true, AllowPrivate: true}})
	b := startNode(t, node.Config{Name: "b", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}},
		Discovery: node.DiscoveryConfig{ListURL: blocked.URL + "/relays.json", ListKey: pub, NoBuiltin: true, AllowPrivate: true}})
	waitKnown(t, a, relay, func(k node.KnownRelay) bool { return k.Source == "list" })
	time.Sleep(3 * time.Second)
	for _, k := range b.KnownRelays() {
		if k.URL == relay && k.Source == "list" {
			t.Fatalf("b took a list signed with another key: %+v", k)
		}
	}
}

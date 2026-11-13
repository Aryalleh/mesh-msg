package node_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// Two apps that share only a relay: a normal (not important) file never
// goes to the relay; the receiver opens a direct link to the sender by
// signaling through the relay and fetches it over that link.
func TestNormalMediaOverAutoDirectLink(t *testing.T) {
	url := startRelay(t, "r-1", "global")
	relays := []node.RelayConfig{{Name: "r-1", URL: url}}
	dc := node.DirectConfig{NoPortMapping: true, Reflectors: []string{"127.0.0.1:9"}}
	a := startNode(t, node.Config{Name: "A", Relays: relays, Discovery: local, Direct: dc})
	b := startNode(t, node.Config{Name: "B", Relays: relays, Discovery: local, Direct: dc})
	a.UseLoopbackDirect()
	b.UseLoopbackDirect()
	befriend(t, a, b, "a", "b")
	ev, cancel := b.Subscribe()
	defer cancel()
	time.Sleep(time.Second)

	file := randomFile(300_000)
	m, err := a.SendMedia("b", file, "video/mp4", "clip.mp4", "direct please", false)
	if err != nil || m.Media.Relays != 0 {
		t.Fatal(err, m.Media)
	}
	got := waitText(t, ev, "direct please", 20*time.Second)
	b.FetchMedia(got.Media.ID)
	if !bytes.Equal(waitMediaReady(t, b, got.Media.ID, 40*time.Second), file) {
		t.Fatal("file differs")
	}
	if !b.DirectConnected(a.Identity().Hash().String()) {
		t.Fatal("no direct link")
	}
}

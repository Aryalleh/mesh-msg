package node_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

func jpeg(n int) []byte {
	b := randomFile(n)
	copy(b, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	return b
}

func waitAvatar(t *testing.T, n *node.Node, hash string, want []byte, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if p := n.AvatarPath(hash); p != "" {
			if b, _ := os.ReadFile(p); bytes.Equal(b, want) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("picture of %s not fetched", hash[:8])
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A profile picture travels only over a direct link, when asked for.
func TestAvatarOverDirectLink(t *testing.T) {
	url := startRelay(t, "r-1", "global")
	relays := []node.RelayConfig{{Name: "r-1", URL: url}}
	dc := node.DirectConfig{NoPortMapping: true, Reflectors: []string{"127.0.0.1:9"}}
	a := startNode(t, node.Config{Name: "A", Relays: relays, Discovery: local, Direct: dc})
	b := startNode(t, node.Config{Name: "B", Relays: relays, Discovery: local, Direct: dc})
	a.UseLoopbackDirect()
	b.UseLoopbackDirect()
	befriend(t, a, b, "a", "b")
	time.Sleep(time.Second)

	if err := a.SetAvatar([]byte("not an image")); err == nil {
		t.Fatal("non-image accepted")
	}
	if err := a.SetAvatar(jpeg(node.MaxAvatar + 1)); err == nil {
		t.Fatal("oversized picture accepted")
	}
	pic := jpeg(20_000)
	if err := a.SetAvatar(pic); err != nil {
		t.Fatal(err)
	}
	ah := a.Identity().Hash().String()
	if a.AvatarPath(ah) == "" {
		t.Fatal("own picture not stored")
	}
	if b.AvatarPath(ah) != "" {
		t.Fatal("picture arrived without being asked for")
	}
	b.RequestAvatar(ah) // opens a direct link, then fetches
	waitAvatar(t, b, ah, pic, 30*time.Second)

	// A new picture is fetched again on request.
	pic2 := jpeg(30_000)
	a.SetAvatar(pic2)
	b.RequestAvatar(ah)
	waitAvatar(t, b, ah, pic2, 20*time.Second)
}

// Over the LAN (not an authenticated contact link) the picture is not served.
func TestAvatarNotOverLAN(t *testing.T) {
	a := startNode(t, node.Config{Name: "A", LAN: node.LANConfig{Enabled: true}})
	if err := a.SetAvatar(jpeg(1000)); err != nil {
		t.Fatal(err)
	}
	b := startNode(t, node.Config{Name: "B", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	befriend(t, a, b, "a", "b")
	time.Sleep(2 * time.Second)
	ah := a.Identity().Hash().String()
	// No direct link configured: nothing to fetch over, and the LAN peer may not ask.
	b.RequestAvatar(ah)
	time.Sleep(3 * time.Second)
	if b.AvatarPath(ah) != "" {
		t.Fatal("picture fetched over the LAN")
	}
	served, answered := b.ProbeAvatarOver("lan", ah)
	if !answered {
		t.Fatal("no LAN neighbour answered (test not testing anything)")
	}
	if served {
		t.Fatal("LAN neighbour served the picture")
	}
}

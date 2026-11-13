package node_test

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

func TestReadReceiptThroughRelay(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	ali := startNode(t, node.Config{Name: "Ali", Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	sara := startNode(t, node.Config{Name: "Sara", Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	befriend(t, ali, sara, "ali", "sara")
	saraEv, c1 := sara.Subscribe()
	defer c1()
	aliEv, c2 := ali.Subscribe()
	defer c2()

	ali.Send("sara", "one")
	waitEvent(t, saraEv, "message", 20*time.Second)
	ali.Send("sara", "two")
	waitEvent(t, saraEv, "message", 20*time.Second)
	if ms, _ := ali.Messages("sara", 10); ms[0].Read || ms[1].Read {
		t.Fatal("read before Sara opened the chat")
	}
	if err := sara.MarkRead("ali"); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, aliEv, "read", 20*time.Second)
	ms, _ := ali.Messages("sara", 10)
	if len(ms) != 2 || !ms[0].Read || !ms[1].Read || !ms[1].Delivered {
		t.Fatalf("ali: %+v", ms)
	}
	// A later message is not read until Sara opens the chat again.
	ali.Send("sara", "three")
	waitEvent(t, saraEv, "message", 20*time.Second)
	if ms, _ := ali.Messages("sara", 10); ms[2].Read {
		t.Fatal("three read too early")
	}
}

func TestReadReceiptsOff(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	ali := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	sara := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	befriend(t, ali, sara, "ali", "sara")
	saraEv, c1 := sara.Subscribe()
	defer c1()
	if err := sara.SetReadReceipts(false); err != nil || sara.ReadReceipts() {
		t.Fatal("not off")
	}
	ali.Send("sara", "hi")
	waitEvent(t, saraEv, "message", 20*time.Second)
	sara.MarkRead("ali")
	time.Sleep(2 * time.Second)
	if ms, _ := ali.Messages("sara", 10); ms[0].Read {
		t.Fatal("read receipt sent while off")
	}
}

// Over LAN only (the same goes for Bluetooth and LoRa) no read receipt is
// sent; it waits for a relay.
func TestReadReceiptNotOverLAN(t *testing.T) {
	a := startNode(t, node.Config{LAN: node.LANConfig{Enabled: true}})
	time.Sleep(100 * time.Millisecond)
	b := startNode(t, node.Config{LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	befriend(t, a, b, "a", "b")
	ev, cancel := a.Subscribe()
	defer cancel()
	b.Send("a", "hello over LAN")
	waitEvent(t, ev, "message", 20*time.Second)
	a.MarkRead("b")
	time.Sleep(2 * time.Second)
	if ms, _ := b.Messages("a", 10); len(ms) != 1 || !ms[0].Delivered || ms[0].Read {
		t.Fatalf("b: %+v", ms)
	}
}

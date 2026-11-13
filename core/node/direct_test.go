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

// A voice call between two apps that share only a relay: it rings through
// the relay, audio goes over a direct path.
func TestCallThroughRelaySignaling(t *testing.T) {
	url := startRelay(t, "r-1", "global")
	relays := []node.RelayConfig{{Name: "r-1", URL: url}}
	dc := node.DirectConfig{NoPortMapping: true, Reflectors: []string{"127.0.0.1:9"}}
	a := startNode(t, node.Config{Name: "A", Relays: relays, Discovery: local, Direct: dc})
	b := startNode(t, node.Config{Name: "B", Relays: relays, Discovery: local, Direct: dc})
	a.UseLoopbackDirect()
	b.UseLoopbackDirect()
	befriend(t, a, b, "a", "b")
	evA, cA := a.Subscribe()
	defer cA()
	evB, cB := b.Subscribe()
	defer cB()
	time.Sleep(time.Second)

	waitCall := func(ev <-chan node.Event, state string) node.Event {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case e := <-ev:
				if e.Type == "call" && e.Call.State == state {
					return e
				}
			case <-deadline:
				t.Fatalf("no call %q", state)
			}
		}
	}
	info, err := a.StartCall("b", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	ring := waitCall(evB, "ringing")
	if err := b.AnswerCall(ring.Call.ID, 2); err != nil {
		t.Fatal(err)
	}
	ea, eb := waitCall(evA, "active"), waitCall(evB, "active")
	if ea.Call.Code != eb.Call.Code || ea.Call.Codec != 2 {
		t.Fatalf("%+v %+v", ea.Call, eb.Call)
	}
	for i := 0; i < 10; i++ {
		if err := a.CallAudio(info.ID, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	played := 0
	for i := 0; i < 20; i++ {
		if _, r := b.PullCallAudio(ring.Call.ID); r == 0 {
			played++
		}
	}
	if played == 0 {
		t.Fatal("no audio")
	}
	b.EndCall(ring.Call.ID)
	waitCall(evA, "ended")
}

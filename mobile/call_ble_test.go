package meshcore

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// Two phones with no relay at all, joined only by Bluetooth for messages and
// by the same local network for audio: the call must ring and connect.
func TestCallWithoutRelay(t *testing.T) { callOverBLE(t, "") }

// The same with a relay configured that can't be reached (a network without
// internet): reflector and relay time out, the local path still works.
func TestCallRelayUnreachable(t *testing.T) {
	callOverBLE(t, `{"relays":[{"name":"x","url":"http://192.0.2.1"}]}`)
}

func callOverBLE(t *testing.T, cfg string) {
	a, aev := phone(t, cfg)
	b, bev := phone(t, cfg)
	mustAdd(t, a, b.URI(), "b")
	mustAdd(t, b, a.URI(), "a")
	connectBLE(t, a, b, 180)

	waitCall := func(ev *events, state string) node.Event {
		t.Helper()
		deadline := time.Now().Add(40 * time.Second)
		for time.Now().Before(deadline) {
			e := ev.wait(t, "call", 40*time.Second)
			if e.Call.State == state {
				return e
			}
			if e.Call.State == "ended" {
				t.Fatalf("call ended (%s) before %q", e.Call.Reason, state)
			}
		}
		t.Fatalf("no call %q", state)
		return node.Event{}
	}
	start := time.Now()
	if _, err := a.StartCall("b", "1,2"); err != nil {
		t.Fatal(err)
	}
	ring := waitCall(bev, "ringing")
	t.Logf("ringing after %v", time.Since(start))
	if err := b.AnswerCall(ring.Call.ID, "1,2"); err != nil {
		t.Fatal(err)
	}
	waitCall(aev, "active")
	t.Logf("active after %v", time.Since(start))
}

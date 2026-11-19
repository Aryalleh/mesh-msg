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
	"github.com/Aryalleh/mesh-msg/server/trips"
)

// startTripRelay is a relay that runs trips; admins decides who may.
func startTripRelay(t *testing.T, admins func(string) bool) string {
	key, _ := identity.Generate()
	h := api.New(api.Config{
		Relay: relay.New(relay.Config{Name: "trip-1", Region: "ir"}), Identity: key, PowBits: 4, RateLimit: 1000,
	})
	trips.New(trips.Config{DataDir: t.TempDir(), IsAdmin: admins}).Routes(h.Handle)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func waitTrip(t *testing.T, n *node.Node, ok func(node.Trip) bool, timeout time.Duration) node.Trip {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, tr := range n.Trips() {
			if ok(tr) {
				return tr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("trip never reached the expected state: %+v", n.Trips())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitMember(t *testing.T, n *node.Node, trip string, ok func(node.TripMember) bool, timeout time.Duration) node.TripMember {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ms, _ := n.TripMembers(trip)
		for _, m := range ms {
			if ok(m) {
				return m
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("member never reached the expected state: %+v", ms)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestTripLocationSharing(t *testing.T) {
	admins := map[string]bool{}
	url := startTripRelay(t, func(h string) bool { return admins[h] })
	relays := []node.RelayConfig{{Name: "trip-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays})   // leader, relay admin
	reza := startNode(t, node.Config{Name: "Reza", Relays: relays}) // second leader, relay admin
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays}) // member
	admins[ali.Identity().Hash().String()] = true
	befriend(t, ali, reza, "ali", "reza")
	ctx := context.Background()

	// Only an admin of the relay can start a trip there.
	if _, err := sara.CreateTrip(ctx, "trip-1", "x", 5*time.Minute, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("a non-admin started a trip")
	}
	tr, err := ali.CreateTrip(ctx, "trip-1", "Arbaeen 1405", 5*time.Minute, time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Leader || tr.MapKey == "" || tr.Consent != "" || tr.Sharing {
		t.Fatalf("new trip %+v", tr)
	}

	// Sara's phone finds the trip on its relay and asks her; nothing is
	// sent before she agrees.
	deadline := time.Now().Add(20 * time.Second)
	for len(sara.Trips()) == 0 && time.Now().Before(deadline) {
		sara.RefreshTrips(ctx) // the trips screen does this when it opens
		time.Sleep(200 * time.Millisecond)
	}
	st := waitTrip(t, sara, func(x node.Trip) bool { return x.Hash == tr.Hash }, time.Second)
	if st.Leader || st.Sharing || st.Consent != "" {
		t.Fatalf("offered trip %+v", st)
	}
	if sent, _ := sara.TripReport(tr.Hash, 32.6, 44.0, 10, 5, 80, true, true); sent {
		t.Fatal("reported without consent")
	}
	if err := sara.TripConsent(tr.Hash, true); err != nil {
		t.Fatal(err)
	}
	if sent, err := sara.TripReport(tr.Hash, 32.6160567, 44.0248793, 10, 5, 80, true, false); err != nil || !sent {
		t.Fatalf("first report: %v %v", sent, err)
	}
	if sent, _ := sara.TripReport(tr.Hash, 32.6, 44.0, 10, 5, 80, true, false); sent {
		t.Fatal("reported again before the interval")
	}
	m := waitMember(t, ali, tr.Hash, func(m node.TripMember) bool { return m.Name == "Sara" }, 30*time.Second)
	if m.Hash != sara.Identity().Hash().String() || m.Lat != 32.6160567 || m.Battery != 80 || m.SOS || len(m.Track) != 1 {
		t.Fatalf("member %+v", m)
	}

	// The leader asks: Sara's phone reports every minute for a while.
	if err := ali.LocateTripMember(tr.Hash, m.Hash); err != nil {
		t.Fatal(err)
	}
	st = waitTrip(t, sara, func(x node.Trip) bool { return x.Hash == tr.Hash && x.FastUntil > 0 }, 30*time.Second)
	if st.Interval != 60 || st.NextIn != 0 {
		t.Fatalf("after locate %+v", st)
	}

	// I'm lost: the leaders are alerted.
	aliEv, cancel := ali.Subscribe()
	defer cancel()
	if err := sara.TripSOS(tr.Hash, true); err != nil {
		t.Fatal(err)
	}
	if sent, _ := sara.TripReport(tr.Hash, 32.62, 44.03, 30, 0, 79, true, false); !sent {
		t.Fatal("SOS report not sent")
	}
	waitEvent(t, aliEv, "trip_sos", 30*time.Second)

	// A second leader (an admin too) gets the key and sees members.
	if _, err := ali.AddTripLeader(ctx, tr.Hash, "reza"); err == nil {
		t.Fatal("added a leader who is not a relay admin")
	}
	admins[reza.Identity().Hash().String()] = true
	if _, err := ali.AddTripLeader(ctx, tr.Hash, "reza"); err != nil {
		t.Fatal(err)
	}
	waitTrip(t, reza, func(x node.Trip) bool { return x.Hash == tr.Hash && x.Leader }, 30*time.Second)
	if sent, _ := sara.TripReport(tr.Hash, 32.63, 44.04, 20, 0, 78, true, true); !sent {
		t.Fatal("report after the new leader not sent")
	}
	waitMember(t, reza, tr.Hash, func(m node.TripMember) bool { return m.Lat == 32.63 }, 30*time.Second)

	// Sara stops: the leaders see it, and nothing more is sent.
	if err := sara.TripStop(tr.Hash); err != nil {
		t.Fatal(err)
	}
	waitMember(t, ali, tr.Hash, func(m node.TripMember) bool { return m.Stopped && !m.SOS && m.Lat == 32.63 }, 30*time.Second)
	if sent, _ := sara.TripReport(tr.Hash, 1, 1, 1, 1, 1, true, true); sent {
		t.Fatal("reported after stopping")
	}

	// The leader ends the trip: it leaves the relay.
	if err := ali.EndTrip(ctx, tr.Hash); err != nil {
		t.Fatal(err)
	}
	if len(ali.Trips()) != 0 {
		t.Fatal("ended trip still listed")
	}
}

func TestJoinTripLink(t *testing.T) {
	admins := map[string]bool{}
	url := startTripRelay(t, func(h string) bool { return admins[h] })
	ali := startNode(t, node.Config{Name: "Ali", Relays: []node.RelayConfig{{Name: "trip-1", URL: url}}})
	admins[ali.Identity().Hash().String()] = true
	tr, err := ali.CreateTrip(context.Background(), "trip-1", "Hike", 10*time.Minute, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Someone without that relay joins with the link (a QR code).
	sara := startNode(t, node.Config{Name: "Sara"})
	got, err := sara.JoinTrip(tr.Link)
	if err != nil || got.Hash != tr.Hash || got.Every != 600 || got.Consent != "" {
		t.Fatalf("join: %v %+v", err, got)
	}
	if _, err := sara.JoinTrip("meshtrip:AAAA"); err == nil {
		t.Fatal("bad link accepted")
	}
}

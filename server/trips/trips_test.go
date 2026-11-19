package trips_test

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/trip"
	"github.com/Aryalleh/mesh-msg/server/trips"
)

func TestTripCard(t *testing.T) {
	ali, _ := identity.Generate()  // admin, leader
	sara, _ := identity.Generate() // admin
	reza, _ := identity.Generate() // not an admin
	admins := map[string]bool{ali.Hash().String(): true, sara.Hash().String(): true}
	dir := t.TempDir()
	svc := trips.New(trips.Config{DataDir: dir, IsAdmin: func(h string) bool { return admins[h] }})
	mux := http.NewServeMux()
	svc.Routes(mux.Handle)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := relayclient.New(srv.URL, relayclient.Options{})
	ctx := context.Background()

	if _, err := c.Trip(ctx); !errors.Is(err, relayclient.ErrNoTrip) {
		t.Fatalf("empty relay: %v", err)
	}
	tid, _ := identity.Generate()
	now := time.Now()
	card := trip.Card{
		V: 1, Name: "Arbaeen", Key: hex.EncodeToString(tid.Bytes()),
		Leaders: []trip.Leader{{ID: ali.Hash().String(), Name: "Ali"}},
		Every:   300, Made: now.Unix(), Ends: now.Add(48 * time.Hour).Unix(),
	}
	s, err := trip.Sign(tid, card)
	if err != nil {
		t.Fatal(err)
	}
	status := func(err error) int {
		var se *relayclient.StatusError
		if errors.As(err, &se) {
			return se.Code
		}
		return 0
	}
	if code := status(c.PutTrip(ctx, reza, s)); code != 403 {
		t.Fatalf("non-admin upload: %d", code)
	}
	if code := status(c.PutTrip(ctx, sara, s)); code != 403 {
		t.Fatalf("admin who is not a leader: %d", code)
	}
	if err := c.PutTrip(ctx, ali, s); err != nil {
		t.Fatal(err)
	}
	got, err := c.Trip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gc, err := got.Open(); err != nil || gc.Name != "Arbaeen" {
		t.Fatalf("served card: %v %+v", err, gc)
	}
	// A leader who isn't an admin is refused; the same card again too.
	card2 := card
	card2.Leaders = append(card2.Leaders, trip.Leader{ID: reza.Hash().String(), Name: "Reza"})
	card2.Made++
	s2, _ := trip.Sign(tid, card2)
	if code := status(c.PutTrip(ctx, ali, s2)); code != 403 {
		t.Fatalf("non-admin leader: %d", code)
	}
	if code := status(c.PutTrip(ctx, ali, s)); code != 409 {
		t.Fatalf("same card again: %d", code)
	}
	// Survives a restart.
	if _, _, ok := trips.New(trips.Config{DataDir: dir, IsAdmin: func(string) bool { return false }}).Current(); !ok {
		t.Fatal("trip not saved")
	}
	// End it.
	if code := status(c.DeleteTrip(ctx, reza, tid.Hash())); code != 403 {
		t.Fatalf("non-admin delete: %d", code)
	}
	if err := c.DeleteTrip(ctx, sara, tid.Hash()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Trip(ctx); !errors.Is(err, relayclient.ErrNoTrip) {
		t.Fatalf("after delete: %v", err)
	}
	if st := svc.PanelState(); st.Card != nil {
		t.Fatal("panel still shows the trip")
	}
}

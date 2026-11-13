package api_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/update"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func TestReleaseEndpoint(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cache := &update.Cache{Key: pub}
	key, _ := identity.Generate()
	h := api.New(api.Config{Relay: relay.New(relay.Config{Name: "t"}), Identity: key, RateLimit: 1000, Release: cache})
	srv := httptest.NewServer(h)
	defer srv.Close()

	if resp, _ := http.Get(srv.URL + "/v1/release?channel=beta"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("empty cache: %s", resp.Status)
	}
	if resp, _ := http.Get(srv.URL + "/v1/release?channel=nightly"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad channel: %s", resp.Status)
	}
	m := &update.Manifest{Version: "0.1.1-beta1", Channel: update.ChannelBeta, Date: time.Now().UTC().Format(time.RFC3339),
		Notes: map[string]string{"en": "x", "fa": "y"}}
	raw, err := update.Sign(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(raw)

	c := &update.Checker{Current: "0.1.0-alpha13", Relays: func() []string { return []string{srv.URL} }, GitHub: "-", Key: pub}
	st := c.Check(context.Background())
	if !st.Available || st.Latest.Version != "0.1.1-beta1" {
		t.Fatalf("%+v", st)
	}
	// Stable installs are not offered the beta.
	c.Current = "0.1.0"
	if st = c.Check(context.Background()); st.Latest != nil {
		t.Fatalf("stable install offered beta: %+v", st)
	}
}

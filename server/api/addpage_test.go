package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func TestAddPageAndAssetLinks(t *testing.T) {
	key, _ := identity.Generate()
	srv := httptest.NewServer(api.New(api.Config{Relay: relay.New(relay.Config{Name: "t"}), Identity: key, RateLimit: 1000}))
	defer srv.Close()

	r, err := http.Get(srv.URL + "/add")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(b), "Open in Mava") || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
		t.Fatal(r.Status, r.Header)
	}

	r, err = http.Get(srv.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatal(err)
	}
	var al []struct {
		Target struct {
			Package string   `json:"package_name"`
			Certs   []string `json:"sha256_cert_fingerprints"`
		}
	}
	json.NewDecoder(r.Body).Decode(&al)
	r.Body.Close()
	if len(al) != 1 || al[0].Target.Package != "org.meshmsg.app" || len(al[0].Target.Certs) == 0 {
		t.Fatalf("%+v", al)
	}
}

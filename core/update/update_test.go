package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	order := []string{"0.0.9", "0.1.0-alpha2", "0.1.0-alpha10", "0.1.0-alpha13", "0.1.0-beta1", "0.1.0-rc1", "0.1.0",
		"0.1.1-beta1", "0.1.1-beta2", "0.1.1-beta10", "0.1.1", "0.2.0-beta1", "0.2.0", "1.0.0", "1.10.0"}
	for i := range order {
		for j := range order {
			want := cmpInt(i, j)
			if got := Compare(order[i], order[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	if Compare("v0.1.1", "0.1.1+abc") != 0 || Compare("junk", "0.0.1") != -1 || Compare("0.0.0-dev+abc", "0.1.0-alpha1") != -1 {
		t.Error("prefix / build metadata / junk")
	}
}

func TestChannels(t *testing.T) {
	if ChannelOf("0.1.1-beta1") != ChannelBeta || ChannelOf("0.1.0-alpha13") != ChannelBeta || ChannelOf("1.0.0") != ChannelStable {
		t.Error("ChannelOf")
	}
	if !Accepts(ChannelBeta, ChannelStable) || !Accepts(ChannelBeta, ChannelBeta) || Accepts(ChannelStable, ChannelBeta) || !Accepts(ChannelStable, ChannelStable) {
		t.Error("Accepts")
	}
}

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func manifest(v string) *Manifest {
	return &Manifest{Version: v, Channel: ChannelOf(v), Date: "2026-11-13T10:00:00Z",
		Notes: map[string]string{"en": "notes", "fa": "یادداشت"},
		Files: []File{{Name: "meshmsg-" + v + "-arm64-v8a.apk", URL: "http://x/a.apk", SHA256: strings.Repeat("ab", 32), Size: 3}}}
}

func TestSignVerify(t *testing.T) {
	pub, priv := testKey(t)
	raw, err := Sign(manifest("0.1.1-beta1"), priv)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Verify(raw, pub)
	if err != nil || m.Version != "0.1.1-beta1" || m.Notes["fa"] != "یادداشت" || m.File("arm64-v8a.apk") == nil {
		t.Fatalf("verify: %v %+v", err, m)
	}
	other, _ := testKey(t)
	if _, err := Verify(raw, other); err == nil {
		t.Error("verified with the wrong key")
	}
	// Tamper with the manifest: change the version inside the signed bytes.
	var e Envelope
	json.Unmarshal(raw, &e)
	body, _ := base64.StdEncoding.DecodeString(e.Manifest)
	e.Manifest = base64.StdEncoding.EncodeToString([]byte(strings.Replace(string(body), "0.1.1-beta1", "9.9.9-beta1", 1)))
	bad, _ := json.Marshal(e)
	if _, err := Verify(bad, pub); err == nil {
		t.Error("tampered manifest verified")
	}
	// Signature without the context string must not verify.
	e2 := Envelope{Manifest: base64.StdEncoding.EncodeToString(body), Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))}
	bad, _ = json.Marshal(e2)
	if _, err := Verify(bad, pub); err == nil {
		t.Error("signature without context verified")
	}
	m2 := manifest("1.0.0-beta1")
	m2.Channel = ChannelStable
	if _, err := Sign(m2, priv); err == nil {
		t.Error("stable pre-release signed")
	}
}

func TestReleaseKey(t *testing.T) {
	if len(ReleaseKey()) != ed25519.PublicKeySize {
		t.Fatal("ReleaseKeyHex")
	}
}

func TestEvaluate(t *testing.T) {
	m := manifest("0.1.1-beta2")
	if st := Evaluate("0.1.1-beta1", m); !st.Available || st.Required {
		t.Errorf("%+v", st)
	}
	if st := Evaluate("0.1.1-beta2", m); st.Available {
		t.Errorf("same version offered: %+v", st)
	}
	m.MinSupported = "0.1.1-beta2"
	if st := Evaluate("0.1.1-beta1", m); !st.Required {
		t.Errorf("not required: %+v", st)
	}
}

// fakeGitHub serves a releases list with the given manifests (newest first).
func fakeGitHub(t *testing.T, raws [][]byte, pre []bool) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		for i := range raws {
			out = append(out, map[string]any{"prerelease": pre[i], "assets": []map[string]string{
				{"name": "other.apk", "browser_download_url": srv.URL + "/nope"},
				{"name": ManifestName, "browser_download_url": fmt.Sprintf("%s/m/%d", srv.URL, i)},
			}})
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/m/{i}", func(w http.ResponseWriter, r *http.Request) {
		var i int
		fmt.Sscan(r.PathValue("i"), &i)
		w.Write(raws[i])
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckerGitHubAndRelay(t *testing.T) {
	pub, priv := testKey(t)
	beta, _ := Sign(manifest("0.2.0-beta1"), priv)
	stable, _ := Sign(manifest("0.1.1"), priv)
	gh := fakeGitHub(t, [][]byte{beta, stable}, []bool{true, false})

	c := &Checker{Current: "0.1.1-beta1", GitHub: gh.URL + "/releases", Key: pub}
	st := c.Check(context.Background())
	if st.Latest == nil || st.Latest.Version != "0.2.0-beta1" || !st.Available || st.Source != "github" {
		t.Fatalf("beta install: %+v", st)
	}
	c.Current = "0.1.0"
	st = c.Check(context.Background())
	if st.Latest == nil || st.Latest.Version != "0.1.1" || !st.Available {
		t.Fatalf("stable install: %+v", st)
	}

	// A relay with a cache filled from the same GitHub answers first.
	cache := &Cache{Source: gh.URL + "/releases", Key: pub}
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := cache.Get(r.URL.Query().Get("channel"))
		if raw == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(raw)
	}))
	defer relay.Close()
	dead := "http://127.0.0.1:1"
	c = &Checker{Current: "0.1.1-beta1", Relays: func() []string { return []string{dead, relay.URL} }, GitHub: "-", Key: pub}
	st = c.Check(context.Background())
	if st.Latest == nil || st.Latest.Version != "0.2.0-beta1" || st.Source != relay.URL {
		t.Fatalf("via relay: %+v", st)
	}
	c.Current = "0.1.0"
	if st = c.Check(context.Background()); st.Latest == nil || st.Latest.Version != "0.1.1" {
		t.Fatalf("stable via relay: %+v", st)
	}

	// A relay serving a manifest signed by someone else is ignored.
	evilPub, evilPriv := testKey(t)
	_ = evilPub
	evil, _ := Sign(manifest("9.0.0"), evilPriv)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(evil) }))
	defer bad.Close()
	c = &Checker{Current: "0.1.0", Relays: func() []string { return []string{bad.URL} }, GitHub: "-", Key: pub}
	if st = c.Check(context.Background()); st.Latest != nil || st.Error == "" {
		t.Fatalf("forged manifest accepted: %+v", st)
	}
}

func TestCachePut(t *testing.T) {
	pub, priv := testKey(t)
	c := &Cache{Key: pub}
	b1, _ := Sign(manifest("0.1.1-beta1"), priv)
	b2, _ := Sign(manifest("0.1.1-beta2"), priv)
	s1, _ := Sign(manifest("0.1.1"), priv)
	if !c.Put(b2) || c.Put(b1) {
		t.Fatal("older manifest replaced newer one")
	}
	if raw, _ := c.Get(ChannelStable); raw != nil {
		t.Fatal("beta on stable channel")
	}
	c.Put(s1)
	if _, m := c.Get(ChannelStable); m == nil || m.Version != "0.1.1" {
		t.Fatal("stable")
	}
	if _, m := c.Get(ChannelBeta); m == nil || m.Version != "0.1.1" {
		t.Fatal("beta channel should see the newer stable")
	}
	_, evil := testKey(t)
	e, _ := Sign(manifest("9.9.9"), evil)
	if c.Put(e) {
		t.Fatal("forged manifest cached")
	}
}

func TestDownload(t *testing.T) {
	data := []byte("apk bytes")
	sum := sha256.Sum256(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer srv.Close()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.apk")
	f := &File{Name: "a.apk", URL: srv.URL, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	cl := &http.Client{Timeout: 5 * time.Second}
	if err := Download(context.Background(), cl, f, p); err != nil {
		t.Fatal(err)
	}
	f.SHA256 = strings.Repeat("00", 32)
	if err := Download(context.Background(), cl, f, p); err == nil {
		t.Fatal("bad hash accepted")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file with bad hash kept")
	}
}

// A relay whose copy is stale (beta2) doesn't hide a newer release on GitHub
// (beta3); with GitHub unreachable the relay's answer still counts.
func TestCheckerStaleRelay(t *testing.T) {
	pub, priv := testKey(t)
	b2, _ := Sign(manifest("0.1.1-beta2"), priv)
	b3, _ := Sign(manifest("0.1.1-beta3"), priv)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b2) }))
	defer relay.Close()
	gh := fakeGitHub(t, [][]byte{b3, b2}, []bool{true, true})
	c := &Checker{Current: "0.1.1-beta2", Relays: func() []string { return []string{relay.URL} }, GitHub: gh.URL + "/releases", Key: pub}
	st := c.Check(context.Background())
	if st.Latest == nil || st.Latest.Version != "0.1.1-beta3" || !st.Available || st.Source != "github" {
		t.Fatalf("stale relay won: %+v", st)
	}
	c.GitHub = "http://127.0.0.1:1/releases" // blocked
	st = c.Check(context.Background())
	if st.Latest == nil || st.Latest.Version != "0.1.1-beta2" || st.Available || st.Error != "" {
		t.Fatalf("relay only: %+v", st)
	}
}

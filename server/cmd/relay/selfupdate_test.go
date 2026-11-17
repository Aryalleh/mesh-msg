package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/update"
)

// A fake GitHub with one signed release: the relay replaces its binary with
// the one in the tarball, and refuses a tarball whose hash isn't signed.
func TestSelfUpdate(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	const newVer = "0.1.1-beta99"
	script := "#!/bin/sh\necho " + newVer + "\n"
	tgz := tarball(t, "meshmsg-relay-"+newVer+"-linux-"+runtime.GOARCH+"/meshmsg-relay", script)
	name := fmt.Sprintf("meshmsg-relay-%s-linux-%s.tar.gz", newVer, runtime.GOARCH)
	sum := sha256.Sum256(tgz)
	var srv *httptest.Server
	tamper := false
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			json.NewEncoder(w).Encode([]map[string]any{{
				"tag_name": "v" + newVer, "prerelease": true,
				"assets": []map[string]string{{"name": update.ManifestName, "browser_download_url": srv.URL + "/release.json"}},
			}})
		case "/release.json":
			m := &update.Manifest{Version: newVer, Channel: update.ChannelBeta, Date: time.Now().UTC().Format(time.RFC3339),
				Notes: map[string]string{"en": "x"},
				Files: []update.File{{Name: name, URL: srv.URL + "/" + name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(tgz))}}}
			b, err := update.Sign(m, priv)
			if err != nil {
				t.Error(err)
			}
			w.Write(b)
		case "/" + name:
			b := tgz
			if tamper {
				b = append(bytes.Clone(tgz[:len(tgz)-1]), tgz[len(tgz)-1]^1)
			}
			w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "meshmsg-relay")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 0.1.1-beta1\n"), 0o755)
	ctx := context.Background()

	tamper = true
	if _, err := selfUpdateFrom(ctx, srv.URL+"/releases", pub, "0.1.1-beta1", exe, "", false); err == nil {
		t.Fatal("a tarball that does not match the signed hash was installed")
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "beta1") {
		t.Fatal("binary changed after a failed update")
	}
	tamper = false
	msg, err := selfUpdateFrom(ctx, srv.URL+"/releases", pub, "0.1.1-beta1", exe, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != script {
		t.Fatalf("binary not replaced (%s)", msg)
	}
	msg, err = selfUpdateFrom(ctx, srv.URL+"/releases", pub, newVer, exe, "", false)
	if err != nil || !strings.HasPrefix(msg, "up to date") {
		t.Fatalf("second run: %q %v", msg, err)
	}
	// Signed by someone else: refused.
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := selfUpdateFrom(ctx, srv.URL+"/releases", other, "0.1.1-beta1", exe, "", false); err == nil {
		t.Fatal("a manifest signed with another key was accepted")
	}
}

func tarball(t *testing.T, path, body string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

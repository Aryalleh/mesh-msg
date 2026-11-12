package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
)

func startTestDaemon(t *testing.T) (*Daemon, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg, err := LoadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Relays, cfg.UI, cfg.LAN.Discovery = nil, "", false
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	d.mu.Lock()
	d.startNode()
	d.mu.Unlock()
	t.Cleanup(func() {
		cancel()
		d.mu.Lock()
		d.stopNode()
		d.mu.Unlock()
	})
	return d, d.apiHandler(), path
}

func do(t *testing.T, h http.Handler, method, path string, body any, out any) int {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	if out != nil && rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body)
		}
	}
	return rec.Code
}

func TestSettingsApplyAndSave(t *testing.T) {
	d, h, path := startTestDaemon(t)
	var cfg map[string]any
	if code := do(t, h, "GET", "/config", nil, &cfg); code != 200 {
		t.Fatal(code)
	}
	cfg["name"] = "Sara"
	cfg["relays"] = []map[string]string{{"name": "eu-1", "url": "http://127.0.0.1:1", "via": "direct"}}
	cfg["gateway"] = map[string]any{"enabled": false, "mode": "carrier", "quota_kb_per_hour": 100}
	cfg["ui"] = "0.0.0.0:80" // ignored
	if code := do(t, h, "PUT", "/config", cfg, nil); code != 200 {
		t.Fatal(code)
	}
	if d.Node().Identity().Name != "Sara" {
		t.Fatal("node not restarted with the new name")
	}
	var st map[string]any
	if do(t, h, "GET", "/status", nil, &st) != 200 || st["gateway"] != false {
		t.Fatalf("status after apply: %v", st)
	}
	saved, err := LoadConfig(path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Sara" || len(saved.Relays) != 1 || saved.Gateway.Mode != "carrier" || saved.Gateway.Enabled || saved.UI != "" || !saved.LAN.Enabled {
		t.Fatalf("saved: %+v", saved)
	}

	// Invalid settings are refused and the node keeps running.
	cfg["relays"] = []map[string]string{{"name": "bad name", "url": "ftp://x"}}
	if code := do(t, h, "PUT", "/config", cfg, nil); code != 400 {
		t.Fatalf("bad relay accepted: %d", code)
	}
	if do(t, h, "GET", "/contacts", nil, nil) != 200 {
		t.Fatal("node not usable after a refused change")
	}
}

func TestIdentityExportImport(t *testing.T) {
	d, h, _ := startTestDaemon(t)
	var exp map[string]string
	if code := do(t, h, "POST", "/identity/export", map[string]string{"passphrase": "short"}, &exp); code != 400 {
		t.Fatalf("short passphrase: %d", code)
	}
	if code := do(t, h, "POST", "/identity/export", map[string]string{"passphrase": "correct horse"}, &exp); code != 200 || !strings.HasPrefix(exp["backup"], identity.BackupPrefix) {
		t.Fatalf("export: %d %v", code, exp)
	}

	other, _ := identity.Generate()
	backup, err := other.Backup("another phrase")
	if err != nil {
		t.Fatal(err)
	}
	if code := do(t, h, "POST", "/identity/import", map[string]string{"backup": backup, "passphrase": "wrong phrase"}, nil); code != 400 {
		t.Fatalf("wrong passphrase: %d", code)
	}
	var res map[string]string
	if code := do(t, h, "POST", "/identity/import", map[string]string{"backup": backup, "passphrase": "another phrase"}, &res); code != 200 {
		t.Fatal(code)
	}
	if d.Node().Identity().Hash() != other.Hash() || res["hash"] != other.Hash().String() {
		t.Fatal("identity not switched")
	}
	if _, err := os.Stat(res["replaced"]); err != nil {
		t.Fatalf("old key not kept: %v", err)
	}
}

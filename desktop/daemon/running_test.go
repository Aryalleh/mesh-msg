package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// A second meshd on the same data directory refuses to start and leaves the
// first one's control socket alone.
func TestSecondDaemonRefuses(t *testing.T) {
	dir := t.TempDir()
	if Running(dir) {
		t.Fatal("Running with no daemon")
	}
	first, err := net.Listen("unix", SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if !Running(dir) {
		t.Fatal("Running false with a daemon listening")
	}

	cfg, err := LoadConfig(filepath.Join(dir, "config.toml"), dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Relays, cfg.UI, cfg.LAN.Discovery = nil, "", false
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Run(context.Background()); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Run = %v, want ErrRunning", err)
	}
	if _, err := os.Stat(SocketPath(dir)); err != nil {
		t.Fatalf("first daemon's socket gone: %v", err)
	}
	if !Running(dir) {
		t.Fatal("first daemon no longer reachable")
	}
}

// A socket file left by a crashed meshd doesn't block the next start.
func TestStaleSocketIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(SocketPath(dir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if Running(dir) {
		t.Fatal("stale socket file counted as running")
	}
}

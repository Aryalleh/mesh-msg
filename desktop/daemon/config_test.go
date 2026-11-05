package daemon

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigParses(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(dir, "meshd.toml"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoRa.Enabled || cfg.LoRa.Port != "/dev/ttyACM0" || !cfg.LAN.Enabled || cfg.UI != "127.0.0.1:7878" {
		t.Fatalf("%+v", cfg)
	}
}

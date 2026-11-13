package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallFromTarball(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "r.tar.gz")
	f, _ := os.Create(tgz)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"d/meshd": "new meshd", "d/mesh": "new mesh", "d/README.md": "x"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()

	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "meshd"), []byte("old"), 0o755)
	if err := installFromTarball(tgz, bin, "meshd", "mesh"); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]string{"meshd": "new meshd", "mesh": "new mesh"} {
		b, _ := os.ReadFile(filepath.Join(bin, n))
		if string(b) != want {
			t.Errorf("%s = %q", n, b)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "README.md")); err == nil {
		t.Error("extra file installed")
	}
	if err := installFromTarball(tgz, bin, "meshd", "missing"); err == nil {
		t.Error("missing binary not reported")
	}
	if b, _ := os.ReadFile(filepath.Join(bin, "meshd")); string(b) != "new meshd" {
		t.Error("partial install changed meshd")
	}
}

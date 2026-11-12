package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
)

func TestIdentityExportImport(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	id, _ := identity.Generate()
	if _, err := node.RestoreIdentity(src, id, false); err != nil {
		t.Fatal(err)
	}
	client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dst, "no.sock")) // meshd not running
	}}}
	pass := "correct horse"
	askPassphrase = func(string) (string, error) { return pass, nil }

	dataDir = src
	var out bytes.Buffer
	if err := exportIdentity("", &out); err != nil {
		t.Fatal(err)
	}
	backup := strings.TrimSpace(out.String())
	file := filepath.Join(t.TempDir(), "backup.txt")
	if err := exportIdentity(file, nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file mode %v", fi.Mode())
	}

	dataDir = dst
	// Pasted on stdin, wrong passphrase first.
	pass = "wrong passphrase"
	if err := importIdentity("", false, strings.NewReader(backup+"\n")); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	pass = "correct horse"
	if err := importIdentity("", false, strings.NewReader(backup+"\n")); err != nil {
		t.Fatal(err)
	}
	if got, err := node.ReadIdentity(dst); err != nil || got.Hash() != id.Hash() {
		t.Fatal("not restored", err)
	}
	// From the file; a different identity needs -replace.
	other, _ := identity.Generate()
	otherBackup, _ := other.Backup(pass)
	if err := importIdentity("-", false, strings.NewReader(otherBackup)); err == nil || !strings.Contains(err.Error(), "-replace") {
		t.Fatalf("want -replace hint, got %v", err)
	}
	if err := importIdentity(file, true, nil); err != nil { // same identity: no-op
		t.Fatal(err)
	}
	if err := importIdentity("-", true, strings.NewReader(otherBackup)); err != nil {
		t.Fatal(err)
	}
	if got, _ := node.ReadIdentity(dst); got.Hash() != other.Hash() {
		t.Fatal("not replaced")
	}

	// Mismatched confirmation on export.
	n := 0
	askPassphrase = func(string) (string, error) { n++; return strings.Repeat("x", 8+n), nil }
	if err := exportIdentity("", &out); err == nil {
		t.Fatal("mismatch accepted")
	}
}

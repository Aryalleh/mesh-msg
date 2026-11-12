package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestBackupRoundTrip(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b1, err := a.Backup("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := a.Backup("correct horse")
	if !strings.HasPrefix(b1, BackupPrefix) || b1 == b2 {
		t.Fatalf("backup %q / %q: want prefix and a fresh salt each time", b1, b2)
	}
	if len(b1) > 200 {
		t.Fatalf("backup is %d chars, want a small QR", len(b1))
	}
	// Wrapped and indented, as copied from a page.
	wrapped := "  " + b1[:40] + "\n" + b1[40:100] + " \r\n" + b1[100:] + "\n"
	got, err := FromBackup(wrapped, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash() != a.Hash() || string(got.Seed()) != string(a.Seed()) {
		t.Fatal("restored identity differs")
	}
}

func TestBackupRejects(t *testing.T) {
	a, _ := Generate()
	if _, err := a.Backup("short"); !errors.Is(err, ErrShortPassphrase) {
		t.Fatalf("short passphrase: %v", err)
	}
	b, _ := a.Backup("correct horse")
	if _, err := FromBackup(b, "correct hors"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	// Flip one character of the body: authentication must fail.
	i := len(b) - 10
	c := byte('A')
	if b[i] == 'A' {
		c = 'B'
	}
	if _, err := FromBackup(b[:i]+string(c)+b[i+1:], "correct horse"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("tampered: %v", err)
	}
	// A different version prefix is not accepted (the prefix is authenticated too).
	if _, err := FromBackup("meshkey2:"+b[len(BackupPrefix):], "correct horse"); !errors.Is(err, ErrNotBackup) {
		t.Fatalf("other prefix: %v", err)
	}
	for _, s := range []string{"", "meshmsg:abc", BackupPrefix, BackupPrefix + "!!!", b[:len(b)-4]} {
		if _, err := FromBackup(s, "correct horse"); !errors.Is(err, ErrNotBackup) {
			t.Fatalf("%q: %v", s, err)
		}
	}
}

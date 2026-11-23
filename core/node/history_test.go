package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
)

func TestHistoryBackupRoundTrip(t *testing.T) {
	src := t.TempDir()
	n, err := Open(Config{DataDir: src})
	if err != nil {
		t.Fatal(err)
	}
	friend, _ := identity.Generate()
	if _, err := n.AddContact(identity.Contact{Public: friend.Public}.URI(), "Sara"); err != nil {
		t.Fatal(err)
	}
	me := n.Identity().Hash()
	out := filepath.Join(t.TempDir(), "history.bin")
	if err := n.ExportHistory("short", out); !errors.Is(err, identity.ErrShortPassphrase) {
		t.Fatal("short passphrase accepted:", err)
	}
	if err := n.ExportHistory("correct horse", out); err != nil {
		t.Fatal(err)
	}
	n.Close()

	dst := t.TempDir()
	if _, err := RestoreHistory(dst, out, "wrong passphrase", false); !errors.Is(err, identity.ErrBadPassphrase) {
		t.Fatal("wrong passphrase:", err)
	}
	hash, err := RestoreHistory(dst, out, "correct horse", false)
	if err != nil || hash != me.String() {
		t.Fatal(hash, err)
	}
	m, err := Open(Config{DataDir: dst})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Identity().Hash() != me {
		t.Fatal("identity not restored")
	}
	cs, err := m.Contacts()
	if err != nil || len(cs) != 1 || cs[0].Name != "Sara" {
		t.Fatalf("contacts not restored: %+v %v", cs, err)
	}

	// another account's directory is refused without replace
	other := t.TempDir()
	o, _ := identity.Generate()
	if _, err := RestoreIdentity(other, o, false); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreHistory(other, out, "correct horse", false); !errors.Is(err, ErrOtherIdentity) {
		t.Fatal(err)
	}
}

func TestHistoryRejectsOtherFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(p, []byte("meshkey1:abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreHistory(t.TempDir(), p, "correct horse", false); !errors.Is(err, ErrNotHistory) {
		t.Fatal(err)
	}
}

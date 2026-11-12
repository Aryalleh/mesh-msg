package node

import (
	"errors"
	"os"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
)

func TestRestoreIdentity(t *testing.T) {
	dir := t.TempDir()
	a, _ := identity.Generate()
	b, _ := identity.Generate()

	// Fresh dir: installed, and a node opened there uses it.
	if r, err := RestoreIdentity(dir, a, false); err != nil || r != "" {
		t.Fatal(r, err)
	}
	n, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if n.Identity().Hash() != a.Hash() {
		t.Fatal("node did not pick up the restored identity")
	}
	n.Close()

	// Same identity again: no-op.
	if r, err := RestoreIdentity(dir, a, false); err != nil || r != "" {
		t.Fatal(r, err)
	}
	// A different one is refused without replace…
	if _, err := RestoreIdentity(dir, b, false); !errors.Is(err, ErrOtherIdentity) {
		t.Fatal(err)
	}
	// …and with replace, the old key is kept aside.
	r, err := RestoreIdentity(dir, b, true)
	if err != nil || r == "" {
		t.Fatal(r, err)
	}
	if got, err := ReadIdentity(dir); err != nil || got.Hash() != b.Hash() {
		t.Fatal("not replaced", err)
	}
	if fi, err := os.Stat(r); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("old key not kept", err)
	}
}

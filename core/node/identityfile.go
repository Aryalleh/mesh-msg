package node

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
)

// IdentityFile holds the node's 64-byte secret (hex) inside its data dir.
const IdentityFile = "identity.key"

// ErrOtherIdentity means the data dir already holds a different identity.
var ErrOtherIdentity = errors.New("node: this data directory already has a different identity")

// RestoreIdentity installs id as the identity of the node in dataDir. The
// node must not be running. If dataDir already holds the same identity this
// is a no-op. If it holds a different one, RestoreIdentity fails with
// ErrOtherIdentity unless replace is set; the old key is then kept next to
// it as identity.key.replaced-<unix time> and the returned path names it.
func RestoreIdentity(dataDir string, id *identity.Identity, replace bool) (replaced string, err error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, IdentityFile)
	seed := []byte(hex.EncodeToString(id.Seed()))
	if old, err := os.ReadFile(path); err == nil {
		if bytes.Equal(bytes.TrimSpace(old), seed) {
			return "", nil
		}
		if !replace {
			return "", ErrOtherIdentity
		}
		replaced = fmt.Sprintf("%s.replaced-%d", path, time.Now().Unix())
		if err := os.Rename(path, replaced); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, seed, 0o600); err != nil {
		return replaced, err
	}
	return replaced, os.Rename(tmp, path)
}

// ReadIdentity loads the identity stored in dataDir without opening the node.
func ReadIdentity(dataDir string) (*identity.Identity, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, IdentityFile))
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("node: identity file: %w", err)
	}
	return identity.FromSeed(seed)
}

// BackupIdentity returns this node's secret sealed with passphrase (see
// identity.Backup).
func (n *Node) BackupIdentity(passphrase string) (string, error) { return n.id.Backup(passphrase) }

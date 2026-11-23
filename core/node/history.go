package node

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
)

// A chat-history backup is the account key plus a snapshot of app.db
// (contacts, messages, settings; not media files), sealed with a passphrase:
//
//	meshhist1\n || salt || nonce || XChaCha20-Poly1305(u32be(len seed) || seed || app.db)
//
// Same key derivation as identity backups. Whoever stores the file (for
// example Hamyar, for a university that turned chat history on) cannot read it.
const HistoryPrefix = "meshhist1\n"

// ErrNotHistory means the file is not a chat-history backup.
var ErrNotHistory = errors.New("node: not a chat-history backup")

// ExportHistory writes a sealed chat-history backup to out.
func (n *Node) ExportHistory(passphrase, out string) error {
	tmp := out + ".db.tmp"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)
	// a consistent copy while the node keeps running
	if _, err := n.db.db.Exec("VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	db, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	seed := n.id.Seed()
	payload := make([]byte, 4, 4+len(seed)+len(db))
	binary.BigEndian.PutUint32(payload, uint32(len(seed)))
	payload = append(append(payload, seed...), db...)
	clear(seed)
	sealed, err := identity.SealWithPassphrase(HistoryPrefix, passphrase, payload)
	clear(payload)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+".tmp", sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(out+".tmp", out)
}

// RestoreHistory installs a backup made by ExportHistory into dataDir before
// the node opens: the account key (see RestoreIdentity for replace) and
// app.db (an existing one is kept as app.db.replaced-<unix time>). Returns
// the account's hash.
func RestoreHistory(dataDir, path, passphrase string, replace bool) (string, error) {
	sealed, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	payload, err := identity.OpenWithPassphrase(HistoryPrefix, passphrase, sealed)
	if errors.Is(err, identity.ErrNotBackup) {
		return "", ErrNotHistory
	}
	if err != nil {
		return "", err
	}
	defer clear(payload)
	if len(payload) < 4 {
		return "", ErrNotHistory
	}
	n := binary.BigEndian.Uint32(payload)
	if uint64(len(payload)) < 4+uint64(n) {
		return "", ErrNotHistory
	}
	id, err := identity.FromSeed(payload[4 : 4+n])
	if err != nil {
		return "", err
	}
	if _, err := RestoreIdentity(dataDir, id, replace); err != nil {
		return "", err
	}
	dbPath := filepath.Join(dataDir, "app.db")
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, fmt.Sprintf("%s.replaced-%d", dbPath, time.Now().Unix())); err != nil {
			return "", err
		}
	}
	for _, s := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + s)
	}
	if err := os.WriteFile(dbPath+".tmp", payload[4+n:], 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(dbPath+".tmp", dbPath); err != nil {
		return "", err
	}
	return id.Hash().String(), nil
}

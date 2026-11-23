package identity

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// An identity backup is the 64-byte secret sealed with a passphrase, as one
// line of text that fits a QR code:
//
//	meshkey1:<base64url(salt[16] || nonce[24] || XChaCha20-Poly1305(seed)[80])>
//
// The key is Argon2id(passphrase, salt, t=3, m=64 MiB, p=4). The prefix is
// the format version and fixes these parameters; the AEAD binds the prefix.
const (
	BackupPrefix        = "meshkey1:"
	MinPassphraseLength = 8

	backupSaltLen = 16
	argonTime     = 3
	argonMemoryKB = 64 * 1024
	argonThreads  = 4
)

var (
	// ErrBadPassphrase means the passphrase is wrong or the backup was altered.
	ErrBadPassphrase = errors.New("identity: wrong passphrase or damaged backup")
	// ErrNotBackup means the text is not an identity backup.
	ErrNotBackup = errors.New("identity: not a mesh-msg identity backup (meshkey1:…)")
	// ErrShortPassphrase means the passphrase is under MinPassphraseLength characters.
	ErrShortPassphrase = errors.New("identity: passphrase must be at least 8 characters")
)

func backupKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemoryKB, argonThreads, chacha20poly1305.KeySize)
}

// Backup returns the identity's secret sealed with passphrase.
func (id *Identity) Backup(passphrase string) (string, error) {
	if utf8.RuneCountInString(passphrase) < MinPassphraseLength {
		return "", ErrShortPassphrase
	}
	buf := make([]byte, backupSaltLen+chacha20poly1305.NonceSizeX, backupSaltLen+chacha20poly1305.NonceSizeX+64+chacha20poly1305.Overhead)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	aead, err := chacha20poly1305.NewX(backupKey(passphrase, buf[:backupSaltLen]))
	if err != nil {
		return "", err
	}
	seed := id.Seed()
	out := aead.Seal(buf, buf[backupSaltLen:], seed, []byte(BackupPrefix))
	clear(seed)
	return BackupPrefix + base64.RawURLEncoding.EncodeToString(out), nil
}

// FromBackup opens a backup made by Backup. Whitespace and line breaks in
// the text are ignored, so a backup copied from a wrapped page still works.
func FromBackup(text, passphrase string) (*Identity, error) {
	text = strings.Join(strings.Fields(text), "")
	if !strings.HasPrefix(text, BackupPrefix) {
		return nil, ErrNotBackup
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(text[len(BackupPrefix):], "="))
	if err != nil || len(raw) != backupSaltLen+chacha20poly1305.NonceSizeX+64+chacha20poly1305.Overhead {
		return nil, ErrNotBackup
	}
	aead, err := chacha20poly1305.NewX(backupKey(passphrase, raw[:backupSaltLen]))
	if err != nil {
		return nil, err
	}
	nonce := raw[backupSaltLen : backupSaltLen+chacha20poly1305.NonceSizeX]
	seed, err := aead.Open(nil, nonce, raw[backupSaltLen+chacha20poly1305.NonceSizeX:], []byte(BackupPrefix))
	if err != nil {
		return nil, ErrBadPassphrase
	}
	defer clear(seed)
	return FromSeed(seed)
}

// SealWithPassphrase seals data with the same key derivation as Backup
// (Argon2id t=3, m=64 MiB, p=4; XChaCha20-Poly1305), bound to prefix:
// prefix || salt[16] || nonce[24] || ciphertext. Used for chat-history backups.
func SealWithPassphrase(prefix, passphrase string, data []byte) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < MinPassphraseLength {
		return nil, ErrShortPassphrase
	}
	head := make([]byte, backupSaltLen+chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(head); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(backupKey(passphrase, head[:backupSaltLen]))
	if err != nil {
		return nil, err
	}
	out := append([]byte(prefix), head...)
	return aead.Seal(out, head[backupSaltLen:], data, []byte(prefix)), nil
}

// OpenWithPassphrase opens what SealWithPassphrase made with the same prefix.
func OpenWithPassphrase(prefix, passphrase string, sealed []byte) ([]byte, error) {
	if !strings.HasPrefix(string(sealed[:min(len(sealed), len(prefix))]), prefix) {
		return nil, ErrNotBackup
	}
	raw := sealed[len(prefix):]
	if len(raw) < backupSaltLen+chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return nil, ErrNotBackup
	}
	aead, err := chacha20poly1305.NewX(backupKey(passphrase, raw[:backupSaltLen]))
	if err != nil {
		return nil, err
	}
	data, err := aead.Open(nil, raw[backupSaltLen:backupSaltLen+chacha20poly1305.NonceSizeX], raw[backupSaltLen+chacha20poly1305.NonceSizeX:], []byte(prefix))
	if err != nil {
		return nil, ErrBadPassphrase
	}
	return data, nil
}

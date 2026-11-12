// Package media encrypts files for sending and describes them in messages.
//
// A file never travels through the packet mesh. The sender seals it with a
// fresh random key into a blob and the chat message carries only a small
// reference (Ref: id, key, size, type, name, where to fetch it). The blob
// moves over fast paths only: directly between neighbours (LAN, Bluetooth;
// pulled in small windows so chats never wait behind it) and through relays
// that accept media (PUT/GET /v1/media/{id}). Relays and neighbours see only
// ciphertext; the id is the first 16 bytes of SHA-256(blob), so anyone can
// check a blob without the key.
//
// Blob: the file cut into 64 KiB chunks, each sealed with
// ChaCha20-Poly1305(key, nonce = 0^8 || i(4), ad = "mesh-msg media v1" || i(4) || last(1)),
// concatenated. The last-chunk flag stops truncation.
package media

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/textenc"
)

const (
	ChunkSize = 64 << 10
	adDomain  = "mesh-msg media v1"
	// MaxFileBytes is the hard limit of this client; relays set their own.
	MaxFileBytes = 64 << 20
	MaxURLs      = 4
)

// ID identifies a blob: SHA-256(blob)[:16].
type ID = packet.Hash

// BlobID computes a blob's id.
func BlobID(blob []byte) ID {
	s := sha256.Sum256(blob)
	var id ID
	copy(id[:], s[:])
	return id
}

// SealedSize is the blob size for a file of n bytes.
func SealedSize(n int) int {
	chunks := max(1, (n+ChunkSize-1)/ChunkSize)
	return n + chunks*chacha20poly1305.Overhead
}

// PlainSize is the file size for a blob of n bytes.
func PlainSize(n int) int {
	c := ChunkSize + chacha20poly1305.Overhead
	chunks := max(1, (n+c-1)/c)
	return max(0, n-chunks*chacha20poly1305.Overhead)
}

func nonce(i uint32) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint32(n[8:], i)
	return n
}

func ad(i uint32, last bool) []byte {
	b := binary.BigEndian.AppendUint32([]byte(adDomain), i)
	if last {
		return append(b, 1)
	}
	return append(b, 0)
}

// Seal encrypts a file with a new random key.
func Seal(file []byte) (blob []byte, key [32]byte, id ID, err error) {
	if len(file) > MaxFileBytes {
		return nil, key, id, fmt.Errorf("media: file larger than %d MB", MaxFileBytes>>20)
	}
	if _, err = rand.Read(key[:]); err != nil {
		return
	}
	aead, _ := chacha20poly1305.New(key[:])
	blob = make([]byte, 0, SealedSize(len(file)))
	for i := uint32(0); ; i++ {
		n := min(ChunkSize, len(file))
		last := n == len(file)
		blob = aead.Seal(blob, nonce(i), file[:n], ad(i, last))
		file = file[n:]
		if last {
			break
		}
	}
	return blob, key, BlobID(blob), nil
}

// ErrBad means the blob is not the file of this reference.
var ErrBad = errors.New("media: blob does not match")

// Open checks and decrypts a blob.
func Open(blob []byte, key [32]byte, id ID) ([]byte, error) {
	if BlobID(blob) != id {
		return nil, ErrBad
	}
	aead, _ := chacha20poly1305.New(key[:])
	out := make([]byte, 0, PlainSize(len(blob)))
	c := ChunkSize + chacha20poly1305.Overhead
	for i := uint32(0); len(blob) > 0; i++ {
		n := min(c, len(blob))
		last := n == len(blob)
		var err error
		if out, err = aead.Open(out, nonce(i), blob[:n], ad(i, last)); err != nil {
			return nil, ErrBad
		}
		blob = blob[n:]
	}
	return out, nil
}

// Ref is what a chat message carries about a file.
type Ref struct {
	ID      ID
	Key     [32]byte
	Size    int      // blob bytes
	Mime    string   // e.g. image/jpeg
	Name    string   // file name shown to the user
	URLs    []string // relays that hold (or will hold) the blob
	Caption string
}

// FileSize is the size of the file itself.
func (r *Ref) FileSize() int { return PlainSize(r.Size) }

func short(s string, max int) string {
	for len(s) > max {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

func appendStr(b []byte, s string, max int) []byte {
	s = short(s, max)
	return append(append(b, byte(len(s))), s...)
}

// Encode serialises a reference:
//
//	id(16) | key(32) | size(4) | mime | name | n(1) | n × url | caption_type(1) | caption
//
// strings are len(1) | bytes; the caption uses textenc like a text message.
func (r *Ref) Encode() []byte {
	b := make([]byte, 0, 128)
	b = append(b, r.ID[:]...)
	b = append(b, r.Key[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(r.Size))
	b = appendStr(b, r.Mime, 64)
	b = appendStr(b, r.Name, 80)
	urls := r.URLs[:min(len(r.URLs), MaxURLs)]
	b = append(b, byte(len(urls)))
	for _, u := range urls {
		b = appendStr(b, u, 120)
	}
	ct, cap := textenc.Encode(r.Caption)
	b = append(b, ct)
	return append(b, cap...)
}

// DecodeRef parses a reference.
func DecodeRef(b []byte) (*Ref, error) {
	errShort := errors.New("media: truncated reference")
	if len(b) < 16+32+4 {
		return nil, errShort
	}
	r := &Ref{}
	copy(r.ID[:], b)
	copy(r.Key[:], b[16:])
	r.Size = int(binary.BigEndian.Uint32(b[48:]))
	b = b[52:]
	str := func() (string, bool) {
		if len(b) < 1 || len(b) < 1+int(b[0]) {
			return "", false
		}
		s := strings.ToValidUTF8(string(b[1:1+int(b[0])]), "")
		b = b[1+int(b[0]):]
		return s, true
	}
	var ok bool
	if r.Mime, ok = str(); !ok {
		return nil, errShort
	}
	if r.Name, ok = str(); !ok || len(b) < 1 {
		return nil, errShort
	}
	n := int(b[0])
	b = b[1:]
	if n > MaxURLs {
		return nil, errors.New("media: too many URLs")
	}
	for i := 0; i < n; i++ {
		u, ok := str()
		if !ok {
			return nil, errShort
		}
		r.URLs = append(r.URLs, u)
	}
	if len(b) < 1 {
		return nil, errShort
	}
	cap, err := textenc.Decode(b[0], b[1:])
	if err != nil {
		return nil, err
	}
	r.Caption = cap
	if r.Size <= 0 || r.Size > SealedSize(MaxFileBytes) {
		return nil, errors.New("media: bad size")
	}
	r.Name = strings.Map(func(c rune) rune {
		if c == '/' || c == '\\' || c < 32 {
			return '_'
		}
		return c
	}, r.Name)
	return r, nil
}

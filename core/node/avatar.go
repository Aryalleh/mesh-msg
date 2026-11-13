package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// Profile pictures. Each person keeps one small picture (≤ MaxAvatar bytes).
// It is never sent through relays or the mesh: a contact fetches it over a
// direct link (core/direct, authenticated per contact) with the media
// window protocol, under an ID derived from the owner's hash. It is
// fetched when the user asks (opens the chat: RequestAvatar) and refreshed
// when a direct link to the contact comes up and the copy is missing or a
// day old.

// MaxAvatar is the largest profile picture accepted (apps shrink to fit).
const MaxAvatar = 64 << 10

const avatarFresh = 24 * time.Hour

var errAvatarFormat = errors.New("profile picture must be a JPEG, PNG or WebP image")

type avatars struct {
	mu      sync.Mutex
	pending map[string]bool // contact hashes asked for, fetched on link-up
	busy    map[string]bool
}

// avatarID is the blob ID a person's picture is fetched under.
func avatarID(owner string) packet.Hash {
	s := sha256.Sum256([]byte("mava-avatar-v1:" + owner))
	var h packet.Hash
	copy(h[:], s[:])
	return h
}

func (n *Node) myAvatarPath() string { return filepath.Join(n.cfg.DataDir, "avatar") }

func (n *Node) avatarDir() string { return filepath.Join(n.cfg.DataDir, "avatars") }

func imageOK(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) || // JPEG
		bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) ||
		len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

// SetAvatar sets this account's profile picture; nil or empty removes it.
func (n *Node) SetAvatar(img []byte) error {
	if len(img) == 0 {
		err := os.Remove(n.myAvatarPath())
		if os.IsNotExist(err) {
			err = nil
		}
		n.emit(Event{Type: "avatar", Info: n.id.Hash().String()})
		return err
	}
	if len(img) > MaxAvatar {
		return errors.New("profile picture too large (max 64 KB)")
	}
	if !imageOK(img) {
		return errAvatarFormat
	}
	tmp := n.myAvatarPath() + ".tmp"
	if err := os.WriteFile(tmp, img, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, n.myAvatarPath()); err != nil {
		return err
	}
	n.emit(Event{Type: "avatar", Info: n.id.Hash().String()})
	return nil
}

// AvatarPath returns the file holding the picture of hash (a contact, or
// this account), or "" if there is none yet.
func (n *Node) AvatarPath(hash string) string {
	p := filepath.Join(n.avatarDir(), hash)
	if hash == n.id.Hash().String() {
		p = n.myAvatarPath()
	}
	if _, err := packet.ParseHash(hash); err != nil {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// RequestAvatar asks for a contact's picture: at once over a direct link if
// one is up, otherwise a direct link is opened and the picture fetched when
// it comes up. Nothing goes through relays.
func (n *Node) RequestAvatar(hash string) {
	if n.direct == nil || hash == n.id.Hash().String() {
		return
	}
	pub, ok := n.contactPub(hash)
	if !ok {
		return
	}
	if n.direct.connected(hash) {
		go n.fetchAvatar(hash)
		return
	}
	n.av.mu.Lock()
	n.av.pending[hash] = true
	n.av.mu.Unlock()
	n.direct.ensure(pub)
}

// avatarLinkUp: a direct link to hash came up.
func (n *Node) avatarLinkUp(hash string) {
	n.av.mu.Lock()
	want := n.av.pending[hash]
	delete(n.av.pending, hash)
	n.av.mu.Unlock()
	if !want {
		st, err := os.Stat(filepath.Join(n.avatarDir(), hash))
		want = err != nil || time.Since(st.ModTime()) > avatarFresh
	}
	if want {
		go n.fetchAvatar(hash)
	}
}

func (n *Node) contactPub(hash string) (identity.Public, bool) {
	for _, k := range n.contactKeys() {
		if k.Hash().String() == hash {
			return k, true
		}
	}
	return identity.Public{}, false
}

// fetchAvatar pulls hash's picture over the direct link.
func (n *Node) fetchAvatar(hash string) {
	n.av.mu.Lock()
	if n.av.busy[hash] {
		n.av.mu.Unlock()
		return
	}
	n.av.busy[hash] = true
	n.av.mu.Unlock()
	defer func() {
		n.av.mu.Lock()
		delete(n.av.busy, hash)
		n.av.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	nb := router.Neighbour{Link: "direct", Peer: router.PeerID(hash)}
	id := avatarID(hash)
	size, err := n.probeP2P(ctx, nb, id)
	if err != nil {
		return
	}
	dst := filepath.Join(n.avatarDir(), hash)
	if size == 0 {
		// The contact has no picture (any more).
		if os.Remove(dst) == nil {
			n.emit(Event{Type: "avatar", Info: hash})
		}
		return
	}
	if size > MaxAvatar {
		return
	}
	os.MkdirAll(n.avatarDir(), 0o700)
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	err = n.fetchP2P(ctx, nb, id.String(), int(size), f, 0)
	f.Close()
	b, rerr := os.ReadFile(part)
	if err != nil || rerr != nil || int64(len(b)) != size || !imageOK(b) {
		os.Remove(part)
		return
	}
	if os.Rename(part, dst) == nil {
		n.emit(Event{Type: "avatar", Info: hash})
	}
}

// probeP2P asks a neighbour for a blob's size without data (0: none).
func (n *Node) probeP2P(ctx context.Context, nb router.Neighbour, id packet.Hash) (int64, error) {
	key := waitKey(nb.Link, nb.Peer, id.String())
	ch := make(chan []byte, 4)
	n.mw.wmu.Lock()
	n.mw.waiters[key] = ch
	n.mw.wmu.Unlock()
	defer func() {
		n.mw.wmu.Lock()
		delete(n.mw.waiters, key)
		n.mw.wmu.Unlock()
	}()
	for try := 0; try < 3; try++ {
		if !n.router.SendRaw(nb.Link, nb.Peer, reqFrame(id, 0, 0)) {
			return 0, errors.New("neighbour gone")
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case fr := <-ch:
			return int64(uint32(fr[21])<<24 | uint32(fr[22])<<16 | uint32(fr[23])<<8 | uint32(fr[24])), nil
		case <-time.After(3 * time.Second): // a lost datagram: ask again
		}
	}
	return 0, errors.New("no answer")
}

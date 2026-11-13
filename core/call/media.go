package call

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/flynn/noise"
)

// On the chosen path every datagram starts with a type byte:
//
//	1 | noise message 1          caller → callee, repeated until answered
//	2 | noise message 2          callee → caller, repeated for each message 1
//	3 | frame                    media, sealed with the Noise transport keys
//
//	frame = stream_id(1) | seq(4) | timestamp(4) | flags(1) | sealed payload
//
// The Noise handshake is KK (both static keys are known from the contact)
// with fresh ephemeral keys, so a call has forward secrecy and is bound to
// both identities, on top of the path's own encryption. The AEAD nonce is
// the Noise counter n = stream_id << 32 | seq, one key per direction, so the
// receiver can open frames out of order.

const (
	dgHello1 = 1
	dgHello2 = 2
	dgFrame  = 3

	frameHead = 1 + 4 + 4 + 1
)

// Frame flags.
const (
	FlagKeyframe   = 1 << 7
	FlagFragmented = 1 << 6
	FlagFEC        = 1 << 5
	FlagEnd        = 1 << 4
)

// Frame is one media frame.
type Frame struct {
	Stream  byte
	Seq     uint32
	TS      uint32 // milliseconds since the stream started
	Flags   byte
	Payload []byte
}

var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

func keypair(id *identity.Identity) noise.DHKey {
	return noise.DHKey{Private: id.XPrivate(), Public: append([]byte(nil), id.X[:]...)}
}

// crypto is one call's handshake and transport keys.
type crypto struct {
	mu        sync.Mutex
	hs        *noise.HandshakeState
	initiator bool
	hello1    []byte // initiator: to repeat
	hello2    []byte // responder: to repeat
	send, rcv noise.Cipher
	hash      []byte
}

func newCrypto(self *identity.Identity, peer identity.Public, callID [16]byte, initiator bool) (*crypto, error) {
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: suite, Pattern: noise.HandshakeKK, Initiator: initiator,
		Prologue:      append([]byte("mava call v1"), callID[:]...),
		StaticKeypair: keypair(self), PeerStatic: append([]byte(nil), peer.X[:]...),
	})
	if err != nil {
		return nil, err
	}
	c := &crypto{hs: hs, initiator: initiator}
	if initiator {
		msg, _, _, err := hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, err
		}
		c.hello1 = append([]byte{dgHello1}, msg...)
	}
	return c, nil
}

func (c *crypto) ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.send != nil
}

// onHello handles a handshake datagram; reply, if any, is to be sent back.
func (c *crypto) onHello(b []byte) (reply []byte, done bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case b[0] == dgHello1 && !c.initiator:
		if c.hello2 != nil {
			return c.hello2, false, nil // our answer was lost
		}
		if _, _, _, err := c.hs.ReadMessage(nil, b[1:]); err != nil {
			return nil, false, err
		}
		msg, cs1, cs2, err := c.hs.WriteMessage(nil, nil)
		if err != nil || cs1 == nil {
			return nil, false, fmt.Errorf("call: handshake: %v", err)
		}
		c.hello2 = append([]byte{dgHello2}, msg...)
		c.send, c.rcv, c.hash = cs2.Cipher(), cs1.Cipher(), c.hs.ChannelBinding()
		return c.hello2, true, nil
	case b[0] == dgHello2 && c.initiator && c.send == nil:
		_, cs1, cs2, err := c.hs.ReadMessage(nil, b[1:])
		if err != nil || cs1 == nil {
			return nil, false, fmt.Errorf("call: handshake: %v", err)
		}
		c.send, c.rcv, c.hash = cs1.Cipher(), cs2.Cipher(), c.hs.ChannelBinding()
		return nil, true, nil
	}
	return nil, false, nil
}

func nonce(stream byte, seq uint32) uint64 { return uint64(stream)<<32 | uint64(seq) }

func (c *crypto) seal(f Frame) []byte {
	b := make([]byte, 1+frameHead, 1+frameHead+len(f.Payload)+16)
	b[0] = dgFrame
	b[1] = f.Stream
	binary.BigEndian.PutUint32(b[2:], f.Seq)
	binary.BigEndian.PutUint32(b[6:], f.TS)
	b[10] = f.Flags
	return c.send.Encrypt(b, nonce(f.Stream, f.Seq), b[1:], f.Payload)
}

var errOpen = errors.New("call: frame does not open")

func (c *crypto) open(b []byte) (Frame, error) {
	if len(b) < 1+frameHead+16 {
		return Frame{}, errOpen
	}
	f := Frame{Stream: b[1], Seq: binary.BigEndian.Uint32(b[2:]), TS: binary.BigEndian.Uint32(b[6:]), Flags: b[10]}
	p, err := c.rcv.Decrypt(nil, nonce(f.Stream, f.Seq), b[1:1+frameHead], b[1+frameHead:])
	if err != nil {
		return Frame{}, errOpen
	}
	f.Payload = p
	return f, nil
}

// Code is the short code both people can compare by voice: if it matches,
// nobody is in the middle (6 digits from the handshake hash).
func (c *crypto) code() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hash == nil {
		return ""
	}
	h := sha256.Sum256(append([]byte("mava call code"), c.hash...))
	v := binary.BigEndian.Uint32(h[:]) % 1000000
	return fmt.Sprintf("%03d %03d", v/1000, v%1000)
}

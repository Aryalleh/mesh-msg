// Package e2e implements end-to-end encryption of DATA packet bodies with
// standard one-way Noise patterns (docs/protocol.md §3):
//
//   - Noise_K_25519_ChaChaPoly_SHA256 for known contacts (48 bytes overhead)
//   - Noise_X_25519_ChaChaPoly_SHA256 for first contact (96 bytes overhead);
//     the sender's Ed25519 key travels inside the ciphertext so the
//     recipient learns the full identity.
//
// The sender is never visible outside the ciphertext. The recipient detects
// the pattern by trying X first (one DH, fails fast on the encrypted static
// key's tag) and then K against each contact.
package e2e

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/flynn/noise"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Content types of the inner payload.
const (
	ContentText        uint8 = 0
	ContentTextCompact uint8 = 1
	ContentContactCard uint8 = 2
	ContentMediaChunk  uint8 = 3
	ContentAck         uint8 = 4 // reserved (protocol.md §3)
	ContentGroup       uint8 = 5 // private group traffic (core/group: text, roster, leave, removed, media)
	ContentMedia       uint8 = 6 // a file: core/media reference (the file itself travels outside the mesh)
	// 7 is reserved for call signaling (docs/calls.md), not implemented yet.
)

const (
	OverheadK   = 32 + 16
	OverheadX   = 32 + 48 + 16
	InnerHeader = 5 // content_type(1) + timestamp(4)
)

// Inner is the decrypted payload.
type Inner struct {
	ContentType uint8
	Time        time.Time
	Content     []byte
}

func (in Inner) encode() []byte {
	b := make([]byte, InnerHeader, InnerHeader+len(in.Content))
	b[0] = in.ContentType
	binary.BigEndian.PutUint32(b[1:], uint32(in.Time.Unix()))
	return append(b, in.Content...)
}

func decodeInner(b []byte) (Inner, error) {
	if len(b) < InnerHeader {
		return Inner{}, errors.New("e2e: inner payload too short")
	}
	return Inner{
		ContentType: b[0],
		Time:        time.Unix(int64(binary.BigEndian.Uint32(b[1:])), 0),
		Content:     b[InnerHeader:],
	}, nil
}

var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

func keypair(id *identity.Identity) noise.DHKey {
	return noise.DHKey{Private: id.XPrivate(), Public: append([]byte(nil), id.X[:]...)}
}

// Seal encrypts inner from sender to recipient. knownContact selects Noise K
// (the recipient already has the sender's key) or Noise X (first contact).
func Seal(sender *identity.Identity, recipient identity.Public, knownContact bool, id packet.ID, inner Inner) ([]byte, error) {
	cfg := noise.Config{
		CipherSuite:   suite,
		Random:        rand.Reader,
		Initiator:     true,
		Prologue:      packet.Prologue(packet.TypeData, id, recipient.Hash()),
		StaticKeypair: keypair(sender),
		PeerStatic:    recipient.X[:],
		Pattern:       noise.HandshakeK,
	}
	payload := inner.encode()
	if !knownContact {
		cfg.Pattern = noise.HandshakeX
		payload = append(append([]byte(nil), sender.Ed[:]...), payload...)
	}
	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}
	msg, _, _, err := hs.WriteMessage(nil, payload)
	return msg, err
}

// ErrNotForMe means no pattern/contact combination decrypted the body.
var ErrNotForMe = errors.New("e2e: cannot decrypt")

// Open decrypts a DATA body addressed to recipient. contacts are the known
// senders to try for Noise K. It returns the sender's public identity and
// whether the sender was a first-contact (X) message.
func Open(recipient *identity.Identity, contacts []identity.Public, id packet.ID, body []byte) (sender identity.Public, firstContact bool, inner Inner, err error) {
	prologue := packet.Prologue(packet.TypeData, id, recipient.Hash())

	// Noise X: sender static is inside the message.
	if len(body) >= OverheadX+InnerHeader+32 {
		hs, err := noise.NewHandshakeState(noise.Config{
			CipherSuite: suite, Pattern: noise.HandshakeX, Prologue: prologue,
			StaticKeypair: keypair(recipient),
		})
		if err == nil {
			if pt, _, _, err := hs.ReadMessage(nil, body); err == nil {
				copy(sender.X[:], hs.PeerStatic())
				copy(sender.Ed[:], pt[:32])
				inner, err = decodeInner(pt[32:])
				return sender, true, inner, err
			}
		}
	}

	if len(body) < OverheadK+InnerHeader {
		return sender, false, inner, ErrNotForMe
	}
	for _, c := range contacts {
		hs, err := noise.NewHandshakeState(noise.Config{
			CipherSuite: suite, Pattern: noise.HandshakeK, Prologue: prologue,
			StaticKeypair: keypair(recipient), PeerStatic: c.X[:],
		})
		if err != nil {
			continue
		}
		if pt, _, _, err := hs.ReadMessage(nil, body); err == nil {
			inner, err = decodeInner(pt)
			return c, false, inner, err
		}
	}
	return sender, false, inner, ErrNotForMe
}

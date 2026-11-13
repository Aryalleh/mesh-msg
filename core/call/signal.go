// Package call implements one-to-one calls (docs/calls.md): signaling in
// end-to-end encrypted messages (content type 7), a direct path from
// core/direct, a Noise KK handshake on it for forward secrecy, sealed media
// frames and an adaptive jitter buffer. Audio capture, codecs and playback
// belong to the platform (Android: AudioRecord / MediaCodec / AudioTrack).
package call

import (
	"encoding/binary"
	"errors"

	"github.com/Aryalleh/mesh-msg/core/direct"
)

// Op is a signaling operation.
type Op byte

const (
	OpOffer      Op = 0
	OpAnswer     Op = 1
	OpCandidates Op = 2
	OpReject     Op = 3
	OpEnd        Op = 4
	OpCancel     Op = 5
	OpUpdate     Op = 6
	OpControl    Op = 7
)

// Reject reasons.
const (
	RejectDeclined   = 0
	RejectBusy       = 1
	RejectNotContact = 2
)

// End reasons.
const (
	EndHangup  = 0
	EndNoPath  = 1
	EndTimeout = 2
	EndError   = 3
)

// Media kinds and directions.
const (
	MediaAudio  = 0
	MediaVideo  = 1
	MediaScreen = 2
	MediaData   = 3

	DirSend    = 1
	DirReceive = 2
	DirBoth    = 3
)

// Codec ids (calls.md §2). AMR-WB is what every Android phone can encode
// without a native library; Opus encoding through MediaCodec needs Android 10.
const (
	CodecOpus  = 1
	CodecAMRWB = 2
	CodecVP8   = 16
	CodecH264  = 17
)

// Version is the call protocol version this side speaks.
const Version = 1

// StreamCap describes one stream.
type StreamCap struct {
	ID      byte
	Media   byte
	Dir     byte
	Codecs  []byte
	MaxKbps uint16
	Params  []byte
}

// Caps lists the streams a side offers or accepts.
type Caps struct {
	Version byte
	Streams []StreamCap
}

// Audio returns the main audio stream (id 0), if present.
func (c Caps) Audio() (StreamCap, bool) {
	for _, s := range c.Streams {
		if s.ID == 0 && s.Media == MediaAudio {
			return s, true
		}
	}
	return StreamCap{}, false
}

func (c Caps) encode(b []byte) []byte {
	b = append(b, c.Version, byte(len(c.Streams)))
	for _, s := range c.Streams {
		b = append(b, s.ID, s.Media, s.Dir, byte(len(s.Codecs)))
		b = append(b, s.Codecs...)
		b = binary.BigEndian.AppendUint16(b, s.MaxKbps)
		b = append(b, byte(len(s.Params)))
		b = append(b, s.Params...)
	}
	return b
}

var errShort = errors.New("call: short signal")

func decodeCaps(b []byte) (Caps, []byte, error) {
	if len(b) < 2 {
		return Caps{}, nil, errShort
	}
	c := Caps{Version: b[0]}
	n := int(b[1])
	b = b[2:]
	for i := 0; i < n; i++ {
		if len(b) < 4 {
			return Caps{}, nil, errShort
		}
		s := StreamCap{ID: b[0], Media: b[1], Dir: b[2]}
		nc := int(b[3])
		b = b[4:]
		if len(b) < nc+3 {
			return Caps{}, nil, errShort
		}
		s.Codecs = append([]byte(nil), b[:nc]...)
		s.MaxKbps = binary.BigEndian.Uint16(b[nc:])
		np := int(b[nc+2])
		b = b[nc+3:]
		if len(b) < np {
			return Caps{}, nil, errShort
		}
		s.Params = append([]byte(nil), b[:np]...)
		b = b[np:]
		c.Streams = append(c.Streams, s)
	}
	return c, b, nil
}

// Signal is one call signaling message.
//
//	call = call_id(16) | op(1) | body
//	OFFER      expires(4) | caps | key(32) | candidates
//	ANSWER     caps | candidates
//	CANDIDATES candidates
//	REJECT/END reason(1)
//	CANCEL     -
//	UPDATE     caps
//	CONTROL    stream_id(1) | kind(1) | body
//
// candidates is core/direct's offer encoding (NAT type, then the candidate
// list of calls.md). The OFFER carries the 32-byte key of the path checks
// (the message itself is end-to-end encrypted).
type Signal struct {
	CallID  [16]byte
	Op      Op
	Expires uint32 // OFFER: unix seconds
	Caps    Caps
	Key     [32]byte     // OFFER
	Offer   direct.Offer // OFFER, ANSWER, CANDIDATES
	Reason  byte         // REJECT, END
	Stream  byte         // CONTROL
	Kind    byte         // CONTROL
	Body    []byte       // CONTROL
}

// Encode serializes the signal.
func (s Signal) Encode() []byte {
	b := append(append([]byte(nil), s.CallID[:]...), byte(s.Op))
	switch s.Op {
	case OpOffer:
		b = binary.BigEndian.AppendUint32(b, s.Expires)
		b = s.Caps.encode(b)
		b = append(b, s.Key[:]...)
		b = append(b, s.Offer.Encode()...)
	case OpAnswer:
		b = s.Caps.encode(b)
		b = append(b, s.Offer.Encode()...)
	case OpCandidates:
		b = append(b, s.Offer.Encode()...)
	case OpReject, OpEnd:
		b = append(b, s.Reason)
	case OpUpdate:
		b = s.Caps.encode(b)
	case OpControl:
		b = append(b, s.Stream, s.Kind)
		b = append(b, s.Body...)
	}
	return b
}

// ErrUnknownOp: an op this version doesn't know (ignore it, never fail).
var ErrUnknownOp = errors.New("call: unknown op")

// Decode parses a signal; trailing bytes are ignored.
func Decode(b []byte) (Signal, error) {
	var s Signal
	if len(b) < 17 {
		return s, errShort
	}
	copy(s.CallID[:], b)
	s.Op = Op(b[16])
	b = b[17:]
	var err error
	switch s.Op {
	case OpOffer:
		if len(b) < 4 {
			return s, errShort
		}
		s.Expires = binary.BigEndian.Uint32(b)
		if s.Caps, b, err = decodeCaps(b[4:]); err != nil {
			return s, err
		}
		if len(b) < 32 {
			return s, errShort
		}
		copy(s.Key[:], b)
		s.Offer, _, err = direct.DecodeOffer(b[32:])
	case OpAnswer:
		if s.Caps, b, err = decodeCaps(b); err != nil {
			return s, err
		}
		s.Offer, _, err = direct.DecodeOffer(b)
	case OpCandidates:
		s.Offer, _, err = direct.DecodeOffer(b)
	case OpReject, OpEnd:
		if len(b) < 1 {
			return s, errShort
		}
		s.Reason = b[0]
	case OpCancel:
	case OpUpdate:
		s.Caps, _, err = decodeCaps(b)
	case OpControl:
		if len(b) < 2 {
			return s, errShort
		}
		s.Stream, s.Kind, s.Body = b[0], b[1], append([]byte(nil), b[2:]...)
	default:
		return s, ErrUnknownOp
	}
	return s, err
}

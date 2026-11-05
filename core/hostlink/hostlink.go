// Package hostlink is the byte-stream protocol between a LoRa node and its
// host — a phone over BLE or a PC (meshd) over USB serial (docs/firmware.md
// §2b, §6). The firmware implements the same format in C++; spec/test-vectors
// keeps the two identical.
//
// Each frame is
//
//	COBS( kind(1) | len(2, BE) | payload(len) | crc16(2, BE) ) 0x00
//
// COBS removes every zero byte, so 0x00 delimits frames and a receiver that
// starts mid-stream (or loses bytes) resynchronises at the next zero. The
// CRC is CRC-16/CCITT-FALSE over kind..payload.
package hostlink

import (
	"encoding/binary"
	"errors"
)

// Frame kinds.
const (
	KindPacket  byte = 0 // a mesh packet (encoded, no LoRa link header)
	KindCommand byte = 1 // host → node
	KindEvent   byte = 2 // node → host
	KindTunnel  byte = 3 // gateway session bytes carried for the node (§2b)
)

// MaxPayload bounds a frame payload.
const MaxPayload = 512

// ErrTooLarge is returned for payloads over MaxPayload.
var ErrTooLarge = errors.New("hostlink: payload too large")

// CRC16 is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF, no reflection).
func CRC16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, x := range b {
		crc ^= uint16(x) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// cobsEncode returns the COBS encoding of b (without the 0x00 delimiter).
func cobsEncode(b []byte) []byte {
	out := make([]byte, 1, len(b)+len(b)/254+2)
	code, codeAt := byte(1), 0
	for _, x := range b {
		if x == 0 {
			out[codeAt] = code
			codeAt = len(out)
			out = append(out, 0)
			code = 1
			continue
		}
		out = append(out, x)
		code++
		if code == 0xFF {
			out[codeAt] = code
			codeAt = len(out)
			out = append(out, 0)
			code = 1
		}
	}
	out[codeAt] = code
	return out
}

// cobsDecode reverses cobsEncode. It returns false on malformed input.
func cobsDecode(b []byte) ([]byte, bool) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		code := int(b[i])
		if code == 0 || i+code > len(b) {
			return nil, false
		}
		out = append(out, b[i+1:i+code]...)
		i += code
		if code < 0xFF && i < len(b) {
			out = append(out, 0)
		}
	}
	return out, true
}

// Encode returns one framed, delimited frame.
func Encode(kind byte, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, ErrTooLarge
	}
	raw := make([]byte, 0, 5+len(payload))
	raw = append(raw, kind, byte(len(payload)>>8), byte(len(payload)))
	raw = append(raw, payload...)
	raw = binary.BigEndian.AppendUint16(raw, CRC16(raw))
	return append(cobsEncode(raw), 0), nil
}

// Frame is a decoded frame.
type Frame struct {
	Kind    byte
	Payload []byte
}

// Decoder splits a byte stream into frames. The zero value is ready to use.
type Decoder struct {
	buf      []byte
	overflow bool
	// Errors counts discarded frames (bad COBS, length or CRC, oversize).
	Errors int
}

// maxEncoded is the longest COBS-encoded frame body.
const maxEncoded = (5 + MaxPayload) + (5+MaxPayload)/254 + 1

// Feed consumes b and returns the frames completed by it.
func (d *Decoder) Feed(b []byte) []Frame {
	var out []Frame
	for _, x := range b {
		if x != 0 {
			if len(d.buf) >= maxEncoded {
				d.overflow = true
				continue
			}
			d.buf = append(d.buf, x)
			continue
		}
		if d.overflow {
			d.Errors++
		} else if len(d.buf) > 0 {
			if f, ok := parse(d.buf); ok {
				out = append(out, f)
			} else {
				d.Errors++
			}
		}
		d.buf = d.buf[:0]
		d.overflow = false
	}
	return out
}

func parse(enc []byte) (Frame, bool) {
	raw, ok := cobsDecode(enc)
	if !ok || len(raw) < 5 {
		return Frame{}, false
	}
	n := int(raw[1])<<8 | int(raw[2])
	if n > MaxPayload || len(raw) != 5+n {
		return Frame{}, false
	}
	if CRC16(raw[:3+n]) != binary.BigEndian.Uint16(raw[3+n:]) {
		return Frame{}, false
	}
	return Frame{Kind: raw[0], Payload: append([]byte(nil), raw[3:3+n]...)}, true
}

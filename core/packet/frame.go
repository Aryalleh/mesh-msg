package packet

import (
	"encoding/binary"
	"errors"
	"io"
)

// Frames encode a list of raw packets as len(4, big-endian) || bytes, the
// stream format used by relay HTTP bodies, TCP and LAN links.

// AppendFrame appends one length-prefixed packet to dst.
func AppendFrame(dst, raw []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(raw)))
	return append(dst, raw...)
}

// EncodeFrames concatenates raw packets as frames.
func EncodeFrames(raws [][]byte) []byte {
	var out []byte
	for _, r := range raws {
		out = AppendFrame(out, r)
	}
	return out
}

// DecodeFrames splits a frame stream. It fails on truncated input or an
// oversized frame.
func DecodeFrames(b []byte) ([][]byte, error) {
	var out [][]byte
	for len(b) > 0 {
		if len(b) < 4 {
			return nil, ErrShort
		}
		n := binary.BigEndian.Uint32(b)
		if n > MaxSize {
			return nil, ErrTooLarge
		}
		if uint32(len(b)-4) < n {
			return nil, ErrShort
		}
		out = append(out, b[4:4+n])
		b = b[4+n:]
	}
	return out, nil
}

// ReadFrame reads one frame from r.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxSize {
		return nil, ErrTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// WriteFrame writes one frame to w.
func WriteFrame(w io.Writer, raw []byte) error {
	if len(raw) > MaxSize {
		return errors.New("packet: frame too large")
	}
	_, err := w.Write(AppendFrame(nil, raw))
	return err
}

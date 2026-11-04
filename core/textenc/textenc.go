// Package textenc implements the compact text encoding (content type 1)
// that stores Persian/Arabic text at one byte per character instead of two,
// roughly doubling how much Persian fits in a LoRa frame.
//
// Encoding:
//
//	0x00         escape: the next bytes are one UTF-8 encoded rune
//	0x01..0x7F   ASCII
//	0x80..0xFF   index into Table (Persian/Arabic letters, digits, punctuation)
//
// The table is part of the wire protocol: never reorder it, only append.
package textenc

import (
	"errors"
	"unicode/utf8"
)

// Table maps byte 0x80+i to Table[i].
var Table = buildTable()

func buildTable() []rune {
	var t []rune
	span := func(a, b rune) {
		for r := a; r <= b; r++ {
			t = append(t, r)
		}
	}
	span(0x0621, 0x063A)                                                  // ء … غ
	span(0x0641, 0x064A)                                                  // ف … ي
	span(0x064B, 0x0652)                                                  // harakat
	t = append(t, 0x0654, 0x0670)                                         // hamza above, superscript alef
	t = append(t, 0x067E, 0x0686, 0x0698, 0x06A9, 0x06AF, 0x06CC, 0x06C0) // پ چ ژ ک گ ی ۀ
	span(0x0660, 0x0669)                                                  // Arabic-Indic digits
	span(0x06F0, 0x06F9)                                                  // Persian digits
	t = append(t, 0x060C, 0x061B, 0x061F, 0x0640, 0x066A, 0x066B, 0x066C) // ، ؛ ؟ ـ ٪ ٫ ٬
	t = append(t, 0x00AB, 0x00BB, 0x200C, 0x200D, 0x200E, 0x200F)         // « » ZWNJ ZWJ LRM RLM
	t = append(t, 0x2026, 0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00A0)
	if len(t) > 128 {
		panic("textenc: table too large")
	}
	return t
}

var index = func() map[rune]byte {
	m := make(map[rune]byte, len(Table))
	for i, r := range Table {
		m[r] = byte(0x80 + i)
	}
	return m
}()

// Compact encodes s with the compact encoding.
func Compact(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch b, ok := index[r]; {
		case ok:
			out = append(out, b)
		case r > 0 && r < 0x80:
			out = append(out, byte(r))
		default:
			out = append(out, 0x00)
			out = utf8.AppendRune(out, r)
		}
	}
	return out
}

var ErrInvalid = errors.New("textenc: invalid compact text")

// Expand decodes compact-encoded text.
func Expand(b []byte) (string, error) {
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x00:
			r, n := utf8.DecodeRune(b[i+1:])
			if r == utf8.RuneError && n <= 1 {
				return "", ErrInvalid
			}
			out = append(out, r)
			i += 1 + n
		case c < 0x80:
			out = append(out, rune(c))
			i++
		default:
			idx := int(c - 0x80)
			if idx >= len(Table) {
				return "", ErrInvalid
			}
			out = append(out, Table[idx])
			i++
		}
	}
	return string(out), nil
}

// Content types for text (shared with core/e2e).
const (
	TypeUTF8    uint8 = 0
	TypeCompact uint8 = 1
)

// Encode picks the smaller of UTF-8 and compact encoding.
func Encode(s string) (contentType uint8, b []byte) {
	c := Compact(s)
	if len(c) < len(s) {
		return TypeCompact, c
	}
	return TypeUTF8, []byte(s)
}

// Decode reverses Encode.
func Decode(contentType uint8, b []byte) (string, error) {
	switch contentType {
	case TypeUTF8:
		if !utf8.Valid(b) {
			return "", ErrInvalid
		}
		return string(b), nil
	case TypeCompact:
		return Expand(b)
	}
	return "", errors.New("textenc: not a text content type")
}

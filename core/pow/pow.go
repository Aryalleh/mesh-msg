// Package pow implements the hashcash-style proof-of-work relays require on
// uploads from anonymous clients (no accounts → work is the spam brake).
//
// A proof is an 8-byte nonce such that SHA-256("MESHMSG-POW" || body || nonce)
// starts with at least Bits zero bits.
package pow

import (
	"crypto/sha256"
	"encoding/binary"
	"math/bits"
)

func digest(body []byte, nonce uint64) [32]byte {
	h := sha256.New()
	h.Write([]byte("MESHMSG-POW"))
	h.Write(body)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], nonce)
	h.Write(n[:])
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func leadingZeros(d [32]byte) int {
	n := 0
	for _, b := range d {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}

// Check verifies a proof.
func Check(body []byte, nonce uint64, need int) bool {
	if need <= 0 {
		return true
	}
	return leadingZeros(digest(body, nonce)) >= need
}

// Solve finds a nonce (expected 2^need hashes).
func Solve(body []byte, need int) uint64 {
	for n := uint64(0); ; n++ {
		if Check(body, n, need) {
			return n
		}
	}
}

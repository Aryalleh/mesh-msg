// Package spec generates the language-neutral test vectors in
// spec/test-vectors/vectors.json. Every implementation of the outer protocol
// (Go core, C++ firmware) must reproduce them exactly.
//
//	go run ./spec/cmd/genvectors > spec/test-vectors/vectors.json
package spec

import (
	"encoding/hex"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/textenc"
)

// Vectors is the JSON document.
type Vectors struct {
	Version   int              `json:"version"`
	Constants map[string]int   `json:"constants"`
	Headers   []HeaderVector   `json:"headers"`
	IncHop    []IncHopVector   `json:"inc_hop"`
	Fragments []FragmentVector `json:"fragments"`
	Frames    []FrameVector    `json:"frames"`
	Identity  []IdentityVector `json:"identities"`
	Announces []SignedVector   `json:"announces"`
	Receipts  []SignedVector   `json:"receipts"`
	Text      []TextVector     `json:"text"`
}

type HeaderVector struct {
	Name      string `json:"name"`
	Type      uint8  `json:"type"`
	Ctx       uint8  `json:"ctx"`
	HopCount  uint8  `json:"hop_count"`
	MaxHops   uint8  `json:"max_hops"`
	ID        string `json:"id"`
	Dest      string `json:"dest"`
	FragIndex uint8  `json:"frag_index"`
	FragCount uint8  `json:"frag_count"`
	Body      string `json:"body"`
	Encoded   string `json:"encoded"`
}

type IncHopVector struct {
	In  string `json:"in"`
	Out string `json:"out"` // "" = must be dropped (hop limit)
}

type FragmentVector struct {
	BodyLen   int      `json:"body_len"`
	MaxSize   int      `json:"max_size"`
	Fragments []string `json:"fragments"` // encoded
}

// FrameVector: LoRa link frame = sender short id || packet.
type FrameVector struct {
	SenderID uint8  `json:"sender_id"`
	Packet   string `json:"packet"`
	Frame    string `json:"frame"`
}

type IdentityVector struct {
	Seed     string `json:"seed"`
	X25519   string `json:"x25519_pub"`
	Ed25519  string `json:"ed25519_pub"`
	DestHash string `json:"dest_hash"`
	URI      string `json:"uri"`
}

type SignedVector struct {
	Seed    string `json:"seed"`
	Packet  string `json:"packet"`
	Valid   bool   `json:"valid"`
	Comment string `json:"comment"`
	Acked   string `json:"acked,omitempty"`
}

type TextVector struct {
	Text        string `json:"text"`
	ContentType uint8  `json:"content_type"`
	Encoded     string `json:"encoded"`
}

func h(b []byte) string { return hex.EncodeToString(b) }

func seed(n byte) []byte {
	s := make([]byte, 64)
	for i := range s {
		s[i] = n + byte(i)
	}
	return s
}

// Generate builds the vectors deterministically.
func Generate() Vectors {
	v := Vectors{Version: 1, Constants: map[string]int{
		"version": packet.Version, "header_size": packet.HeaderSize,
		"lora_radio_max": packet.LoRaRadioMax, "lora_link_header": packet.LoRaLinkHeader,
		"lora_mtu": packet.LoRaMTU, "max_fragments": packet.MaxFragments,
		"frag_body_size": packet.FragBodySize, "default_max_hops": packet.DefaultMaxHops,
	}}

	id := packet.ID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	dest := packet.Hash{0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	headers := []struct {
		name string
		p    packet.Packet
	}{
		{"data", packet.Packet{Type: packet.TypeData, MaxHops: 8, ID: id, Dest: dest, Body: []byte("hello")}},
		{"data-priority-nolora-hop3", packet.Packet{Type: packet.TypeData, Ctx: packet.CtxPriority | packet.CtxNoLoRa, HopCount: 3, MaxHops: 15, ID: id, Dest: dest, Body: []byte{0}}},
		{"receipt-empty-body", packet.Packet{Type: packet.TypeReceipt, MaxHops: 8, ID: id, Dest: dest}},
		{"announce", packet.Packet{Type: packet.TypeAnnounce, MaxHops: 4, ID: id, Dest: dest, Body: []byte{1, 2, 3}}},
		{"fragment-2-of-3", packet.Packet{Type: packet.TypeData, Ctx: packet.CtxFragmented, MaxHops: 8, ID: id, Dest: dest, FragIndex: 1, FragCount: 3, Body: []byte("frag")}},
		{"fragment-15-of-16", packet.Packet{Type: packet.TypeData, Ctx: packet.CtxFragmented, MaxHops: 8, ID: id, Dest: dest, FragIndex: 15, FragCount: 16, Body: []byte{0xff}}},
	}
	for _, c := range headers {
		p := c.p
		v.Headers = append(v.Headers, HeaderVector{
			Name: c.name, Type: uint8(p.Type), Ctx: p.Ctx, HopCount: p.HopCount, MaxHops: p.MaxHops,
			ID: h(p.ID[:]), Dest: h(p.Dest[:]), FragIndex: p.FragIndex, FragCount: p.FragCount,
			Body: h(p.Body), Encoded: h(p.MustEncode()),
		})
	}

	for _, hc := range []struct{ hop, max uint8 }{{0, 8}, {7, 8}, {8, 8}, {14, 15}, {15, 15}, {0, 0}} {
		p := packet.Packet{Type: packet.TypeData, HopCount: hc.hop, MaxHops: hc.max, ID: id, Dest: dest, Body: []byte("x")}
		in := p.MustEncode()
		out, ok := packet.IncHop(in)
		vec := IncHopVector{In: h(in)}
		if ok {
			vec.Out = h(out)
		}
		v.IncHop = append(v.IncHop, vec)
	}

	for _, n := range []int{100, 229, 600, 3000} {
		body := make([]byte, n)
		for i := range body {
			body[i] = byte(i)
		}
		frags, _ := packet.Fragment(&packet.Packet{Type: packet.TypeData, MaxHops: 8, ID: id, Dest: dest, Body: body}, packet.LoRaMTU)
		fv := FragmentVector{BodyLen: n, MaxSize: packet.LoRaMTU}
		for _, f := range frags {
			fv.Fragments = append(fv.Fragments, h(f.MustEncode()))
		}
		v.Fragments = append(v.Fragments, fv)
	}

	data := (&packet.Packet{Type: packet.TypeData, MaxHops: 8, ID: id, Dest: dest, Body: []byte("air")}).MustEncode()
	v.Frames = append(v.Frames, FrameVector{SenderID: 0x5a, Packet: h(data), Frame: h(append([]byte{0x5a}, data...))})

	ts := time.Unix(1_767_225_600, 0) // 2026-01-01T00:00:00Z
	for i, n := range []byte{1, 100} {
		ident, _ := identity.FromSeed(seed(n))
		v.Identity = append(v.Identity, IdentityVector{
			Seed: h(seed(n)), X25519: h(ident.X[:]), Ed25519: h(ident.Ed[:]),
			DestHash: h(ident.Hash().Bytes()), URI: identity.Contact{Public: ident.Public}.URI(),
		})

		an := announce.Build(ident, ts, announce.AppData{Name: []string{"node-a", ""}[i], Flags: announce.FlagDelegate}, 8)
		an.ID = id // signature does not cover the packet ID
		raw := an.MustEncode()
		v.Announces = append(v.Announces, SignedVector{Seed: h(seed(n)), Packet: h(raw), Valid: true, Comment: "valid announce"})
		bad := append([]byte(nil), raw...)
		bad[len(bad)-1] ^= 1
		v.Announces = append(v.Announces, SignedVector{Seed: h(seed(n)), Packet: h(bad), Valid: false, Comment: "signature bit flipped"})

		rc := announce.BuildReceipt(ident, id, 8).MustEncode()
		v.Receipts = append(v.Receipts, SignedVector{Seed: h(seed(n)), Packet: h(rc), Valid: true, Acked: h(id[:]), Comment: "valid receipt"})
	}
	other, _ := identity.FromSeed(seed(1))
	rc := announce.BuildReceipt(other, id, 8)
	rc.Dest = packet.Hash{1} // claims another destination
	v.Receipts = append(v.Receipts, SignedVector{Seed: h(seed(1)), Packet: h(rc.MustEncode()), Valid: false, Acked: h(id[:]), Comment: "destination does not match signer"})

	for _, s := range []string{"hello", "سلام دنیا", "ساعت ۱۰:۳۰ — «میدان»", "mixed فارسی 😀"} {
		ct, b := textenc.Encode(s)
		v.Text = append(v.Text, TextVector{Text: s, ContentType: ct, Encoded: h(b)})
	}
	return v
}

// Package direct finds and runs a direct IP path between two devices across
// any NAT topology (docs/connectivity.md): candidates from every interface,
// reflectors (STUN on relays), router port mapping, authenticated UDP hole
// punching, symmetric-NAT port prediction and the birthday method, and TURN
// on a relay only for that relay's own admins. The path chosen is the most
// direct one that an authenticated check proved; address classes never
// decide anything.
//
// Signaling (exchanging candidates) is the caller's job: an end-to-end
// encrypted message over any path (core/node, content type 8).
package direct

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

// Kind of a candidate (connectivity.md §3).
type Kind uint8

const (
	Host      Kind = 0 // an address of an up interface
	Reflexive Kind = 1 // outer address a reflector saw
	PeerRefl  Kind = 2 // address a peer saw our probe come from
	Local     Kind = 3 // heard from local discovery
	Mapped    Kind = 4 // port the router opened (PCP / NAT-PMP / UPnP)
	Predicted Kind = 5 // guessed port of a symmetric NAT
	Relayed   Kind = 6 // TURN allocation (relay admins only)
)

func (k Kind) String() string {
	return [...]string{"host", "reflexive", "peer-reflexive", "local", "mapped", "predicted", "relayed", "?"}[min(int(k), 7)]
}

// Candidate is one address:port to try.
type Candidate struct {
	Kind     Kind
	Addr     netip.AddrPort
	Priority uint32
}

func (c Candidate) String() string { return fmt.Sprintf("%s %s", c.Kind, c.Addr) }

// NATType classifies how an interface's NAT maps ports (connectivity.md §3a).
type NATType uint8

const (
	NATUnknown   NATType = 0 // no reflector answered
	NATOpen      NATType = 1 // the reflector saw our own address: no NAT
	NATIndep     NATType = 2 // endpoint-independent mapping: hole punching works
	NATSymSeq    NATType = 3 // a new port per destination, in steps of Step
	NATSymRandom NATType = 4 // a new random port per destination
	NATBlocked   NATType = 5 // UDP doesn't get out
)

func (t NATType) String() string {
	return [...]string{"unknown", "open", "endpoint-independent", "symmetric (sequential)", "symmetric (random)", "UDP blocked", "?"}[min(int(t), 6)]
}

// NATInfo is what a side learned about its NAT, sent with its candidates.
type NATInfo struct {
	Type NATType
	Step int16          // NATSymSeq: port step between destinations
	Last netip.AddrPort // the last mapping a reflector saw (prediction base)
}

// Offer is one side's half of the signaling: its NAT and candidates.
type Offer struct {
	NAT        NATInfo
	Candidates []Candidate
}

// class orders succeeded pairs (connectivity.md §5): lower is better.
func class(local, remote Kind, addr netip.Addr) int {
	switch {
	case local == Relayed || remote == Relayed:
		return 6
	case local == Predicted || remote == Predicted:
		return 5
	case (local == Host || local == Local) && (remote == Host || remote == Local):
		if addr.Is6() && !addr.IsPrivate() && !addr.IsLinkLocalUnicast() {
			return 2 // global IPv6 both ways, no NAT on the way
		}
		return 1 // stays on the shared local network
	case local == Mapped || remote == Mapped || local == PeerRefl || remote == PeerRefl:
		return 3
	}
	return 4
}

// priority is a candidate's default priority (higher first).
func priority(k Kind, a netip.Addr) uint32 {
	base := map[Kind]uint32{Host: 126, Local: 120, Mapped: 110, PeerRefl: 110, Reflexive: 100, Predicted: 50, Relayed: 0}[k]
	p := base << 24
	if a.Is6() {
		p += 1 << 16
	}
	return p
}

// ---- encoding (calls.md §2 "candidates", plus the NAT info) ----

// Encode writes nat_type(1) | step(2) | last | n(1) | n × candidate,
// candidate = kind(1) | family(1) | addr | port(2) | priority(4).
func (o Offer) Encode() []byte {
	b := []byte{byte(o.NAT.Type)}
	b = binary.BigEndian.AppendUint16(b, uint16(o.NAT.Step))
	b = appendAddr(b, o.NAT.Last)
	n := min(len(o.Candidates), 64)
	b = append(b, byte(n))
	for _, c := range o.Candidates[:n] {
		b = append(b, byte(c.Kind))
		b = appendAddr(b, c.Addr)
		b = binary.BigEndian.AppendUint32(b, c.Priority)
	}
	return b
}

// family(1) | addr(0, 4 or 16) | port(2); family 0 = none.
func appendAddr(b []byte, a netip.AddrPort) []byte {
	switch {
	case !a.IsValid():
		return append(b, 0)
	case a.Addr().Is4() || a.Addr().Is4In6():
		v := a.Addr().Unmap().As4()
		b = append(append(b, 4), v[:]...)
	default:
		v := a.Addr().As16()
		b = append(append(b, 6), v[:]...)
	}
	return binary.BigEndian.AppendUint16(b, a.Port())
}

func readAddr(b []byte) (netip.AddrPort, []byte, error) {
	if len(b) < 1 {
		return netip.AddrPort{}, nil, errShort
	}
	var a netip.Addr
	switch b[0] {
	case 0:
		return netip.AddrPort{}, b[1:], nil
	case 4:
		if len(b) < 1+4+2 {
			return netip.AddrPort{}, nil, errShort
		}
		a = netip.AddrFrom4([4]byte(b[1:5]))
		b = b[5:]
	case 6:
		if len(b) < 1+16+2 {
			return netip.AddrPort{}, nil, errShort
		}
		a = netip.AddrFrom16([16]byte(b[1:17]))
		b = b[17:]
	default:
		return netip.AddrPort{}, nil, errors.New("direct: unknown address family")
	}
	return netip.AddrPortFrom(a, binary.BigEndian.Uint16(b)), b[2:], nil
}

var errShort = errors.New("direct: short message")

// DecodeOffer parses Encode's output; unknown trailing bytes are ignored.
func DecodeOffer(b []byte) (Offer, []byte, error) {
	var o Offer
	if len(b) < 3 {
		return o, nil, errShort
	}
	o.NAT.Type = NATType(b[0])
	o.NAT.Step = int16(binary.BigEndian.Uint16(b[1:]))
	var err error
	if o.NAT.Last, b, err = readAddr(b[3:]); err != nil {
		return o, nil, err
	}
	if len(b) < 1 {
		return o, nil, errShort
	}
	n := int(b[0])
	b = b[1:]
	for i := 0; i < n; i++ {
		if len(b) < 1 {
			return o, nil, errShort
		}
		c := Candidate{Kind: Kind(b[0])}
		if c.Addr, b, err = readAddr(b[1:]); err != nil {
			return o, nil, err
		}
		if len(b) < 4 {
			return o, nil, errShort
		}
		c.Priority = binary.BigEndian.Uint32(b)
		b = b[4:]
		if c.Addr.IsValid() {
			o.Candidates = append(o.Candidates, c)
		}
	}
	return o, b, nil
}

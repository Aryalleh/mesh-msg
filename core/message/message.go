// Package message turns chat messages into DATA packets (encrypt, then
// fragment to LoRa size) and back (reassemble, then decrypt).
package message

import (
	"errors"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/textenc"
)

// Options for outgoing messages.
type Options struct {
	KnownContact bool  // recipient has our key → Noise K; else Noise X
	Priority     bool  // PRIORITY ctx flag (preferred on LoRa)
	MaxHops      uint8 // 0 → packet.DefaultMaxHops
	Now          time.Time
}

// MaxTextBytes is the largest encoded text that still fits in MaxFragments
// LoRa frames with first-contact overhead.
const MaxTextBytes = packet.MaxFragments*packet.FragBodySize - e2e.OverheadX - 32 - e2e.InnerHeader

// BuildText encrypts text for recipient and returns LoRa-sized packets
// (one, or several fragments sharing one packet ID).
func BuildText(sender *identity.Identity, recipient identity.Public, text string, o Options) ([]*packet.Packet, error) {
	ct, content := textenc.Encode(text)
	if len(content) > MaxTextBytes {
		return nil, errors.New("message: text too long")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.MaxHops == 0 {
		o.MaxHops = packet.DefaultMaxHops
	}
	id := packet.NewID()
	body, err := e2e.Seal(sender, recipient, o.KnownContact, id, e2e.Inner{ContentType: ct, Time: o.Now, Content: content})
	if err != nil {
		return nil, err
	}
	p := &packet.Packet{Type: packet.TypeData, MaxHops: o.MaxHops, ID: id, Dest: recipient.Hash(), Body: body}
	if o.Priority {
		p.Ctx |= packet.CtxPriority
	}
	return packet.Fragment(p, packet.LoRaMTU)
}

// Received is a decrypted incoming message.
type Received struct {
	PacketID     packet.ID
	From         identity.Public
	FirstContact bool
	Sent         time.Time
	ContentType  uint8
	Text         string // for text content types
	Content      []byte
}

// Open decrypts a complete (unfragmented or reassembled) DATA packet.
func Open(recipient *identity.Identity, contacts []identity.Public, p *packet.Packet) (*Received, error) {
	from, first, in, err := e2e.Open(recipient, contacts, p.ID, p.Body)
	if err != nil {
		return nil, err
	}
	r := &Received{PacketID: p.ID, From: from, FirstContact: first, Sent: in.Time, ContentType: in.ContentType, Content: in.Content}
	if in.ContentType == e2e.ContentText || in.ContentType == e2e.ContentTextCompact {
		if r.Text, err = textenc.Decode(in.ContentType, in.Content); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Reassembler collects fragments until a packet is complete.
type Reassembler struct {
	mu      sync.Mutex
	pending map[packet.ID]*partial
	ttl     time.Duration
}

type partial struct {
	frags   map[uint8]*packet.Packet
	firstAt time.Time
}

// NewReassembler keeps incomplete sets for ttl.
func NewReassembler(ttl time.Duration) *Reassembler {
	return &Reassembler{pending: map[packet.ID]*partial{}, ttl: ttl}
}

// Add returns the full packet once all fragments have arrived; nil otherwise.
func (r *Reassembler) Add(p *packet.Packet, now time.Time) (*packet.Packet, error) {
	if !p.Fragmented() {
		return p, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, pt := range r.pending {
		if now.Sub(pt.firstAt) > r.ttl {
			delete(r.pending, id)
		}
	}
	pt := r.pending[p.ID]
	if pt == nil {
		pt = &partial{frags: map[uint8]*packet.Packet{}, firstAt: now}
		r.pending[p.ID] = pt
	}
	cp := *p
	cp.Body = append([]byte(nil), p.Body...)
	pt.frags[p.FragIndex] = &cp
	if len(pt.frags) < int(p.FragCount) {
		return nil, nil
	}
	delete(r.pending, p.ID)
	list := make([]*packet.Packet, 0, len(pt.frags))
	for _, f := range pt.frags {
		list = append(list, f)
	}
	return packet.Reassemble(list)
}

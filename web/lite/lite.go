// Package lite is the browser's messaging client (docs/web-access.md): the
// Mava core's identity, end-to-end encryption, message, announce and receipt
// code, without the SQL databases of core/node, so it compiles to
// WebAssembly. The browser keeps its whole state as one JSON blob (Store);
// packets go out and come in through any number of transports at once — the
// Mava board's host link over HTTP (LoRa, Bluetooth and the board's own
// relay link) and relays over HTTPS — and are deduplicated by packet ID.
//
// Everything that decides anything is here and runs in `go test`; cmd/webclient
// only connects it to JavaScript.
package lite

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Store persists the client's state (localStorage in the browser).
type Store interface {
	Load() ([]byte, error) // nil, nil = nothing saved yet
	Save([]byte) error
}

// Transport carries raw packets out. Incoming packets are handed to
// Client.Ingest by the transport's own loop.
type Transport interface {
	Name() string
	Send(raws [][]byte)
}

// Contact as stored.
type Contact struct {
	Hash  string `json:"hash"`
	URI   string `json:"uri"`
	Name  string `json:"name"`
	Known bool   `json:"known"` // they have our key: messages use Noise K
	Last  int64  `json:"last"`  // Unix ms of the last message
}

// Message as stored and shown.
type Message struct {
	ID     string `json:"id"`
	Mine   bool   `json:"mine"`
	Text   string `json:"text"`
	Time   int64  `json:"time"`   // Unix ms
	Status string `json:"status"` // mine: "sent" | "delivered"; theirs: "received"
}

// Event tells the UI what changed.
type Event struct {
	Kind    string `json:"kind"` // "message" | "status" | "contact" | "link"
	Contact string `json:"contact,omitempty"`
	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type state struct {
	Seed     string               `json:"seed"`
	Name     string               `json:"name"`
	Contacts []Contact            `json:"contacts"`
	Messages map[string][]Message `json:"messages"`
	Cursors  map[string]int64     `json:"cursors"` // relay URL → mailbox cursor
}

const (
	maxMessagesPerChat = 500
	seenCap            = 4096
	announceEvery      = 30 * time.Minute
)

// Client is one browser identity.
type Client struct {
	mu         sync.Mutex
	store      Store
	st         state
	id         *identity.Identity
	transports []Transport
	reasm      *message.Reassembler
	seen       map[string]bool
	seenOrder  []string
	onEvent    func(Event)
	now        func() time.Time
	lastAnn    time.Time
}

// Open loads the saved identity or creates a new one.
func Open(store Store, name string) (*Client, error) {
	c := &Client{store: store, reasm: message.NewReassembler(10 * time.Minute), seen: map[string]bool{}, now: time.Now,
		onEvent: func(Event) {}}
	b, err := store.Load()
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &c.st); err != nil {
			return nil, fmt.Errorf("lite: saved state: %w", err)
		}
	}
	if c.st.Seed == "" {
		id, err := identity.Generate()
		if err != nil {
			return nil, err
		}
		c.st.Seed = hex.EncodeToString(id.Seed())
	}
	seed, err := hex.DecodeString(c.st.Seed)
	if err != nil {
		return nil, err
	}
	if c.id, err = identity.FromSeed(seed); err != nil {
		return nil, err
	}
	if name != "" {
		c.st.Name = name
	}
	if c.st.Messages == nil {
		c.st.Messages = map[string][]Message{}
	}
	if c.st.Cursors == nil {
		c.st.Cursors = map[string]int64{}
	}
	return c, c.save()
}

func (c *Client) save() error {
	b, err := json.Marshal(c.st)
	if err != nil {
		return err
	}
	return c.store.Save(b)
}

// OnEvent sets the UI callback (called without the client lock held).
func (c *Client) OnEvent(f func(Event)) { c.onEvent = f }

// AddTransport adds a way out; an announce goes out on it at once.
func (c *Client) AddTransport(t Transport) {
	c.mu.Lock()
	c.transports = append(c.transports, t)
	c.mu.Unlock()
	t.Send([][]byte{c.announceRaw()})
}

// Identity returns our identity (relay mailbox fetches sign with it).
func (c *Client) Identity() *identity.Identity { return c.id }

// Hash is our destination hash (hex).
func (c *Client) Hash() string { return c.id.Hash().String() }

// URI is what others add us with.
func (c *Client) URI() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return identity.Contact{Public: c.id.Public, Name: c.st.Name}.URI()
}

// Link is the https form of URI.
func (c *Client) Link() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return identity.Contact{Public: c.id.Public, Name: c.st.Name}.Link()
}

// SetName changes our display name (announced).
func (c *Client) SetName(name string) error {
	c.mu.Lock()
	c.st.Name = strings.TrimSpace(name)
	err := c.save()
	ts := append([]Transport(nil), c.transports...)
	c.mu.Unlock()
	raw := c.announceRaw()
	for _, t := range ts {
		t.Send([][]byte{raw})
	}
	return err
}

// Cursor / SetCursor keep a relay's mailbox position.
func (c *Client) Cursor(relay string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st.Cursors[relay]
}

func (c *Client) SetCursor(relay string, cur int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.st.Cursors[relay] != cur {
		c.st.Cursors[relay] = cur
		_ = c.save()
	}
}

func (c *Client) announceRaw() []byte {
	c.mu.Lock()
	name := c.st.Name
	c.lastAnn = c.now()
	c.mu.Unlock()
	p := announce.Build(c.id, c.now(), announce.AppData{Name: name, Flags: announce.FlagDelegate}, packet.DefaultMaxHops)
	raw, _ := p.Encode()
	return raw
}

// Tick re-announces now and then; call it about once a minute.
func (c *Client) Tick() {
	c.mu.Lock()
	due := c.now().Sub(c.lastAnn) >= announceEvery
	ts := append([]Transport(nil), c.transports...)
	c.mu.Unlock()
	if !due {
		return
	}
	raw := c.announceRaw()
	for _, t := range ts {
		t.Send([][]byte{raw})
	}
}

// AddContact adds (or renames) a contact from a meshmsg: URI or its https link.
func (c *Client) AddContact(uri, name string) (Contact, error) {
	ct, err := identity.ParseURI(uri)
	if err != nil {
		return Contact{}, err
	}
	if ct.Public.Hash() == c.id.Hash() {
		return Contact{}, errors.New("lite: that is you")
	}
	if strings.TrimSpace(name) == "" {
		name = ct.Name
	}
	if name == "" {
		name = ct.Public.Hash().String()[:8]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	h := ct.Public.Hash().String()
	for i := range c.st.Contacts {
		if c.st.Contacts[i].Hash == h {
			c.st.Contacts[i].Name = name
			return c.st.Contacts[i], c.save()
		}
	}
	k := Contact{Hash: h, URI: identity.Contact{Public: ct.Public}.URI(), Name: name}
	c.st.Contacts = append(c.st.Contacts, k)
	return k, c.save()
}

// RemoveContact deletes a contact and its messages.
func (c *Client) RemoveContact(hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.st.Contacts {
		if c.st.Contacts[i].Hash == hash {
			c.st.Contacts = append(c.st.Contacts[:i], c.st.Contacts[i+1:]...)
			delete(c.st.Messages, hash)
			return c.save()
		}
	}
	return errors.New("lite: no such contact")
}

// Contacts, most recent first.
func (c *Client) Contacts() []Contact {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append(make([]Contact, 0, len(c.st.Contacts)), c.st.Contacts...) // [] not null in JSON
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

// Messages of one chat, oldest first.
func (c *Client) Messages(hash string) []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(make([]Message, 0, len(c.st.Messages[hash])), c.st.Messages[hash]...)
}

func (c *Client) contact(hash string) (int, identity.Public, bool) {
	for i := range c.st.Contacts {
		if c.st.Contacts[i].Hash == hash {
			ct, err := identity.ParseURI(c.st.Contacts[i].URI)
			if err != nil {
				return 0, identity.Public{}, false
			}
			return i, ct.Public, true
		}
	}
	return 0, identity.Public{}, false
}

func (c *Client) keys() []identity.Public {
	var out []identity.Public
	for _, k := range c.st.Contacts {
		if ct, err := identity.ParseURI(k.URI); err == nil {
			out = append(out, ct.Public)
		}
	}
	return out
}

func (c *Client) add(hash string, m Message) {
	l := append(c.st.Messages[hash], m)
	if len(l) > maxMessagesPerChat {
		l = l[len(l)-maxMessagesPerChat:]
	}
	c.st.Messages[hash] = l
}

// Send encrypts text for a contact and hands it to every transport.
func (c *Client) Send(hash, text string) (Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, errors.New("lite: empty message")
	}
	c.mu.Lock()
	i, pub, ok := c.contact(hash)
	if !ok {
		c.mu.Unlock()
		return Message{}, errors.New("lite: no such contact")
	}
	now := c.now()
	pkts, err := message.BuildText(c.id, pub, text, message.Options{KnownContact: c.st.Contacts[i].Known, Now: now})
	if err != nil {
		c.mu.Unlock()
		return Message{}, err
	}
	raws := make([][]byte, 0, len(pkts))
	for _, p := range pkts {
		raw, err := p.Encode()
		if err != nil {
			c.mu.Unlock()
			return Message{}, err
		}
		raws = append(raws, raw)
		c.markSeen(p)
	}
	m := Message{ID: pkts[0].ID.String(), Mine: true, Text: text, Time: now.UnixMilli(), Status: "sent"}
	c.add(hash, m)
	c.st.Contacts[i].Last = m.Time
	err = c.save()
	ts := append([]Transport(nil), c.transports...)
	c.mu.Unlock()
	for _, t := range ts {
		t.Send(raws)
	}
	return m, err
}

func seenKey(p *packet.Packet) string {
	return fmt.Sprintf("%s/%d/%d", p.ID.String(), p.Type, p.FragIndex)
}

// markSeen reports whether p is new. Caller holds the lock.
func (c *Client) markSeen(p *packet.Packet) bool {
	k := seenKey(p)
	if c.seen[k] {
		return false
	}
	c.seen[k] = true
	c.seenOrder = append(c.seenOrder, k)
	if len(c.seenOrder) > seenCap {
		delete(c.seen, c.seenOrder[0])
		c.seenOrder = c.seenOrder[1:]
	}
	return true
}

// Ingest takes one raw packet from any transport.
func (c *Client) Ingest(raw []byte) {
	p, err := packet.Decode(raw)
	if err != nil {
		return
	}
	c.mu.Lock()
	if !c.markSeen(p) {
		c.mu.Unlock()
		return
	}
	var events []Event
	var out [][]byte
	switch p.Type {
	case packet.TypeData:
		if p.Dest != c.id.Hash() {
			break
		}
		full, err := c.reasm.Add(p, c.now())
		if err != nil || full == nil {
			break
		}
		r, err := message.Open(c.id, c.keys(), full)
		if err != nil || r.Text == "" && len(r.Content) > 0 && r.ContentType > 1 {
			break // not for us, or a content type the browser does not show yet
		}
		h := r.From.Hash().String()
		i, _, ok := c.contact(h)
		if !ok {
			c.st.Contacts = append(c.st.Contacts, Contact{Hash: h, URI: identity.Contact{Public: r.From}.URI(), Name: h[:8]})
			i = len(c.st.Contacts) - 1
			events = append(events, Event{Kind: "contact", Contact: h})
		}
		c.st.Contacts[i].Known = true // they wrote to us, so they have our key
		m := Message{ID: r.PacketID.String(), Text: r.Text, Time: c.now().UnixMilli(), Status: "received"}
		c.add(h, m)
		c.st.Contacts[i].Last = m.Time
		_ = c.save()
		events = append(events, Event{Kind: "message", Contact: h, Message: m.ID})
		rc := announce.BuildReceipt(c.id, r.PacketID, packet.DefaultMaxHops)
		if rr, err := rc.Encode(); err == nil {
			out = append(out, rr)
		}
	case packet.TypeReceipt:
		acked, err := announce.ReceiptAcked(p)
		if err != nil {
			break
		}
		id := acked.String()
		for h, list := range c.st.Messages {
			for k := range list {
				if list[k].Mine && list[k].ID == id && list[k].Status != "delivered" {
					if _, pub, ok := c.contact(h); ok {
						if _, err := announce.VerifyReceipt(p, pub); err != nil {
							continue
						}
					}
					list[k].Status = "delivered"
					_ = c.save()
					events = append(events, Event{Kind: "status", Contact: h, Message: id, Detail: "delivered"})
				}
			}
		}
	case packet.TypeAnnounce:
		a, err := announce.Parse(p)
		if err != nil {
			break
		}
		h := a.Public.Hash().String()
		if i, _, ok := c.contact(h); ok && a.App.Name != "" && c.st.Contacts[i].Name == h[:8] {
			c.st.Contacts[i].Name = a.App.Name
			_ = c.save()
			events = append(events, Event{Kind: "contact", Contact: h})
		}
	}
	ts := append([]Transport(nil), c.transports...)
	c.mu.Unlock()
	for _, t := range ts {
		if len(out) > 0 {
			t.Send(out)
		}
	}
	for _, e := range events {
		c.onEvent(e)
	}
}

// Pending lists the IDs of our recent messages without a receipt yet (the
// relays are asked for their receipts).
func (c *Client) Pending(max int) []packet.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := c.now().Add(-7 * 24 * time.Hour).UnixMilli()
	var out []packet.ID
	for _, list := range c.st.Messages {
		for i := len(list) - 1; i >= 0 && len(out) < max; i-- {
			m := list[i]
			if !m.Mine || m.Status != "sent" || m.Time < cutoff {
				continue
			}
			b, err := hex.DecodeString(m.ID)
			if err != nil || len(b) != len(packet.ID{}) {
				continue
			}
			var id packet.ID
			copy(id[:], b)
			out = append(out, id)
		}
	}
	return out
}

// Emit sends an event to the UI (transports report their state with it).
func (c *Client) Emit(e Event) { c.onEvent(e) }

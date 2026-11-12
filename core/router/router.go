// Package router implements the store-and-forward routing rules of
// docs/protocol.md §6. It knows nothing about Bluetooth, LoRa or HTTP: those
// are Links registered by the host (Android, desktop, simulator).
//
// The router is deterministic and does no I/O of its own. All Link sends and
// callbacks run after the internal lock is released, so a Link may call back
// into the router (Inbound) safely.
package router

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/group"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/store"
)

// LinkKind tells the router how a link delivers packets.
type LinkKind int

const (
	// PointToPoint links have individual peers (BLE, Wi-Fi Direct, LAN).
	PointToPoint LinkKind = iota
	// Broadcast links reach everyone in range with one transmission (LoRa).
	Broadcast
	// Uplink links lead to a relay server (internet / domestic network).
	Uplink
)

func (k LinkKind) String() string {
	return [...]string{"p2p", "broadcast", "uplink"}[k]
}

// PeerID identifies a neighbour on a PointToPoint link.
type PeerID string

// Link is a transport registered with the router.
type Link interface {
	Name() string
	Kind() LinkKind
	MTU() int
	// Send transmits raw. peer is empty for Broadcast and Uplink links.
	Send(peer PeerID, raw []byte)
}

// Config configures a Router.
type Config struct {
	Store   store.Store
	Now     func() time.Time
	MaxHops uint8 // default packet.DefaultMaxHops

	DataTTL       time.Duration // default 7 days
	SeenTTL       time.Duration // dedup memory, default 24 h
	AnnounceEvery time.Duration // 0 = only on Announce()
	AppData       announce.AppData

	// Contacts returns the known senders for a local identity (Noise K).
	Contacts func(local packet.Hash) []identity.Public

	OnMessage func(local *identity.Identity, m *message.Received)
	// OnGroupMessage receives messages of joined groups (AddGroup).
	OnGroupMessage func(m *group.Message)
	OnReceipt      func(acked packet.ID)
	OnAnnounce     func(a announce.Announce, link string, hops uint8)
	Logf           func(format string, args ...any)
}

// Route is what the router learned about a destination from announces.
type Route struct {
	Dest     packet.Hash
	Link     string
	Hops     uint8
	LastSeen time.Time
	Name     string
}

// Stats are cumulative counters.
type Stats struct {
	In, Dup, Invalid, Delivered, Forwarded, Purged int
}

// Router is safe for concurrent use.
type Router struct {
	mu     sync.Mutex
	cfg    Config
	links  map[string]Link
	order  []string // link registration order (deterministic iteration)
	peers  map[string]map[PeerID]bool
	locals map[packet.Hash]*identity.Identity
	groups map[packet.Hash]*group.Group
	known  map[packet.Hash]identity.Public
	routes map[packet.Hash]Route
	seen   map[packet.Key]time.Time
	has    map[packet.Key]map[string]bool // "link|peer" already holding the packet
	reasm  *message.Reassembler
	lastAn time.Time
	stats  Stats
}

// New creates a router.
func New(cfg Config) *Router {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxHops == 0 {
		cfg.MaxHops = packet.DefaultMaxHops
	}
	if cfg.DataTTL == 0 {
		cfg.DataTTL = 7 * 24 * time.Hour
	}
	if cfg.SeenTTL == 0 {
		cfg.SeenTTL = 24 * time.Hour
	}
	if cfg.Store == nil {
		cfg.Store = store.NewMemory()
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Router{
		cfg:    cfg,
		links:  map[string]Link{},
		peers:  map[string]map[PeerID]bool{},
		locals: map[packet.Hash]*identity.Identity{},
		groups: map[packet.Hash]*group.Group{},
		known:  map[packet.Hash]identity.Public{},
		routes: map[packet.Hash]Route{},
		seen:   map[packet.Key]time.Time{},
		has:    map[packet.Key]map[string]bool{},
		reasm:  message.NewReassembler(24 * time.Hour),
	}
}

type actions []func()

func (a *actions) add(f func()) { *a = append(*a, f) }
func (a actions) run() {
	for _, f := range a {
		f()
	}
}

// AddLocal hosts an identity on this router (messages to it are delivered).
func (r *Router) AddLocal(id *identity.Identity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.locals[id.Hash()] = id
	r.known[id.Hash()] = id.Public
}

// AddGroup joins a group: its messages are delivered (and still forwarded,
// since other members need them; no receipts), and its identity is announced
// so relays let members fetch its mailbox.
func (r *Router) AddGroup(g *group.Group) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[g.Hash()] = g
	r.known[g.Hash()] = g.Identity().Public
}

// RemoveGroup leaves a group.
func (r *Router) RemoveGroup(h packet.Hash) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.groups, h)
}

// SendGroupText sends text to a joined group as from (shown as name).
func (r *Router) SendGroupText(from *identity.Identity, name string, h packet.Hash, text string, priority bool) (packet.ID, error) {
	r.mu.Lock()
	g, ok := r.groups[h]
	r.mu.Unlock()
	if !ok {
		return packet.ID{}, errors.New("router: not a member of this group")
	}
	pkts, err := g.BuildText(from, name, text, r.cfg.MaxHops, priority, r.cfg.Now())
	if err != nil {
		return packet.ID{}, err
	}
	var acts actions
	r.mu.Lock()
	for _, p := range pkts {
		r.originate(&acts, p)
	}
	r.mu.Unlock()
	acts.run()
	return pkts[0].ID, nil
}

// AddKnown registers a public identity (e.g. a contact) so receipts from it
// can be verified before purging.
func (r *Router) AddKnown(p identity.Public) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.known[p.Hash()] = p
}

// AddLink registers a link.
func (r *Router) AddLink(l Link) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.links[l.Name()]; !ok {
		r.order = append(r.order, l.Name())
	}
	r.links[l.Name()] = l
	r.peers[l.Name()] = map[PeerID]bool{}
}

// RemoveLink unregisters a link.
func (r *Router) RemoveLink(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.links, name)
	delete(r.peers, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

func holder(link string, peer PeerID) string { return link + "|" + string(peer) }

func (r *Router) markHas(k packet.Key, link string, peer PeerID) {
	m := r.has[k]
	if m == nil {
		m = map[string]bool{}
		r.has[k] = m
	}
	m[holder(link, peer)] = true
}

func (r *Router) hasIt(k packet.Key, link string, peer PeerID) bool {
	return r.has[k][holder(link, peer)]
}

// forward queues transmissions of raw on every link that doesn't have it.
// originate=true sends the packet as is (hop 0); otherwise the hop count is
// incremented and the packet is dropped from mesh links at its hop limit.
func (r *Router) forward(acts *actions, k packet.Key, raw []byte, noLoRa bool, arrival string, from PeerID, originate bool) {
	fwd, ok := raw, true
	if !originate {
		fwd, ok = packet.IncHop(raw)
	}
	for _, name := range r.order {
		l := r.links[name]
		switch l.Kind() {
		case Uplink:
			if r.hasIt(k, name, "") || len(raw) > l.MTU() {
				continue
			}
			r.markHas(k, name, "")
			acts.add(func() { l.Send("", raw) })
		case Broadcast:
			if !ok || noLoRa || len(fwd) > l.MTU() || r.hasIt(k, name, "") {
				continue
			}
			r.markHas(k, name, "")
			acts.add(func() { l.Send("", fwd) })
		case PointToPoint:
			if !ok || len(fwd) > l.MTU() {
				continue
			}
			for _, peer := range sortedPeers(r.peers[name]) {
				if (name == arrival && peer == from) || r.hasIt(k, name, peer) {
					continue
				}
				r.markHas(k, name, peer)
				acts.add(func() { l.Send(peer, fwd) })
			}
		}
	}
	r.stats.Forwarded++
}

func sortedPeers(m map[PeerID]bool) []PeerID {
	out := make([]PeerID, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (r *Router) storeItem(p *packet.Packet, raw []byte, origin string, ttl time.Duration) {
	now := r.cfg.Now()
	_, _, err := r.cfg.Store.Put(store.Item{
		Key: p.Key(), Type: p.Type, Dest: p.Dest, Raw: raw,
		Origin: origin, Received: now, Expires: now.Add(ttl),
	})
	if err != nil {
		r.cfg.Logf("router: store: %v", err)
	}
}

// Inbound processes a packet received on link from peer (empty for
// Broadcast/Uplink links).
func (r *Router) Inbound(link string, from PeerID, raw []byte) {
	var acts actions
	r.mu.Lock()
	r.inbound(&acts, link, from, raw)
	r.mu.Unlock()
	acts.run()
}

func (r *Router) inbound(acts *actions, link string, from PeerID, raw []byte) {
	r.stats.In++
	raw = append([]byte(nil), raw...)
	p, err := packet.Decode(raw)
	if err != nil {
		r.stats.Invalid++
		return
	}
	k := p.Key()
	// Hearing a packet on a broadcast link doesn't mean everyone in our
	// range has it: we still rebroadcast once (managed flood).
	if l, ok := r.links[link]; ok && l.Kind() != Broadcast {
		r.markHas(k, link, from)
	}
	if _, dup := r.seen[k]; dup {
		r.stats.Dup++
		return
	}
	r.seen[k] = r.cfg.Now()
	if ok, _ := r.cfg.Store.Has(k); ok {
		r.stats.Dup++
		return
	}
	origin := holder(link, from)

	switch p.Type {
	case packet.TypeAnnounce:
		a, err := announce.Parse(p)
		if err != nil {
			r.stats.Invalid++
			return
		}
		r.known[p.Dest] = a.Public
		r.routes[p.Dest] = Route{Dest: p.Dest, Link: link, Hops: p.HopCount, LastSeen: r.cfg.Now(), Name: a.App.Name}
		r.storeItem(p, raw, origin, r.cfg.DataTTL)
		if cb := r.cfg.OnAnnounce; cb != nil {
			hops := p.HopCount
			acts.add(func() { cb(a, link, hops) })
		}
		r.forward(acts, k, raw, false, link, from, false)

	case packet.TypeReceipt:
		acked, err := announce.ReceiptAcked(p)
		if err != nil {
			r.stats.Invalid++
			return
		}
		if signer, ok := r.known[p.Dest]; ok {
			if _, err := announce.VerifyReceipt(p, signer); err != nil {
				r.stats.Invalid++
				return
			}
			n, _ := r.cfg.Store.DeleteData(acked)
			r.stats.Purged += n
			if cb := r.cfg.OnReceipt; cb != nil {
				acts.add(func() { cb(acked) })
			}
		}
		r.storeItem(p, raw, origin, r.cfg.DataTTL)
		r.forward(acts, k, raw, false, link, from, false)

	case packet.TypeData:
		if ok, _ := r.cfg.Store.Has(packet.Key{ID: announce.ReceiptID(p.ID)}); ok {
			return // already delivered
		}
		if local, ok := r.locals[p.Dest]; ok {
			r.deliver(acts, local, p)
			return
		}
		if g, ok := r.groups[p.Dest]; ok {
			r.deliverGroup(acts, g, p) // and keep carrying it for the other members
		}
		r.storeItem(p, raw, origin, r.cfg.DataTTL)
		r.forward(acts, k, raw, p.NoLoRa(), link, from, false)

	default:
		// PATH_REQ / SUMMARY handling is link specific (LoRa firmware).
	}
}

func (r *Router) deliver(acts *actions, local *identity.Identity, p *packet.Packet) {
	full, err := r.reasm.Add(p, r.cfg.Now())
	if err != nil || full == nil {
		return
	}
	var contacts []identity.Public
	if r.cfg.Contacts != nil {
		contacts = r.cfg.Contacts(local.Hash())
	}
	m, err := message.Open(local, contacts, full)
	if err != nil {
		r.cfg.Logf("router: cannot open message %s for %s: %v", full.ID, local.Hash(), err)
		return
	}
	if m.FirstContact {
		r.known[m.From.Hash()] = m.From
	}
	r.stats.Delivered++
	rc := announce.BuildReceipt(local, full.ID, r.cfg.MaxHops)
	r.originate(acts, rc)
	if cb := r.cfg.OnMessage; cb != nil {
		acts.add(func() { cb(local, m) })
	}
}

func (r *Router) deliverGroup(acts *actions, g *group.Group, p *packet.Packet) {
	full, err := r.reasm.Add(p, r.cfg.Now())
	if err != nil || full == nil {
		return
	}
	m, err := g.Open(full)
	if err != nil {
		r.cfg.Logf("router: group %s: %v", g.Hash(), err)
		return
	}
	r.stats.Delivered++
	if cb := r.cfg.OnGroupMessage; cb != nil {
		acts.add(func() { cb(m) })
	}
}

// originate stores and sends a packet created on this node.
func (r *Router) originate(acts *actions, p *packet.Packet) {
	raw := p.MustEncode()
	k := p.Key()
	r.seen[k] = r.cfg.Now()
	r.storeItem(p, raw, "local", r.cfg.DataTTL)
	r.forward(acts, k, raw, p.NoLoRa(), "", "", true)
}

// SendText encrypts and sends a text message. known selects Noise K (the
// recipient already has our key) instead of first-contact Noise X.
func (r *Router) SendText(from *identity.Identity, to identity.Public, text string, known, priority bool) (packet.ID, error) {
	pkts, err := message.BuildText(from, to, text, message.Options{
		KnownContact: known, Priority: priority, MaxHops: r.cfg.MaxHops, Now: r.cfg.Now(),
	})
	if err != nil {
		return packet.ID{}, err
	}
	var acts actions
	r.mu.Lock()
	r.known[to.Hash()] = to
	if local, ok := r.locals[to.Hash()]; ok { // note to self
		for _, p := range pkts {
			q, _ := packet.Decode(p.MustEncode())
			r.deliver(&acts, local, q)
		}
	} else {
		for _, p := range pkts {
			r.originate(&acts, p)
		}
	}
	r.mu.Unlock()
	acts.run()
	return pkts[0].ID, nil
}

// SendContent seals arbitrary inner content (content type ct) for to, like
// SendText; private groups use it. It returns the packet ID, whose receipt
// arrives through OnReceipt.
func (r *Router) SendContent(from *identity.Identity, to identity.Public, ct uint8, content []byte, known, priority bool) (packet.ID, error) {
	pkts, err := message.BuildContent(from, to, ct, content, message.Options{
		KnownContact: known, Priority: priority, MaxHops: r.cfg.MaxHops, Now: r.cfg.Now(),
	})
	if err != nil {
		return packet.ID{}, err
	}
	var acts actions
	r.mu.Lock()
	r.known[to.Hash()] = to
	for _, p := range pkts {
		r.originate(&acts, p)
	}
	r.mu.Unlock()
	acts.run()
	return pkts[0].ID, nil
}

// SetRelayHint sets the relay URL our announces carry (empty: none). It
// reports whether it changed.
func (r *Router) SetRelayHint(url string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cfg.AppData.Relay == url {
		return false
	}
	r.cfg.AppData.Relay = url
	return true
}

// Announce sends a fresh announce for every local identity.
func (r *Router) Announce() {
	var acts actions
	r.mu.Lock()
	r.lastAn = r.cfg.Now()
	for _, id := range r.sortedLocals() {
		r.originate(&acts, announce.Build(id, r.cfg.Now(), r.cfg.AppData, r.cfg.MaxHops))
	}
	// Groups announce without a name, with our delegation choice, so gateways
	// can fetch them for members who are offline.
	for _, g := range r.sortedGroups() {
		r.originate(&acts, announce.Build(g.Identity(), r.cfg.Now(), announce.AppData{Flags: r.cfg.AppData.Flags}, r.cfg.MaxHops))
	}
	r.mu.Unlock()
	acts.run()
}

func (r *Router) sortedGroups() []*group.Group {
	out := make([]*group.Group, 0, len(r.groups))
	for _, g := range r.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash().String() < out[j].Hash().String() })
	return out
}

func (r *Router) sortedLocals() []*identity.Identity {
	out := make([]*identity.Identity, 0, len(r.locals))
	for _, id := range r.locals {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].Ed[:]) < string(out[j].Ed[:]) })
	return out
}

// PeerUp tells the router a neighbour appeared on a PointToPoint link; it
// receives the store backlog it doesn't have yet (epidemic exchange).
func (r *Router) PeerUp(link string, peer PeerID) {
	var acts actions
	r.mu.Lock()
	if l, ok := r.links[link]; ok {
		r.peers[link][peer] = true
		r.backlog(&acts, l, peer)
	}
	r.mu.Unlock()
	acts.run()
}

// PeerDown removes a neighbour.
func (r *Router) PeerDown(link string, peer PeerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers[link], peer)
}

// LinkUp tells the router an Uplink/Broadcast link (re)connected: stored
// packets it hasn't carried yet are sent.
func (r *Router) LinkUp(link string) {
	var acts actions
	r.mu.Lock()
	if l, ok := r.links[link]; ok {
		r.backlog(&acts, l, "")
	}
	r.mu.Unlock()
	acts.run()
}

func (r *Router) backlog(acts *actions, l Link, peer PeerID) {
	// Receipts first so the peer can purge before receiving data.
	order := [][]packet.Type{{packet.TypeReceipt}, {packet.TypeAnnounce}, {packet.TypeData}}
	for _, types := range order {
		items, err := r.cfg.Store.Since(0, 0, store.Filter{Types: types})
		if err != nil {
			r.cfg.Logf("router: backlog: %v", err)
			return
		}
		for _, it := range items {
			if r.hasIt(it.Key, l.Name(), peer) {
				continue
			}
			raw := it.Raw
			if l.Kind() != Uplink && it.Origin != "local" {
				var ok bool
				if raw, ok = packet.IncHop(it.Raw); !ok {
					continue
				}
			}
			if len(raw) > l.MTU() {
				continue
			}
			if l.Kind() == Broadcast {
				if p, err := packet.Decode(raw); err != nil || p.NoLoRa() {
					continue
				}
			}
			r.markHas(it.Key, l.Name(), peer)
			acts.add(func() { l.Send(peer, raw) })
		}
	}
}

// Tick runs periodic work: expiry, dedup-cache pruning, announces.
func (r *Router) Tick() {
	now := r.cfg.Now()
	r.mu.Lock()
	if _, err := r.cfg.Store.Expire(now); err != nil {
		r.cfg.Logf("router: expire: %v", err)
	}
	for k, t := range r.seen {
		if now.Sub(t) > r.cfg.SeenTTL {
			delete(r.seen, k)
			delete(r.has, k)
		}
	}
	due := r.cfg.AnnounceEvery > 0 && now.Sub(r.lastAn) >= r.cfg.AnnounceEvery
	r.mu.Unlock()
	if due {
		r.Announce()
	}
}

// Routes returns destinations learned from announces.
func (r *Router) Routes() []Route {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Route, 0, len(r.routes))
	for _, rt := range r.routes {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Known returns the public identity for a hash if learned.
func (r *Router) Known(h packet.Hash) (identity.Public, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.known[h]
	return p, ok
}

// Stats returns counters.
func (r *Router) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// Links returns registered link names and kinds, with peer counts.
func (r *Router) Links() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, n := range r.order {
		out = append(out, fmt.Sprintf("%s (%s, %d peers)", n, r.links[n].Kind(), len(r.peers[n])))
	}
	return out
}

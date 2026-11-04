package sim

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/core/store"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

// World holds everything in one simulation.
type World struct {
	E      *Engine
	Nodes  []*Node
	byID   map[packet.Hash]*Node
	Relays map[string]*relay.Relay
	Radios []*Radio
	Places map[string]*Place
	Stats  Stats

	sent map[packet.ID]*sentMsg
}

// Stats aggregates results.
type Stats struct {
	LoRaFrames  int
	LoRaAirtime time.Duration
	Categories  map[string]*Category
}

// Category tracks delivery for one kind of message (e.g. "tehran→berlin").
type Category struct {
	Sent      int
	Delivered int
	Latencies []time.Duration
}

type sentMsg struct {
	cat       string
	at        time.Time
	delivered bool
}

// Node is a simulated device running the real router.
type Node struct {
	Name  string
	ID    *identity.Identity
	R     *router.Router
	Store store.Store
	w     *World
	Place *Place
	BLE   *bleLink
	Radio *Radio
	Up    *uplink
}

// NewWorld creates an empty world.
func NewWorld(seed int64) *World {
	return &World{
		E: NewEngine(seed), byID: map[packet.Hash]*Node{}, Relays: map[string]*relay.Relay{},
		Places: map[string]*Place{}, sent: map[packet.ID]*sentMsg{},
		Stats: Stats{Categories: map[string]*Category{}},
	}
}

// AddNode creates a node that knows every other node as a contact (the
// simulation measures transport, not contact exchange).
func (w *World) AddNode(name string) *Node {
	id, _ := identity.Generate()
	st := store.NewMemory()
	n := &Node{Name: name, ID: id, Store: st, w: w}
	n.R = router.New(router.Config{
		Store:         st,
		Now:           w.E.Now,
		AnnounceEvery: 30 * time.Minute,
		AppData:       announce.AppData{Flags: announce.FlagDelegate},
		Contacts: func(packet.Hash) []identity.Public {
			out := make([]identity.Public, 0, len(w.Nodes))
			for _, o := range w.Nodes {
				out = append(out, o.ID.Public)
			}
			return out
		},
		OnMessage: func(_ *identity.Identity, m *message.Received) { w.delivered(m.PacketID) },
	})
	n.R.AddLocal(id)
	w.Nodes = append(w.Nodes, n)
	w.byID[id.Hash()] = n
	off := time.Duration(w.E.Rand.Intn(600)) * time.Second
	w.E.After(off, n.R.Announce)
	w.E.Every(off+10*time.Minute, 10*time.Minute, n.R.Tick)
	return n
}

func (w *World) delivered(id packet.ID) {
	s, ok := w.sent[id]
	if !ok || s.delivered {
		return
	}
	s.delivered = true
	c := w.Stats.Categories[s.cat]
	c.Delivered++
	c.Latencies = append(c.Latencies, w.E.Now().Sub(s.at))
}

// Send sends a text message and tracks it under category cat.
func (w *World) Send(from, to *Node, cat string) {
	for _, o := range w.Nodes {
		from.R.AddKnown(o.ID.Public)
	}
	text := fmt.Sprintf("msg %d from %s", len(w.sent), from.Name)
	id, err := from.R.SendText(from.ID, to.ID.Public, text, true, false)
	if err != nil {
		panic(err)
	}
	c := w.Stats.Categories[cat]
	if c == nil {
		c = &Category{}
		w.Stats.Categories[cat] = c
	}
	c.Sent++
	w.sent[id] = &sentMsg{cat: cat, at: w.E.Now()}
}

// --- Bluetooth: phones at the same place form a mesh -----------------------

// Place is a location where co-located phones are in Bluetooth range.
type Place struct {
	Name    string
	present map[*Node]bool
}

// AddPlace registers a place.
func (w *World) AddPlace(name string) *Place {
	p := &Place{Name: name, present: map[*Node]bool{}}
	w.Places[name] = p
	return p
}

type bleLink struct{ n *Node }

func (b *bleLink) Name() string          { return "ble" }
func (b *bleLink) Kind() router.LinkKind { return router.PointToPoint }
func (b *bleLink) MTU() int              { return 4096 }
func (b *bleLink) Send(peer router.PeerID, raw []byte) {
	w := b.n.w
	dst := w.byName(string(peer))
	w.E.After(200*time.Millisecond, func() {
		if dst.Place != nil && dst.Place == b.n.Place { // still together
			dst.R.Inbound("ble", router.PeerID(b.n.Name), raw)
		}
	})
}

func (w *World) byName(name string) *Node {
	for _, n := range w.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

// EnableBLE gives a node a Bluetooth link.
func (n *Node) EnableBLE() {
	n.BLE = &bleLink{n: n}
	n.R.AddLink(n.BLE)
}

// MoveTo moves a node to a place (nil = travelling / out of range).
func (n *Node) MoveTo(p *Place) {
	if n.Place != nil {
		for o := range n.Place.present {
			if o != n {
				o.R.PeerDown("ble", router.PeerID(n.Name))
				n.R.PeerDown("ble", router.PeerID(o.Name))
			}
		}
		delete(n.Place.present, n)
	}
	n.Place = p
	if p == nil {
		return
	}
	p.present[n] = true
	for o := range p.present {
		if o != n && o.BLE != nil && n.BLE != nil {
			o.R.PeerUp("ble", router.PeerID(n.Name))
			n.R.PeerUp("ble", router.PeerID(o.Name))
		}
	}
}

// Wander moves a node between places: stay ~mean, travel ~travel.
func (w *World) Wander(n *Node, places []*Place, stay, travel time.Duration) {
	var step func()
	step = func() {
		n.MoveTo(nil)
		w.E.After(w.E.Exp(travel), func() {
			n.MoveTo(places[w.E.Rand.Intn(len(places))])
			w.E.After(w.E.Exp(stay), step)
		})
	}
	n.MoveTo(places[w.E.Rand.Intn(len(places))])
	w.E.After(w.E.Exp(stay), step)
}

// --- LoRa -----------------------------------------------------------------

// AddRadio gives a node a LoRa radio at (x, y) metres.
func (w *World) AddRadio(n *Node, x, y float64, p LoRaParams) *Radio {
	r := &Radio{w: w, node: n, X: x, Y: y, P: p, maxQue: 200}
	n.Radio = r
	n.R.AddLink(r)
	w.Radios = append(w.Radios, r)
	return r
}

// --- Relays, uplinks, bridges ---------------------------------------------

// AddRelay creates an in-process relay.
func (w *World) AddRelay(name, region string) *relay.Relay {
	r := relay.New(relay.Config{Name: name, Region: region, Now: w.E.Now, CarrierMinAge: 10 * time.Minute})
	w.Relays[name] = r
	return r
}

type uplink struct {
	n       *Node
	r       *relay.Relay
	online  bool
	pending [][]byte
	gateway bool
	quota   int
	gwCur   int64
	mbCur   int64
}

func (u *uplink) Name() string          { return "relay" }
func (u *uplink) Kind() router.LinkKind { return router.Uplink }
func (u *uplink) MTU() int              { return packet.MaxSize }
func (u *uplink) Send(_ router.PeerID, raw []byte) {
	u.pending = append(u.pending, raw)
	if u.online {
		u.flush()
	}
}

func (u *uplink) flush() {
	if len(u.pending) == 0 {
		return
	}
	batch := u.pending
	u.pending = nil
	u.n.w.E.After(time.Second, func() { u.r.Ingest(batch, "node:"+u.n.Name, u.r.Region()) })
}

func (u *uplink) poll() {
	if !u.online {
		return
	}
	var raws [][]byte
	box, cur := u.r.Mailbox(u.n.ID.Hash(), u.mbCur, 500)
	u.mbCur = cur
	raws = append(raws, box...)
	// receipts for what we sent
	items, _ := u.n.Store.Since(0, 0, store.Filter{Types: []packet.Type{packet.TypeData}})
	var ids []packet.ID
	for _, it := range items {
		if it.Origin == "local" {
			ids = append(ids, it.Key.ID)
		}
	}
	raws = append(raws, u.r.Receipts(ids)...)
	if u.gateway {
		var serve []packet.Hash
		for _, rt := range u.n.R.Routes() {
			if rt.Link != "relay" {
				serve = append(serve, rt.Dest)
			}
		}
		g, cur := u.r.Gateway(relay.GatewayRequest{
			Origin: "node:" + u.n.Name, Serve: serve, Carrier: true, QuotaBytes: u.quota, After: u.gwCur,
		})
		u.gwCur = cur
		raws = append(raws, g...)
	}
	u.n.w.E.After(time.Second, func() {
		for _, raw := range raws {
			u.n.R.Inbound("relay", "", raw)
		}
	})
}

// SetOnline toggles the node's relay connectivity.
func (u *uplink) SetOnline(on bool) {
	if on == u.online {
		return
	}
	u.online = on
	if on {
		u.n.R.LinkUp("relay")
		u.flush()
	}
}

// ConnectRelay gives a node an uplink to relay r, polled every poll.
// gateway enables carrier mode with quota bytes per poll.
func (w *World) ConnectRelay(n *Node, r *relay.Relay, gateway bool, quota int, poll time.Duration) *uplink {
	u := &uplink{n: n, r: r, gateway: gateway, quota: quota}
	n.Up = u
	n.R.AddLink(u)
	w.E.Every(time.Duration(w.E.Rand.Intn(int(poll))), poll, u.poll)
	return u
}

// Intermittent toggles fn(on/off) with exponential on/off periods.
func (w *World) Intermittent(meanOn, meanOff time.Duration, fn func(bool)) {
	var on, off func()
	on = func() { fn(true); w.E.After(w.E.Exp(meanOn), off) }
	off = func() { fn(false); w.E.After(w.E.Exp(meanOff), on) }
	on()
}

// Bridge syncs two relays every period while online() is true.
func (w *World) Bridge(a, b *relay.Relay, period time.Duration, online func() bool) {
	cur := &relayclient.Cursors{}
	label := "bridge-" + a.Name() + "-" + b.Name()
	sa := bridgeSide{r: a, label: label}
	sb := bridgeSide{r: b, label: label}
	w.E.Every(period, period, func() {
		if online() {
			relayclient.SyncOnce(context.Background(), sa, sb, cur, nil)
		}
	})
}

// bridgeSide is a remote relay as a bridge sees it (labels like the HTTP API).
type bridgeSide struct {
	r     *relay.Relay
	label string
}

func (s bridgeSide) Name() string { return s.r.Name() }
func (s bridgeSide) Pull(_ context.Context, after int64, limit int) ([]wire.SyncItem, int64, bool, error) {
	items, cur, more := s.r.Pull(after, limit, "peer:"+s.label)
	return items, cur, more, nil
}
func (s bridgeSide) Push(_ context.Context, items []wire.SyncItem) error {
	s.r.IngestSync(items, "peer:"+s.label)
	return nil
}

// --- Report ---------------------------------------------------------------

// Report renders results as text.
func (w *World) Report(title string, dur time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "== %s (%v simulated, %d events)\n", title, dur, w.E.steps)
	cats := make([]string, 0, len(w.Stats.Categories))
	for c := range w.Stats.Categories {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	fmt.Fprintf(&b, "%-22s %6s %9s %8s %10s %10s\n", "messages", "sent", "delivered", "ratio", "median", "p90")
	for _, name := range cats {
		c := w.Stats.Categories[name]
		ratio := 0.0
		if c.Sent > 0 {
			ratio = float64(c.Delivered) / float64(c.Sent)
		}
		fmt.Fprintf(&b, "%-22s %6d %9d %7.0f%% %10s %10s\n", name, c.Sent, c.Delivered, ratio*100,
			pct(c.Latencies, 0.5), pct(c.Latencies, 0.9))
	}
	if w.Stats.LoRaFrames > 0 {
		fmt.Fprintf(&b, "LoRa: %d frames, %v total airtime\n", w.Stats.LoRaFrames, w.Stats.LoRaAirtime.Round(time.Second))
		for _, r := range w.Radios {
			share := float64(r.Stats.Airtime) / float64(dur) * 100
			fmt.Fprintf(&b, "  %-12s tx %4d  airtime %5.2f%%  rx %4d  missed(busy) %3d  retries %3d  budget-waits %4d  announce-drops %4d  queue-drops %3d\n",
				r.node.Name, r.Stats.Frames, share, r.Stats.Received, r.Stats.MissedBusy, r.Stats.Retries,
				r.Stats.BudgetWaits, r.Stats.AnnDropped, r.Stats.QueueDrops)
		}
	}
	for name, r := range w.Relays {
		s := r.Stats()
		fmt.Fprintf(&b, "relay %-8s accepted %d, duplicates %d, purged %d\n", name, s.Accepted, s.Duplicate, s.Purged)
	}
	return b.String()
}

func pct(ds []time.Duration, p float64) string {
	if len(ds) == 0 {
		return "-"
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(float64(len(s)-1)*p)].Round(time.Second).String()
}

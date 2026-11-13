// Package node is the client runtime shared by the desktop daemon (meshd)
// and the Android app: identity, contacts, message history, the router, and
// the links that need no platform-specific code (relays, LAN). Platforms add
// their own links (BLE, Wi-Fi Direct, LoRa) through Router().
package node

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/call"
	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/core/store"
)

// RelayConfig is one relay this node talks to.
type RelayConfig struct {
	Name  string `toml:"name" json:"name"`
	URL   string `toml:"url" json:"url"`
	Via   string `toml:"via" json:"via"`     // direct | tor | socks5://… | http://…
	Token string `toml:"token" json:"token"` // only for bridge sync
}

// GatewayConfig enables serving the local mesh from relays.
type GatewayConfig struct {
	Enabled        bool   `toml:"enabled" json:"enabled"`
	Mode           string `toml:"mode" json:"mode"` // "targeted" | "carrier"
	QuotaKBPerHour int    `toml:"quota_kb_per_hour" json:"quota_kb_per_hour"`
}

// BridgeConfig syncs two relays that can't reach each other.
type BridgeConfig struct {
	Name           string `toml:"name" json:"name"`
	A              string `toml:"a" json:"a"` // relay names from [[relay]]
	B              string `toml:"b" json:"b"`
	BudgetMBPerDay int    `toml:"budget_mb_per_day" json:"budget_mb_per_day"` // 0 = unlimited
	EverySecs      int    `toml:"every_secs" json:"every_secs"`
}

// LANConfig enables meshing with other nodes on the local network.
type LANConfig struct {
	Enabled     bool     `toml:"enabled" json:"enabled"`
	Port        int      `toml:"port" json:"port"`                 // TCP listen port (0 = random)
	Discovery   bool     `toml:"discovery" json:"discovery"`       // UDP multicast beacons
	StaticPeers []string `toml:"static_peers" json:"static_peers"` // host:port
}

// Config configures a Node.
type Config struct {
	DataDir  string `toml:"-" json:"-"`
	Name     string `toml:"name" json:"name"` // optional display name in announces
	Delegate bool   `toml:"delegate" json:"delegate"`
	// AnnounceMinutes: how often to announce (default 30).
	AnnounceMinutes int             `toml:"announce_minutes" json:"announce_minutes"`
	Relays          []RelayConfig   `toml:"relay" json:"relays"`
	Gateway         GatewayConfig   `toml:"gateway" json:"gateway"`
	Bridges         []BridgeConfig  `toml:"bridge" json:"bridges"`
	LAN             LANConfig       `toml:"lan" json:"lan"`
	LoRa            LoRaConfig      `toml:"lora" json:"lora"`
	Discovery       DiscoveryConfig `toml:"discovery" json:"discovery"`
	Media           MediaConfig     `toml:"media" json:"media"`
	Direct          DirectConfig    `toml:"direct" json:"direct"`
	// CallsFromAnyone: ring for calls from people who aren't contacts.
	CallsFromAnyone bool                             `toml:"calls_from_anyone" json:"calls_from_anyone"`
	Logf            func(format string, args ...any) `toml:"-" json:"-"`
}

// Event is pushed to subscribers.
type Event struct {
	Type    string     `json:"type"` // message | delivered | link | contact | call
	Message *Message   `json:"message,omitempty"`
	Call    *call.Info `json:"call,omitempty"`
	Info    string     `json:"info,omitempty"`
}

// Node is a running client.
type Node struct {
	cfg    Config
	id     *identity.Identity
	store  *store.SQLite
	db     *appDB
	router *router.Router
	start  time.Time

	mu      sync.Mutex
	subs    map[chan Event]struct{}
	links   map[string]string // link name → status
	relays  map[string]*relayLink
	bridges []*bridge
	lan     *lanLink
	direct  *directLink
	calls   *call.Manager
	loras   map[string]*hostLinkLink
	disc    *discovery // nil when relay discovery is off
	mw      *mediaWorker
}

// Open loads or creates the node's identity and databases in cfg.DataDir.
func Open(cfg Config) (*Node, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("node: DataDir required")
	}
	if cfg.AnnounceMinutes == 0 {
		cfg.AnnounceMinutes = 30
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	id, err := loadOrCreateIdentity(filepath.Join(cfg.DataDir, IdentityFile))
	if err != nil {
		return nil, err
	}
	st, err := store.OpenSQLite(filepath.Join(cfg.DataDir, "packets.db"))
	if err != nil {
		return nil, err
	}
	db, err := openAppDB(filepath.Join(cfg.DataDir, "app.db"))
	if err != nil {
		st.Close()
		return nil, err
	}
	n := &Node{
		cfg: cfg, id: id, store: st, db: db, start: time.Now(),
		subs: map[chan Event]struct{}{}, links: map[string]string{}, relays: map[string]*relayLink{}, loras: map[string]*hostLinkLink{},
	}
	var flags byte
	if cfg.Delegate {
		flags |= announce.FlagDelegate
	}
	n.router = router.New(router.Config{
		Store:          st,
		AnnounceEvery:  time.Duration(cfg.AnnounceMinutes) * time.Minute,
		AppData:        announce.AppData{Name: cfg.Name, Flags: flags},
		Contacts:       func(packet.Hash) []identity.Public { return n.contactKeys() },
		OnMessage:      n.onMessage,
		OnReceipt:      n.onReceipt,
		OnGroupMessage: n.onGroupMessage,
		OnAnnounce:     n.onAnnounce,
		Logf:           cfg.Logf,
	})
	n.mw = &mediaWorker{kick: make(chan struct{}, 1), busy: map[string]bool{}, waiters: map[string]chan []byte{}, serving: map[string]bool{}}
	if !cfg.Discovery.Off {
		n.disc = &discovery{n: n, cfg: cfg.Discovery, kick: make(chan struct{}, 1), auto: map[string]*autoRelay{}}
	}
	n.router.AddLocal(id)
	n.loadGroups()
	for _, k := range n.contactKeys() {
		n.router.AddKnown(k)
	}
	if !cfg.Direct.Off {
		n.direct = newDirectLink(n)
		n.router.AddLink(n.direct)
	}
	n.calls = n.newCalls()
	if cfg.LAN.Enabled {
		if n.lan, err = newLANLink(n, cfg.LAN); err != nil {
			n.Close()
			return nil, fmt.Errorf("lan: %w", err)
		}
		n.router.AddLink(n.lan)
	}
	return n, nil
}

func loadOrCreateIdentity(path string) (*identity.Identity, error) {
	if b, err := os.ReadFile(path); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, fmt.Errorf("node: identity file: %w", err)
		}
		return identity.FromSeed(seed)
	}
	id, err := identity.Generate()
	if err != nil {
		return nil, err
	}
	return id, os.WriteFile(path, []byte(hex.EncodeToString(id.Seed())), 0o600)
}

// Router exposes the router so platforms can add their own links.
func (n *Node) Router() *router.Router { return n.router }

// Identity returns this node's contact card.
func (n *Node) Identity() identity.Contact {
	return identity.Contact{Public: n.id.Public, Name: n.cfg.Name}
}

func (n *Node) contactKeys() []identity.Public {
	cs, err := n.db.contacts()
	if err != nil {
		return nil
	}
	out := make([]identity.Public, len(cs))
	for i, c := range cs {
		out[i] = c.pub
	}
	return out
}

func (n *Node) emit(e Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop
		}
	}
}

// Subscribe returns a channel of events; call the cancel func when done.
func (n *Node) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		delete(n.subs, ch)
		n.mu.Unlock()
	}
}

func (n *Node) setLinkStatus(name, status string) {
	n.mu.Lock()
	changed := n.links[name] != status
	n.links[name] = status
	n.mu.Unlock()
	if changed {
		n.emit(Event{Type: "link", Info: name + ": " + status})
	}
}

func (n *Node) onMessage(_ *identity.Identity, m *message.Received) {
	if m.ContentType == e2e.ContentGroup {
		// Private-group traffic: the sender is a member, not a new contact.
		n.onPrivateGroup(m)
		return
	}
	if m.ContentType == e2e.ContentCall {
		n.calls.OnSignal(m.From, m.Content)
		return
	}
	if m.ContentType == e2e.ContentDirect {
		// Direct-link signaling: only from saved contacts (no first contact).
		if !m.FirstContact && n.direct != nil {
			n.direct.onSignal(m.From, m.Content)
		}
		return
	}
	if m.FirstContact {
		// Remember first-contact senders (unnamed) so replies use Noise K.
		if err := n.db.putContact(m.From, "", time.Now()); err == nil {
			n.router.AddKnown(m.From)
			n.emit(Event{Type: "contact", Info: m.From.Hash().String()})
		}
	}
	msg := Message{
		ID: m.PacketID.String(), Peer: m.From.Hash().String(), Text: m.Text,
		Sent: m.Sent, Received: time.Now(),
	}
	if m.ContentType == e2e.ContentMedia {
		n.onMedia(&msg, m.Content)
		return
	}
	if err := n.db.putMessage(msg); err != nil {
		n.cfg.Logf("node: store message: %v", err)
	}
	n.emit(Event{Type: "message", Message: &msg})
}

func (n *Node) onReceipt(acked packet.ID) {
	if ok, _ := n.db.markDelivered(acked.String()); ok {
		n.emit(Event{Type: "delivered", Info: acked.String()})
	}
	// A member's copy of a private-group message: Info is the message id.
	if msg, ok := n.onGroupReceipt(acked); ok {
		n.emit(Event{Type: "delivered", Info: msg})
	}
}

// AddContact saves a contact from a meshmsg: URI. name overrides the URI's.
func (n *Node) AddContact(uri, name string) (Contact, error) {
	c, err := identity.ParseURI(uri)
	if err != nil {
		return Contact{}, err
	}
	if name != "" {
		c.Name = name
	}
	if err := n.db.putContact(c.Public, c.Name, time.Now()); err != nil {
		return Contact{}, err
	}
	n.router.AddKnown(c.Public)
	return Contact{Hash: c.Hash().String(), Name: c.Name, URI: c.URI(), pub: c.Public}, nil
}

// RenameContact sets a contact's display name.
func (n *Node) RenameContact(ref, name string) error {
	c, err := n.findContact(ref)
	if err != nil {
		return err
	}
	return n.db.putContact(c.pub, name, time.Now())
}

// RemoveContact deletes a contact (history is kept).
func (n *Node) RemoveContact(ref string) error {
	c, err := n.findContact(ref)
	if err != nil {
		return err
	}
	return n.db.deleteContact(c.pub.Hash())
}

// Contacts lists saved contacts.
func (n *Node) Contacts() ([]Contact, error) { return n.db.contacts() }

// findContact resolves a name, hash or hash prefix.
func (n *Node) findContact(ref string) (Contact, error) {
	cs, err := n.db.contacts()
	if err != nil {
		return Contact{}, err
	}
	var matches []Contact
	for _, c := range cs {
		if strings.EqualFold(c.Name, ref) || c.Hash == ref {
			return c, nil
		}
		if len(ref) >= 6 && strings.HasPrefix(c.Hash, strings.ToLower(ref)) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Contact{}, fmt.Errorf("no contact %q", ref)
	}
	return Contact{}, fmt.Errorf("%q matches %d contacts", ref, len(matches))
}

// Send sends text to a contact (name, hash or hash prefix).
// A group (hash, name or "#channel") works too.
func (n *Node) Send(ref, text string) (Message, error) {
	c, err := n.findContact(ref)
	if err != nil {
		if _, gerr := n.findGroup(ref); gerr == nil {
			return n.SendGroup(ref, text)
		}
		return Message{}, err
	}
	// Noise K needs the recipient to know us; if we have never received
	// anything from them we can't be sure, so first messages use Noise X.
	known := n.heardFrom(c.Hash)
	id, err := n.router.SendText(n.id, c.pub, text, known, false)
	if err != nil {
		return Message{}, err
	}
	m := Message{ID: id.String(), Peer: c.Hash, Outgoing: true, Text: text, Sent: time.Now(), Received: time.Now()}
	if err := n.db.putMessage(m); err != nil {
		return m, err
	}
	return m, nil
}

func (n *Node) heardFrom(peerHash string) bool {
	h, err := packet.ParseHash(peerHash)
	if err != nil {
		return false
	}
	msgs, err := n.db.messages(&h, 1000)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if !m.Outgoing || m.Delivered {
			return true
		}
	}
	return false
}

// Messages returns history with a contact ("" = all).
func (n *Node) Messages(ref string, limit int) ([]Message, error) {
	var peer *packet.Hash
	if ref != "" {
		c, g, err := n.resolveChat(ref)
		if err != nil {
			return nil, err
		}
		var h packet.Hash
		if c != nil {
			h = c.pub.Hash()
		} else {
			h = g.id
		}
		peer = &h
	}
	ms, err := n.db.messages(peer, limit)
	for i := range ms {
		n.nameGroupSender(&ms[i])
	}
	return ms, err
}

// Status is a snapshot for UIs.
type Status struct {
	Hash     string            `json:"hash"`
	URI      string            `json:"uri"`
	Uptime   string            `json:"uptime"`
	Links    map[string]string `json:"links"`
	Routes   int               `json:"routes"`
	Stored   int               `json:"stored"`
	Router   router.Stats      `json:"router"`
	Gateway  bool              `json:"gateway"`
	Bridges  []string          `json:"bridges"`
	Contacts int               `json:"contacts"`
	Groups   int               `json:"groups"`
	LoRa     []LoRaState       `json:"lora,omitempty"`
}

// Status returns a snapshot.
func (n *Node) Status() Status {
	n.mu.Lock()
	links := make(map[string]string, len(n.links))
	for k, v := range n.links {
		links[k] = v
	}
	var br []string
	for _, b := range n.bridges {
		br = append(br, b.status())
	}
	n.mu.Unlock()
	stored, _ := n.store.Count()
	cs, _ := n.db.contacts()
	gs, _ := n.Groups()
	return Status{
		Hash: n.id.Hash().String(), URI: n.Identity().URI(),
		Uptime: time.Since(n.start).Round(time.Second).String(),
		Links:  links, Routes: len(n.router.Routes()), Stored: stored,
		Router: n.router.Stats(), Gateway: n.cfg.Gateway.Enabled, Bridges: br, Contacts: len(cs), Groups: len(gs),
		LoRa: n.LoRa(),
	}
}

// Run starts all links and periodic work; it returns when ctx is cancelled.
func (n *Node) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, rc := range n.cfg.Relays {
		rl, err := newRelayLink(n, rc)
		if err != nil {
			return fmt.Errorf("relay %s: %w", rc.Name, err)
		}
		n.mu.Lock()
		n.relays[rc.Name] = rl
		n.mu.Unlock()
		n.router.AddLink(rl)
		wg.Add(1)
		go func() { defer wg.Done(); rl.run(ctx) }()
	}
	for _, bc := range n.cfg.Bridges {
		b, err := newBridge(n, bc)
		if err != nil {
			return fmt.Errorf("bridge %s: %w", bc.Name, err)
		}
		n.mu.Lock()
		n.bridges = append(n.bridges, b)
		n.mu.Unlock()
		wg.Add(1)
		go func() { defer wg.Done(); b.run(ctx) }()
	}
	if n.lan != nil {
		wg.Add(1)
		go func() { defer wg.Done(); n.lan.run(ctx) }()
	}
	if n.disc != nil {
		wg.Add(1)
		go func() { defer wg.Done(); n.disc.run(ctx) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); n.runMedia(ctx) }()
	if n.cfg.LoRa.Enabled && n.cfg.LoRa.Port != "" {
		port := n.cfg.LoRa.Port
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.AttachLoRa(ctx, "lora", func() (io.ReadWriteCloser, error) { return OpenSerial(port) })
		}()
	}

	n.router.Announce()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-t.C:
			n.router.Tick()
		}
	}
}

// Close releases databases (after Run returns).
func (n *Node) Close() error {
	n.calls.Close()
	if n.direct != nil {
		n.direct.close()
	}
	if n.lan != nil {
		n.lan.ln.Close() // free a fixed port now, for a restart in this process
	}
	return errors.Join(n.store.Close(), n.db.close())
}

func (n *Node) cursor(key string) int64 {
	v, ok := n.db.get("cursor:" + key)
	if !ok {
		return 0
	}
	c, _ := strconv.ParseInt(v, 10, 64)
	return c
}

func (n *Node) setCursor(key string, c int64) {
	n.db.set("cursor:"+key, strconv.FormatInt(c, 10))
}

// LANAddr returns the LAN listener address, if enabled and running.
func (n *Node) LANAddr() string {
	if n.lan == nil {
		return ""
	}
	return n.lan.addr()
}

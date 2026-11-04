package router

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/store"
)

// testNet delivers packets through a FIFO queue (no recursion).
type testNet struct {
	t       *testing.T
	nodes   map[string]*node
	queue   []func()
	txCount map[string]int // broadcast transmissions per node
}

type node struct {
	name  string
	r     *Router
	id    *identity.Identity
	inbox []*message.Received
	acks  []packet.ID
	store store.Store
}

func newNet(t *testing.T) *testNet {
	return &testNet{t: t, nodes: map[string]*node{}, txCount: map[string]int{}}
}

func (n *testNet) add(name string, contacts ...string) *node {
	id, _ := identity.Generate()
	nd := &node{name: name, id: id, store: store.NewMemory()}
	nd.r = New(Config{
		Store: nd.store,
		Contacts: func(packet.Hash) []identity.Public {
			var out []identity.Public
			for _, c := range contacts {
				out = append(out, n.nodes[c].id.Public)
			}
			return out
		},
		OnMessage: func(_ *identity.Identity, m *message.Received) { nd.inbox = append(nd.inbox, m) },
		OnReceipt: func(id packet.ID) { nd.acks = append(nd.acks, id) },
	})
	nd.r.AddLocal(id)
	n.nodes[name] = nd
	return nd
}

type p2p struct {
	n     *testNet
	owner string
}

func (l *p2p) Name() string   { return "ble" }
func (l *p2p) Kind() LinkKind { return PointToPoint }
func (l *p2p) MTU() int       { return 4096 }
func (l *p2p) Send(peer PeerID, raw []byte) {
	dst := l.n.nodes[string(peer)]
	from := PeerID(l.owner)
	l.n.queue = append(l.n.queue, func() { dst.r.Inbound("ble", from, raw) })
}

type radio struct {
	n         *testNet
	owner     string
	neighbors []string
}

func (l *radio) Name() string   { return "lora" }
func (l *radio) Kind() LinkKind { return Broadcast }
func (l *radio) MTU() int       { return packet.LoRaMTU }
func (l *radio) Send(_ PeerID, raw []byte) {
	l.n.txCount[l.owner]++
	for _, nb := range l.neighbors {
		dst := l.n.nodes[nb]
		l.n.queue = append(l.n.queue, func() { dst.r.Inbound("lora", "", raw) })
	}
}

func (n *testNet) bleLinks(names ...string) {
	for _, nm := range names {
		n.nodes[nm].r.AddLink(&p2p{n: n, owner: nm})
	}
}

func (n *testNet) connect(a, b string) {
	n.nodes[a].r.PeerUp("ble", PeerID(b))
	n.nodes[b].r.PeerUp("ble", PeerID(a))
}

func (n *testNet) disconnect(a, b string) {
	n.nodes[a].r.PeerDown("ble", PeerID(b))
	n.nodes[b].r.PeerDown("ble", PeerID(a))
}

func (n *testNet) run() {
	for i := 0; len(n.queue) > 0; i++ {
		if i > 100000 {
			n.t.Fatal("network did not settle")
		}
		f := n.queue[0]
		n.queue = n.queue[1:]
		f()
	}
}

func dataCount(t *testing.T, s store.Store) int {
	items, _ := s.Since(0, 0, store.Filter{Types: []packet.Type{packet.TypeData}})
	return len(items)
}

func TestChainDeliveryAndReceiptPurge(t *testing.T) {
	n := newNet(t)
	a, b, c := n.add("A", "C"), n.add("B"), n.add("C", "A")
	n.bleLinks("A", "B", "C")
	n.connect("A", "B")
	n.connect("B", "C")
	// Announces teach B the identities so it can verify receipts.
	a.r.Announce()
	c.r.Announce()
	n.run()

	id, err := a.r.SendText(a.id, c.id.Public, "سلام از A", true, false)
	if err != nil {
		t.Fatal(err)
	}
	n.run()
	if len(c.inbox) != 1 || c.inbox[0].Text != "سلام از A" || c.inbox[0].From != a.id.Public {
		t.Fatalf("C inbox: %+v", c.inbox)
	}
	if len(a.acks) != 1 || a.acks[0] != id {
		t.Fatalf("A did not get receipt: %v", a.acks)
	}
	if dataCount(t, b.store) != 0 || dataCount(t, a.store) != 0 {
		t.Fatal("stored copies not purged by receipt")
	}
	// Duplicate delivery after receipt must be ignored.
	n.run()
	if len(c.inbox) != 1 {
		t.Fatal("duplicate delivery")
	}
}

func TestStoreAndForwardAcrossTime(t *testing.T) {
	n := newNet(t)
	a, _, c := n.add("A", "C"), n.add("B"), n.add("C", "A")
	n.bleLinks("A", "B", "C")

	// A is alone when sending.
	if _, err := a.r.SendText(a.id, c.id.Public, "later", true, false); err != nil {
		t.Fatal(err)
	}
	n.run()
	// B walks by A, then walks to C.
	n.connect("A", "B")
	n.run()
	n.disconnect("A", "B")
	n.connect("B", "C")
	n.run()
	if len(c.inbox) != 1 || c.inbox[0].Text != "later" {
		t.Fatalf("C inbox: %+v", c.inbox)
	}
	// The receipt reaches A when B meets A again.
	n.disconnect("B", "C")
	n.connect("A", "B")
	n.run()
	if len(a.acks) != 1 {
		t.Fatal("receipt not carried back")
	}
}

func TestFirstContact(t *testing.T) {
	n := newNet(t)
	a, c := n.add("A"), n.add("C") // C does not know A
	n.bleLinks("A", "C")
	n.connect("A", "C")
	if _, err := a.r.SendText(a.id, c.id.Public, "hello stranger", false, false); err != nil {
		t.Fatal(err)
	}
	n.run()
	if len(c.inbox) != 1 || !c.inbox[0].FirstContact || c.inbox[0].From != a.id.Public {
		t.Fatalf("first contact failed: %+v", c.inbox)
	}
}

func TestLoRaManagedFlood(t *testing.T) {
	// Line topology: N0 - N1 - N2 - N3 - N4, each hears only its neighbours.
	n := newNet(t)
	names := []string{"N0", "N1", "N2", "N3", "N4"}
	n.add("N0", "N4")
	for _, nm := range names[1:4] {
		n.add(nm)
	}
	n.add("N4", "N0")
	for i, nm := range names {
		var nb []string
		if i > 0 {
			nb = append(nb, names[i-1])
		}
		if i < len(names)-1 {
			nb = append(nb, names[i+1])
		}
		n.nodes[nm].r.AddLink(&radio{n: n, owner: nm, neighbors: nb})
	}
	src, dst := n.nodes["N0"], n.nodes["N4"]
	if _, err := src.r.SendText(src.id, dst.id.Public, "across the air", true, true); err != nil {
		t.Fatal(err)
	}
	n.run()
	if len(dst.inbox) != 1 {
		t.Fatal("not delivered over LoRa")
	}
	// Each node transmits the DATA at most once; receipt too.
	for _, nm := range names {
		if n.txCount[nm] > 2 {
			t.Fatalf("%s transmitted %d times (flood not deduplicated)", nm, n.txCount[nm])
		}
	}
	if len(src.acks) != 1 {
		t.Fatal("receipt not received over LoRa")
	}
}

func TestHopLimit(t *testing.T) {
	n := newNet(t)
	n.add("A", "Z")
	chain := []string{"A"}
	for i := 0; i < 12; i++ {
		nm := string(rune('a' + i))
		n.add(nm)
		chain = append(chain, nm)
	}
	n.add("Z", "A")
	chain = append(chain, "Z")
	n.bleLinks(chain...)
	for i := 0; i+1 < len(chain); i++ {
		n.connect(chain[i], chain[i+1])
	}
	a, z := n.nodes["A"], n.nodes["Z"]
	a.r.SendText(a.id, z.id.Public, "too far", true, false)
	n.run()
	if len(z.inbox) != 0 {
		t.Fatal("packet exceeded max hops")
	}
}

func TestTickExpires(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := store.NewMemory()
	id, _ := identity.Generate()
	peer, _ := identity.Generate()
	r := New(Config{Store: s, Now: func() time.Time { return now }, DataTTL: time.Hour})
	r.AddLocal(id)
	r.SendText(id, peer.Public, "x", true, false)
	if dataCount(t, s) != 1 {
		t.Fatal("not stored")
	}
	now = now.Add(2 * time.Hour)
	r.Tick()
	if dataCount(t, s) != 0 {
		t.Fatal("not expired")
	}
}

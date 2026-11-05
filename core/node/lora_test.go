package node_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// fakeAir connects fake LoRa nodes: every packet a node's host hands it is
// heard by every other node once (by packet key), which passes it to its
// own host — the behaviour of the firmware node app, without radio timing.
type fakeAir struct {
	mu    sync.Mutex
	nodes []*fakeLoRa
}

type fakeLoRa struct {
	air  *fakeAir
	id   *identity.Identity
	mu   sync.Mutex
	conn net.Conn
	cfg  hostlink.NodeConfig
	seen map[packet.Key]bool
	wmu  sync.Mutex
	fail int // fail this many opens first
	hold [][]byte
}

func (a *fakeAir) add(t *testing.T) *fakeLoRa {
	id, _ := identity.Generate()
	cfg := hostlink.DefaultNodeConfig()
	cfg.Region, cfg.Preset = 3, hostlink.PresetFast // no duty cycle: fast tests
	f := &fakeLoRa{air: a, id: id, cfg: cfg, seen: map[packet.Key]bool{}}
	a.mu.Lock()
	a.nodes = append(a.nodes, f)
	a.mu.Unlock()
	return f
}

func (f *fakeLoRa) opener() node.PortOpener {
	return func() (io.ReadWriteCloser, error) {
		f.mu.Lock()
		if f.fail > 0 {
			f.fail--
			f.mu.Unlock()
			return nil, errors.New("no such device")
		}
		f.mu.Unlock()
		host, dev := net.Pipe()
		f.mu.Lock()
		f.conn = dev
		f.mu.Unlock()
		go f.serve(dev)
		return host, nil
	}
}

func (f *fakeLoRa) send(conn net.Conn, kind byte, payload []byte) {
	b, _ := hostlink.Encode(kind, payload)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn.Write(b)
}

func (f *fakeLoRa) event(conn net.Conn, ev, req byte, data []byte) {
	f.send(conn, hostlink.KindEvent, append([]byte{ev, req}, data...))
}

func (f *fakeLoRa) serve(conn net.Conn) {
	var dec hostlink.Decoder
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			f.mu.Lock()
			if f.conn == conn {
				f.conn = nil
			}
			f.mu.Unlock()
			return
		}
		for _, fr := range dec.Feed(buf[:n]) {
			switch fr.Kind {
			case hostlink.KindPacket:
				p, err := packet.Decode(fr.Payload)
				if err != nil {
					continue
				}
				f.mu.Lock()
				f.seen[p.Key()] = true
				f.mu.Unlock()
				go f.air.broadcast(f, fr.Payload)
			case hostlink.KindCommand:
				m, _ := hostlink.ParseMessage(fr.Payload)
				f.command(conn, m)
			}
		}
	}
}

func (f *fakeLoRa) command(conn net.Conn, m hostlink.Message) {
	f.mu.Lock()
	cfg := f.cfg
	f.mu.Unlock()
	switch m.Op {
	case hostlink.OpHello, hostlink.OpGetStatus:
		st := hostlink.Status{FirmwareVersion: 1, Role: cfg.Role, Region: cfg.Region, Preset: cfg.Preset,
			FreqHz: hostlink.Regions[cfg.Region].FreqHz, Neighbours: uint8(len(f.air.nodes) - 1)}
		f.event(conn, hostlink.EvStatus, m.Req, st.Encode())
		if m.Op == hostlink.OpHello {
			f.mu.Lock()
			held := f.hold
			f.hold = nil
			f.mu.Unlock()
			for _, p := range held {
				f.send(conn, hostlink.KindPacket, p)
			}
		}
	case hostlink.OpGetConfig:
		f.event(conn, hostlink.EvConfig, m.Req, cfg.EncodeTLV())
	case hostlink.OpGetIdentity:
		f.event(conn, hostlink.EvIdentity, m.Req, f.id.Public.Bytes())
	case hostlink.OpSetConfig:
		status := hostlink.AckOK
		f.mu.Lock()
		if err := f.cfg.ApplyTLV(m.Data); err != nil {
			status = hostlink.AckBadArgs
		}
		f.mu.Unlock()
		f.event(conn, hostlink.EvAck, m.Req, []byte{m.Op, status})
	default:
		f.event(conn, hostlink.EvAck, m.Req, []byte{m.Op, hostlink.AckUnknownOp})
	}
}

func (a *fakeAir) broadcast(from *fakeLoRa, raw []byte) {
	p, err := packet.Decode(raw)
	if err != nil {
		return
	}
	a.mu.Lock()
	nodes := append([]*fakeLoRa(nil), a.nodes...)
	a.mu.Unlock()
	for _, o := range nodes {
		if o == from {
			continue
		}
		o.mu.Lock()
		dup := o.seen[p.Key()]
		o.seen[p.Key()] = true
		conn := o.conn
		if !dup && conn == nil {
			o.hold = append(o.hold, raw) // stored until a host attaches
		}
		o.mu.Unlock()
		if !dup && conn != nil {
			o.send(conn, hostlink.KindPacket, raw)
		}
	}
}

func attach(t *testing.T, n *node.Node, name string, f *fakeLoRa) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.AttachLoRa(ctx, name, f.opener()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitFor(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Two desktops with no internet and no LAN, each with a LoRa node on USB:
// text crosses PC → node → air → node → PC and the receipt comes back.
func TestChatOverLoRa(t *testing.T) {
	node.SetLoRaRetry(100 * time.Millisecond)
	air := &fakeAir{}
	fa, fs := air.add(t), air.add(t)
	ali := startNode(t, node.Config{Name: "Ali"})
	sara := startNode(t, node.Config{Name: "Sara"})
	attach(t, ali, "lora", fa)
	attach(t, sara, "lora", fs)
	befriend(t, ali, sara, "ali", "sara")
	waitFor(t, "connected", 5*time.Second, func() bool {
		a, s := ali.LoRa(), sara.LoRa()
		return len(a) == 1 && len(s) == 1 && a[0].Config != nil && s[0].Config != nil
	})

	saraEv, c1 := sara.Subscribe()
	defer c1()
	aliEv, c2 := ali.Subscribe()
	defer c2()
	if _, err := ali.Send("sara", "سلام از رادیو"); err != nil {
		t.Fatal(err)
	}
	e := waitEvent(t, saraEv, "message", 20*time.Second)
	if e.Message.Text != "سلام از رادیو" {
		t.Fatalf("got %+v", e.Message)
	}
	waitEvent(t, aliEv, "delivered", 20*time.Second)

	st := ali.LoRa()[0]
	if !st.Connected || st.Node != fa.id.Hash().String() || st.Status == nil || st.Status.Neighbours != 1 || st.Sent == 0 {
		t.Fatalf("state %+v", st)
	}
	if s := ali.Status(); len(s.LoRa) != 1 || s.Links["lora"] == "" {
		t.Fatalf("status %+v", s)
	}
}

func TestLoRaConfigureAndReconnect(t *testing.T) {
	node.SetLoRaRetry(100 * time.Millisecond)
	air := &fakeAir{}
	f := air.add(t)
	f.fail = 2 // port missing at first (board not plugged in yet)
	n := startNode(t, node.Config{})
	attach(t, n, "lora", f)
	waitFor(t, "connected", 5*time.Second, func() bool {
		s := n.LoRa()
		return len(s) == 1 && s[0].Connected && s[0].Config != nil
	})

	name, preset, region := "roof", "LONG", "US_915"
	if err := n.LoRaConfigure("lora", node.LoRaPatch{Name: &name, Preset: &preset, Region: &region}); err != nil {
		t.Fatal(err)
	}
	c := n.LoRa()[0].Config
	if c.Name != "roof" || c.Preset != hostlink.PresetLong {
		t.Fatalf("config %+v", c)
	}
	bad := "MARS_1"
	if err := n.LoRaConfigure("lora", node.LoRaPatch{Region: &bad}); err == nil {
		t.Fatal("unknown region accepted")
	}
	var power int8 = 40 // over the region cap: the node refuses
	if err := n.LoRaConfigure("lora", node.LoRaPatch{TxDBm: &power}); err == nil {
		t.Fatal("node accepted 40 dBm")
	}
	if err := n.LoRaConfigure("nope", node.LoRaPatch{Name: &name}); err == nil {
		t.Fatal("unknown link accepted")
	}
	if err := n.LoRaConfigure("lora", node.LoRaPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}

	// Unplug: the link notices, reports it, and reconnects by itself.
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	conn.Close()
	waitFor(t, "disconnect", 5*time.Second, func() bool { return !n.LoRa()[0].Connected })
	waitFor(t, "reconnect", 5*time.Second, func() bool { return n.LoRa()[0].Connected })
}

// Messages heard while the PC was away are stored by the node and arrive
// when it reattaches; the PC's own backlog goes out when its node returns.
func TestLoRaStoreAndForward(t *testing.T) {
	node.SetLoRaRetry(100 * time.Millisecond)
	air := &fakeAir{}
	fa, fs := air.add(t), air.add(t)
	ali := startNode(t, node.Config{Name: "Ali"})
	sara := startNode(t, node.Config{Name: "Sara"})
	befriend(t, ali, sara, "ali", "sara")
	attach(t, ali, "lora", fa)
	waitFor(t, "ali connected", 5*time.Second, func() bool { return len(ali.LoRa()) == 1 && ali.LoRa()[0].Connected })

	if _, err := ali.Send("sara", "while you were away"); err != nil {
		t.Fatal(err)
	}
	// Sara's node hears it with no PC attached and holds it.
	waitFor(t, "held", 10*time.Second, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.hold) > 0
	})
	saraEv, c := sara.Subscribe()
	defer c()
	attach(t, sara, "lora", fs)
	e := waitEvent(t, saraEv, "message", 20*time.Second)
	if e.Message.Text != "while you were away" {
		t.Fatalf("got %q", e.Message.Text)
	}
}

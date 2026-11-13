package direct

// An in-memory internet for the connectivity tests (connectivity.md §6):
// networks nested behind NATs of every kind, hosts with several
// interfaces, the same private subnet in unrelated networks, and STUN
// reflectors. Packets are counted per network, so a test can assert that
// session traffic stayed local and that no relay forwarded any of it.

import (
	"errors"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/stun/v3"
)

type natKind int

const (
	natIndep     natKind = iota // endpoint-independent mapping, port-restricted filtering
	natSymSeq                   // new port per destination, +1 each time
	natSymRandom                // new random port per destination
	natBlockUDP                 // drops all UDP going out
)

type world struct {
	mu   sync.Mutex
	lans []*lan
}

type lan struct {
	w       *world
	name    string
	parent  *lan
	nat     *natBox
	conns   map[netip.AddrPort]*simConn
	packets atomic.Int64 // datagrams delivered inside or through this network
}

type natBox struct {
	kind  natKind
	ext   netip.Addr // address in the parent network
	inner *lan
	next  uint16
	maps  map[mapKey]uint16
	rev   map[uint16]*mapping
}

type mapKey struct{ in, dst netip.AddrPort }

type mapping struct {
	in      netip.AddrPort
	allowed map[netip.AddrPort]bool
}

func newWorld() *world { return &world{} }

func (w *world) lan(name string, parent *lan, kind natKind, ext string) *lan {
	l := &lan{w: w, name: name, parent: parent, conns: map[netip.AddrPort]*simConn{}}
	if parent != nil {
		l.nat = &natBox{kind: kind, ext: netip.MustParseAddr(ext), inner: l, next: 20000,
			maps: map[mapKey]uint16{}, rev: map[uint16]*mapping{}}
	}
	w.lans = append(w.lans, l)
	return l
}

// rebind drops a NAT's mappings (a CGNAT timeout or a new public address).
func (l *lan) rebind() {
	l.w.mu.Lock()
	defer l.w.mu.Unlock()
	l.nat.maps, l.nat.rev = map[mapKey]uint16{}, map[uint16]*mapping{}
	l.nat.next += 1000
}

// host is a device with one address per network it is on.
type host struct {
	w     *world
	ifs   []iface
	ports uint16
}

type iface struct {
	l  *lan
	ip netip.Addr
}

func (w *world) host(ifs ...any) *host {
	h := &host{w: w, ports: 40000}
	for i := 0; i < len(ifs); i += 2 {
		h.ifs = append(h.ifs, iface{ifs[i].(*lan), netip.MustParseAddr(ifs[i+1].(string))})
	}
	return h
}

func (h *host) Addrs() ([]netip.Addr, error) {
	var out []netip.Addr
	for _, f := range h.ifs {
		out = append(out, f.ip)
	}
	return out, nil
}

func (h *host) ListenUDP(port int) (net.PacketConn, error) {
	h.w.mu.Lock()
	defer h.w.mu.Unlock()
	if port == 0 {
		h.ports++
		port = int(h.ports)
	}
	c := &simConn{h: h, port: uint16(port), in: make(chan pkt, 4096), done: make(chan struct{})}
	for _, f := range h.ifs {
		f.l.conns[netip.AddrPortFrom(f.ip, uint16(port))] = c
	}
	return c, nil
}

type pkt struct {
	b    []byte
	from netip.AddrPort
}

type simConn struct {
	h    *host
	port uint16
	in   chan pkt
	done chan struct{}
	once sync.Once
}

func (c *simConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-c.in:
		return copy(b, p.b), udpAddr(p.from), nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *simConn) WriteTo(b []byte, to net.Addr) (int, error) {
	select {
	case <-c.done:
		return 0, net.ErrClosed
	default:
	}
	dst := addrPort(to)
	w := c.h.w
	w.mu.Lock()
	defer w.mu.Unlock()
	f := c.h.route(dst)
	w.deliver(f.l, netip.AddrPortFrom(f.ip, c.port), dst, append([]byte(nil), b...), 0)
	return len(b), nil
}

// route picks the interface toward dst: one whose network holds dst (or a
// NAT with that outside address), else the first (the default route).
func (h *host) route(dst netip.AddrPort) iface {
	for _, f := range h.ifs {
		if _, ok := f.l.conns[dst]; ok {
			return f
		}
		for _, l := range h.w.lans {
			if l.parent == f.l && l.nat.ext == dst.Addr() {
				return f
			}
		}
	}
	return h.ifs[0]
}

func (w *world) deliver(l *lan, src, dst netip.AddrPort, b []byte, hops int) {
	if hops > 8 {
		return
	}
	if c, ok := l.conns[dst]; ok {
		l.packets.Add(1)
		select {
		case c.in <- pkt{b, src}:
		default:
		}
		return
	}
	// A NAT inside this network, addressed from outside.
	for _, in := range w.lans {
		if in.parent == l && in.nat.ext == dst.Addr() {
			l.packets.Add(1)
			in.nat.inbound(w, src, dst, b, hops)
			return
		}
	}
	// Out through this network's NAT.
	if l.nat == nil || l.nat.kind == natBlockUDP {
		return
	}
	l.packets.Add(1)
	out := netip.AddrPortFrom(l.nat.ext, l.nat.port(src, dst))
	if dst.Addr() == l.nat.ext { // hairpin
		l.nat.inbound(w, out, dst, b, hops)
		return
	}
	w.deliver(l.parent, out, dst, b, hops+1)
}

func (n *natBox) port(in, dst netip.AddrPort) uint16 {
	k := mapKey{in: in}
	if n.kind != natIndep {
		k.dst = dst
	}
	p, ok := n.maps[k]
	if !ok {
		switch n.kind {
		case natSymRandom:
			for {
				p = uint16(1024 + rand.IntN(64000))
				if _, used := n.rev[p]; !used {
					break
				}
			}
		default:
			n.next++
			p = n.next
		}
		n.maps[k] = p
		n.rev[p] = &mapping{in: in, allowed: map[netip.AddrPort]bool{}}
	}
	n.rev[p].allowed[dst] = true
	return p
}

func (n *natBox) inbound(w *world, src, dst netip.AddrPort, b []byte, hops int) {
	m := n.rev[dst.Port()]
	if m == nil || !m.allowed[src] { // port-restricted: only who we sent to
		return
	}
	w.deliver(n.inner, src, m.in, b, hops+1)
}

func (c *simConn) Close() error {
	c.once.Do(func() {
		close(c.done)
		w := c.h.w
		w.mu.Lock()
		for _, f := range c.h.ifs {
			delete(f.l.conns, netip.AddrPortFrom(f.ip, c.port))
		}
		w.mu.Unlock()
	})
	return nil
}

func (c *simConn) LocalAddr() net.Addr {
	return udpAddr(netip.AddrPortFrom(netip.IPv4Unspecified(), c.port))
}
func (c *simConn) SetDeadline(time.Time) error      { return errors.New("not supported") }
func (c *simConn) SetReadDeadline(time.Time) error  { return errors.New("not supported") }
func (c *simConn) SetWriteDeadline(time.Time) error { return errors.New("not supported") }

// reflector answers STUN binding requests (a relay's UDP 3478).
func (w *world) reflector(l *lan, addr string) string {
	h := w.host(l, addr)
	pc, _ := h.ListenUDP(3478)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if req.Decode() != nil || req.Type != stun.BindingRequest {
				continue
			}
			u := from.(*net.UDPAddr)
			resp, err := stun.Build(stun.NewTransactionIDSetter(req.TransactionID), stun.BindingSuccess,
				&stun.XORMappedAddress{IP: u.IP, Port: u.Port}, stun.Fingerprint)
			if err == nil {
				pc.WriteTo(resp.Raw, from)
			}
		}
	}()
	return addr + ":3478"
}

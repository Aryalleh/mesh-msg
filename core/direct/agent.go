package direct

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/stun/v3"
	"golang.org/x/crypto/hkdf"
)

// Config configures an agent.
type Config struct {
	Net Net // nil: the real network
	// Reflectors are STUN servers ("host:port"), normally every relay's
	// UDP 3478 and 3479. Two or more on different ports or hosts let the
	// agent tell its NAT type.
	Reflectors []string
	// PortMapping asks the router for a port (PCP, NAT-PMP, UPnP).
	PortMapping bool
	// TURN, if set, allocates a relayed candidate (only relay admins get
	// credentials). It is chosen only when nothing else works.
	TURN *TURNServer
	// CheckTimeout bounds the normal checks (default 10 s); symmetric-NAT
	// methods may add up to 20 s more.
	CheckTimeout time.Duration
	// SymmetricTimeout bounds the symmetric-NAT methods (default 20 s).
	SymmetricTimeout time.Duration
	Logf             func(format string, args ...any)
}

// TURNServer is a relay's TURN service with credentials for this session.
type TURNServer struct {
	Addr               string // host:port
	Username, Password string
}

// Agent runs one side of one direct session.
type Agent struct {
	cfg         Config
	sid         [16]byte
	macKey      []byte
	key         [32]byte
	controlling bool

	mu      sync.Mutex
	socks   []*sock
	local   []Candidate
	nat     NATInfo
	remote  Offer
	pairs   map[pairKey]*pair
	byTx    map[[8]byte]*pair
	stunTx  map[[12]byte]chan netip.AddrPort
	won     chan struct{} // a pair succeeded
	link    *Link
	release []func() // port mappings, TURN
	closed  bool
}

type sock struct {
	pc      net.PacketConn
	relayed bool
	extra   bool // birthday socket, closed after the choice
}

type pairKey struct {
	s      *sock
	remote netip.AddrPort
}

type pair struct {
	s        *sock
	remote   Candidate
	tx       [8]byte
	sent     time.Time
	next     time.Time
	interval time.Duration
	ok       bool
	rtt      time.Duration
	seenAs   netip.AddrPort // our address as the peer saw it
}

// ErrNoPath: no authenticated check succeeded.
var ErrNoPath = errors.New("direct: no direct path")

// NewAgent opens the session socket. sid and key are shared with the peer
// through the end-to-end encrypted signaling; controlling is the side that
// started.
func NewAgent(cfg Config, sid [16]byte, key [32]byte, controlling bool) (*Agent, error) {
	if cfg.Net == nil {
		cfg.Net = OSNet{}
	}
	if cfg.CheckTimeout == 0 {
		cfg.CheckTimeout = 10 * time.Second
	}
	if cfg.SymmetricTimeout == 0 {
		cfg.SymmetricTimeout = 20 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	a := &Agent{
		cfg: cfg, sid: sid, key: key, controlling: controlling,
		pairs: map[pairKey]*pair{}, byTx: map[[8]byte]*pair{}, stunTx: map[[12]byte]chan netip.AddrPort{},
		won: make(chan struct{}, 1),
	}
	a.macKey = derive(key, "mava direct probe")
	pc, err := cfg.Net.ListenUDP(0)
	if err != nil {
		return nil, err
	}
	a.addSock(&sock{pc: pc})
	return a, nil
}

func derive(key [32]byte, label string) []byte {
	out := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, key[:], nil, []byte(label)), out)
	return out
}

func (a *Agent) addSock(s *sock) {
	a.mu.Lock()
	a.socks = append(a.socks, s)
	a.mu.Unlock()
	go a.readLoop(s)
}

func (a *Agent) mainSock() *sock { return a.socks[0] }

// Close ends the session and releases everything.
func (a *Agent) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	socks, rel := a.socks, a.release
	a.mu.Unlock()
	for _, f := range rel {
		f()
	}
	for _, s := range socks {
		s.pc.Close()
	}
}

// ---- gathering ----

// Gather collects this side's candidates and NAT type (connectivity.md §3,
// §3a). It takes a few seconds at most.
func (a *Agent) Gather(ctx context.Context) Offer {
	port := addrPort(a.mainSock().pc.LocalAddr()).Port()
	addrs, _ := a.cfg.Net.Addrs()
	var cands []Candidate
	for _, ip := range addrs {
		cands = append(cands, Candidate{Kind: Host, Addr: netip.AddrPortFrom(ip, port), Priority: priority(Host, ip)})
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	add := func(c ...Candidate) {
		mu.Lock()
		cands = append(cands, c...)
		mu.Unlock()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		refl, nat := a.reflect(ctx, addrs)
		a.nat = nat
		add(refl...)
	}()
	if a.cfg.PortMapping {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c, release, err := mapPort(ctx, a.cfg.Net, port); err == nil {
				add(c)
				a.mu.Lock()
				a.release = append(a.release, release)
				a.mu.Unlock()
			} else {
				a.cfg.Logf("direct: port mapping: %v", err)
			}
		}()
	}
	if a.cfg.TURN != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c, err := a.allocateTURN(ctx); err == nil {
				add(c)
			} else {
				a.cfg.Logf("direct: TURN: %v", err)
			}
		}()
	}
	wg.Wait()
	cands = dedupe(cands)
	a.mu.Lock()
	a.local = cands
	a.mu.Unlock()
	return Offer{NAT: a.nat, Candidates: cands}
}

func dedupe(cs []Candidate) []Candidate {
	seen := map[netip.AddrPort]bool{}
	var out []Candidate
	for _, c := range cs {
		if !seen[c.Addr] {
			seen[c.Addr] = true
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// reflect asks every reflector from the session socket and classifies the
// NAT from the mappings seen.
func (a *Agent) reflect(ctx context.Context, hosts []netip.Addr) ([]Candidate, NATInfo) {
	type seen struct {
		dst, mapped netip.AddrPort
		order       int
	}
	var mu sync.Mutex
	var got []seen
	var wg sync.WaitGroup
	for i, r := range a.cfg.Reflectors {
		dst, err := resolve(ctx, r)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Spread the requests a little so a sequential NAT shows its order.
			time.Sleep(time.Duration(i) * 30 * time.Millisecond)
			if m, err := a.binding(ctx, a.mainSock(), dst); err == nil {
				mu.Lock()
				got = append(got, seen{dst, m, i})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(got) == 0 {
		if len(a.cfg.Reflectors) > 0 {
			return nil, NATInfo{Type: NATBlocked}
		}
		return nil, NATInfo{}
	}
	sort.Slice(got, func(i, j int) bool { return got[i].order < got[j].order })
	var cands []Candidate
	for _, g := range got {
		cands = append(cands, Candidate{Kind: Reflexive, Addr: g.mapped, Priority: priority(Reflexive, g.mapped.Addr())})
	}
	nat := NATInfo{Last: got[len(got)-1].mapped}
	if slices.Contains(hosts, got[0].mapped.Addr()) && got[0].mapped.Port() == addrPort(a.mainSock().pc.LocalAddr()).Port() {
		nat.Type = NATOpen
		return cands, nat
	}
	// Distinct destinations: same outer port everywhere → independent.
	ports := map[netip.AddrPort]uint16{}
	var order []uint16
	for _, g := range got {
		if _, ok := ports[g.dst]; !ok {
			ports[g.dst] = g.mapped.Port()
			order = append(order, g.mapped.Port())
		}
	}
	switch {
	case len(order) < 2:
		nat.Type = NATIndep // can't tell; assume the common case
	case allSame(order):
		nat.Type = NATIndep
	default:
		step := int(order[1]) - int(order[0])
		seq := step != 0 && step >= -64 && step <= 64
		for i := 2; i < len(order) && seq; i++ {
			seq = int(order[i])-int(order[i-1]) == step
		}
		if seq {
			nat.Type, nat.Step = NATSymSeq, int16(step)
		} else {
			nat.Type = NATSymRandom
		}
	}
	return cands, nat
}

func allSame(ps []uint16) bool {
	for _, p := range ps[1:] {
		if p != ps[0] {
			return false
		}
	}
	return true
}

func resolve(ctx context.Context, hostport string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(hostport); err == nil {
		return ap, nil
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return netip.AddrPort{}, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return netip.AddrPort{}, fmt.Errorf("direct: resolve %s: %v", host, err)
	}
	p, err := net.LookupPort("udp", port)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(ips[0].Unmap(), uint16(p)), nil
}

// binding sends STUN binding requests (3 tries) and returns the mapped address.
func (a *Agent) binding(ctx context.Context, s *sock, dst netip.AddrPort) (netip.AddrPort, error) {
	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if err != nil {
		return netip.AddrPort{}, err
	}
	ch := make(chan netip.AddrPort, 1)
	a.mu.Lock()
	a.stunTx[msg.TransactionID] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.stunTx, msg.TransactionID)
		a.mu.Unlock()
	}()
	for try := 0; try < 3; try++ {
		s.pc.WriteTo(msg.Raw, udpAddr(dst))
		select {
		case m := <-ch:
			return m, nil
		case <-ctx.Done():
			return netip.AddrPort{}, ctx.Err()
		case <-time.After(time.Duration(300*(try+1)) * time.Millisecond):
		}
	}
	return netip.AddrPort{}, errors.New("direct: reflector did not answer")
}

func (a *Agent) onSTUN(b []byte) {
	m := &stun.Message{Raw: append([]byte(nil), b...)}
	if m.Decode() != nil || m.Type != stun.BindingSuccess {
		return
	}
	var x stun.XORMappedAddress
	if x.GetFrom(m) != nil {
		return
	}
	ip, ok := netip.AddrFromSlice(x.IP)
	if !ok {
		return
	}
	a.mu.Lock()
	ch := a.stunTx[m.TransactionID]
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- netip.AddrPortFrom(ip.Unmap(), uint16(x.Port)):
		default:
		}
	}
}

// ---- probes (connectivity.md §4) ----
//
//	probe = "MDP" | 1 | sid(16) | type(1) | tx(8) | [seen addr] | mac(16)
//	type 1/2 request/response from the controlling side, 3/4 from the
//	other; a response carries the address the request came from. A side
//	ignores requests with its own role: on two networks with the same
//	private addresses it could otherwise reach itself and call it a path.

const (
	probeMagic = "MDP\x01"
	probeReq   = 1
	probeResp  = 2
	macLen     = 16
)

func (a *Agent) probe(typ byte, tx [8]byte, seen netip.AddrPort) []byte {
	wire := typ
	if !a.controlling {
		wire += 2
	}
	b := append([]byte(probeMagic), a.sid[:]...)
	b = append(b, wire)
	b = append(b, tx[:]...)
	if typ == probeResp {
		b = appendAddr(b, seen)
	}
	m := hmac.New(sha256.New, a.macKey)
	m.Write(b)
	return m.Sum(b)[:len(b)+macLen]
}

func (a *Agent) checkProbe(b []byte) (typ byte, tx [8]byte, seen netip.AddrPort, ok bool) {
	if len(b) < len(probeMagic)+16+1+8+macLen || string(b[:4]) != probeMagic {
		return
	}
	body, mac := b[:len(b)-macLen], b[len(b)-macLen:]
	m := hmac.New(sha256.New, a.macKey)
	m.Write(body)
	if !hmac.Equal(m.Sum(nil)[:macLen], mac) || string(body[4:20]) != string(a.sid[:]) {
		return // another session, or another device at the same address
	}
	typ = body[20]
	theirs := typ > 2
	if theirs {
		typ -= 2
	}
	if theirs != a.controlling || typ < probeReq || typ > probeResp {
		return 0, tx, seen, false // our own role: a probe of ours that came back
	}
	copy(tx[:], body[21:29])
	if typ == probeResp {
		var err error
		if seen, _, err = readAddr(body[29:]); err != nil {
			return
		}
	}
	return typ, tx, seen, true
}

func (a *Agent) onProbe(s *sock, b []byte, from netip.AddrPort) {
	typ, tx, seen, ok := a.checkProbe(b)
	if !ok {
		return
	}
	switch typ {
	case probeReq:
		s.pc.WriteTo(a.probe(probeResp, tx, from), udpAddr(from))
		// Triggered check: an address we didn't know is peer-reflexive.
		a.mu.Lock()
		if a.link == nil && a.pairs != nil {
			if _, known := a.pairs[pairKey{s, from}]; !known {
				a.addPair(s, Candidate{Kind: PeerRefl, Addr: from, Priority: priority(PeerRefl, from.Addr())})
			}
		}
		a.mu.Unlock()
	case probeResp:
		a.mu.Lock()
		p := a.byTx[tx]
		if p != nil && !p.ok {
			p.ok, p.rtt, p.seenAs = true, time.Since(p.sent), seen
			if from != p.remote.Addr { // answered from elsewhere: that is the path
				p.remote = Candidate{Kind: PeerRefl, Addr: from, Priority: priority(PeerRefl, from.Addr())}
			}
			select {
			case a.won <- struct{}{}:
			default:
			}
		}
		a.mu.Unlock()
	}
}

// addPair adds a pair to check; a.mu held.
func (a *Agent) addPair(s *sock, r Candidate) *pair {
	k := pairKey{s, r.Addr}
	if p, ok := a.pairs[k]; ok {
		return p
	}
	if r.Addr.Addr().Is4() != addrIs4(s) && !s.relayed {
		return nil // other family
	}
	p := &pair{s: s, remote: r, interval: 100 * time.Millisecond}
	rand.Read(p.tx[:])
	a.pairs[k] = p
	a.byTx[p.tx] = p
	return p
}

func addrIs4(s *sock) bool {
	la := addrPort(s.pc.LocalAddr()).Addr()
	return la.Is4() || la.IsUnspecified() // a dual-stack socket also sends IPv4
}

func (a *Agent) readLoop(s *sock) {
	buf := make([]byte, 2048)
	for {
		n, from, err := s.pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "closed") {
				return
			}
			continue
		}
		b, src := buf[:n], addrPort(from)
		switch {
		case n >= 4 && string(b[:4]) == probeMagic:
			a.onProbe(s, b, src)
		case n >= 4 && string(b[:4]) == dataMagic:
			a.mu.Lock()
			l := a.link
			a.mu.Unlock()
			if l != nil {
				l.onData(s, b, src)
			}
		case stun.IsMessage(b):
			a.onSTUN(b)
		}
	}
}

// ---- checks and choice ----

// Connect runs the checks against the peer's offer and returns the link on
// the best pair that succeeded (connectivity.md §4, §4a, §5).
func (a *Agent) Connect(ctx context.Context, remote Offer) (*Link, error) {
	a.mu.Lock()
	a.remote = remote
	for _, s := range a.socks {
		for _, r := range remote.Candidates {
			a.addPair(s, r)
		}
	}
	// Symmetric sequential peer: its next ports are predictable.
	if remote.NAT.Type == NATSymSeq && remote.NAT.Last.IsValid() {
		for k := 1; k <= 64; k++ {
			port := int(remote.NAT.Last.Port()) + k*int(remote.NAT.Step)
			if port > 0 && port < 65536 {
				ap := netip.AddrPortFrom(remote.NAT.Last.Addr(), uint16(port))
				a.addPair(a.socks[0], Candidate{Kind: Predicted, Addr: ap, Priority: priority(Predicted, ap.Addr())})
			}
		}
	}
	a.mu.Unlock()

	start := time.Now()
	normalEnd := start.Add(a.cfg.CheckTimeout)
	end := normalEnd
	var settle time.Time
	symStarted := false
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		now := time.Now()
		a.mu.Lock()
		won := false
		for _, p := range a.pairs {
			won = won || p.ok
			if !p.ok && !now.Before(p.next) {
				p.sent, p.next = now, now.Add(p.interval)
				p.interval = min(p.interval*2, time.Second)
				p.s.pc.WriteTo(a.probe(probeReq, p.tx, netip.AddrPort{}), udpAddr(p.remote.Addr))
			}
		}
		a.mu.Unlock()
		if won && settle.IsZero() {
			settle = now.Add(400 * time.Millisecond) // a better pair may still answer
		}
		if !settle.IsZero() && now.After(settle) {
			return a.choose()
		}
		if !won && !symStarted && now.Sub(start) > min(2500*time.Millisecond, a.cfg.CheckTimeout/2) && a.symmetricCase(remote) {
			symStarted = true
			end = now.Add(a.cfg.SymmetricTimeout)
			go a.symmetric(ctx, remote, end)
		}
		if now.After(end) && now.After(normalEnd) {
			return nil, fmt.Errorf("%w: %s", ErrNoPath, reason(a.nat, remote.NAT))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		case <-a.won:
		}
	}
}

func (a *Agent) symmetricCase(remote Offer) bool {
	sym := func(t NATType) bool { return t == NATSymRandom || t == NATSymSeq }
	return sym(a.nat.Type) || sym(remote.NAT.Type)
}

// symmetric runs the birthday method (connectivity.md §4a) until end.
func (a *Agent) symmetric(ctx context.Context, remote Offer, end time.Time) {
	var target netip.Addr
	for _, c := range remote.Candidates {
		if c.Kind == Reflexive {
			target = c.Addr.Addr()
		}
	}
	if remote.NAT.Last.IsValid() {
		target = remote.NAT.Last.Addr()
	}
	if !target.IsValid() {
		return
	}
	weSym := a.nat.Type == NATSymRandom || a.nat.Type == NATSymSeq
	theySym := remote.NAT.Type == NATSymRandom
	if weSym {
		// 256 sockets, each makes its own mapping by probing the peer's
		// reflexive addresses; a random probe from the peer may hit one.
		var refl []Candidate
		for _, c := range remote.Candidates {
			if c.Kind == Reflexive {
				refl = append(refl, c)
			}
		}
		for i := 0; i < 256 && ctx.Err() == nil; i++ {
			pc, err := a.cfg.Net.ListenUDP(0)
			if err != nil {
				break
			}
			s := &sock{pc: pc, extra: true}
			a.addSock(s)
			a.mu.Lock()
			for _, r := range refl {
				a.addPair(s, r)
			}
			a.mu.Unlock()
		}
	}
	if !theySym && !weSym {
		return
	}
	// Random ports of the symmetric peer's address, ~100 per second.
	from := []*sock{a.mainSock()}
	if weSym {
		a.mu.Lock()
		from = slices.Clone(a.socks)
		a.mu.Unlock()
	}
	if !theySym {
		return // the peer is the one probing random ports
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for i := 0; i < 2048 && time.Now().Before(end); i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.mu.Lock()
		if a.link != nil || a.closed {
			a.mu.Unlock()
			return
		}
		var pb [2]byte
		rand.Read(pb[:])
		port := 1024 + int(binary.BigEndian.Uint16(pb[:]))%(65536-1024)
		ap := netip.AddrPortFrom(target, uint16(port))
		p := a.addPair(from[i%len(from)], Candidate{Kind: Predicted, Addr: ap, Priority: priority(Predicted, target)})
		if p != nil {
			p.interval = time.Hour // one probe each: they are guesses
		}
		a.mu.Unlock()
	}
}

// choose picks the best succeeded pair and starts the link.
func (a *Agent) choose() (*Link, error) {
	a.mu.Lock()
	var best *pair
	bestClass := 99
	for _, p := range a.pairs {
		if !p.ok {
			continue
		}
		lk := a.localKind(p)
		c := class(lk, p.remote.Kind, p.remote.Addr.Addr())
		if best == nil || c < bestClass || (c == bestClass && p.rtt < best.rtt) {
			best, bestClass = p, c
		}
	}
	if best == nil {
		a.mu.Unlock()
		return nil, ErrNoPath
	}
	l := newLink(a, best.s, best.remote.Addr, Path{
		Local: Candidate{Kind: a.localKind(best), Addr: best.seenAs}, Remote: best.remote, RTT: best.rtt, Class: bestClass,
	})
	a.link = l
	// Birthday sockets other than the chosen one are closed.
	var keep []*sock
	for _, s := range a.socks {
		if s.extra && s != best.s {
			s.pc.Close()
			continue
		}
		keep = append(keep, s)
	}
	a.socks = keep
	a.mu.Unlock()
	go l.keepalive()
	a.cfg.Logf("direct: path %s", l.path)
	return l, nil
}

// localKind: what our side of a pair was, from the address the peer saw.
func (a *Agent) localKind(p *pair) Kind {
	if p.s.relayed {
		return Relayed
	}
	for _, c := range a.local {
		if c.Addr == p.seenAs {
			return c.Kind
		}
	}
	if p.s.extra {
		return Predicted
	}
	return PeerRefl
}

// reason explains a failure from both NAT types, for the user.
func reason(us, them NATInfo) string {
	sym := func(t NATType) bool { return t == NATSymRandom || t == NATSymSeq }
	switch {
	case us.Type == NATBlocked || them.Type == NATBlocked:
		return "a network on the way blocks UDP"
	case sym(us.Type) && sym(them.Type):
		return "both of you are behind strict (symmetric) NATs, often a carrier's: try Wi-Fi"
	case sym(us.Type):
		return "your network's NAT is strict (symmetric): try another network"
	case sym(them.Type):
		return "the other side's NAT is strict (symmetric)"
	}
	return "no address pair answered (firewall or NAT on the way)"
}

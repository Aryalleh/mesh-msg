package node

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Direct links (docs/connectivity.md): two devices exchange candidates in
// end-to-end encrypted messages (content type 8) over any path that works,
// run the checks of core/direct, and the chosen path becomes a router link
// named "direct" to that one contact. Normal media (which never goes
// through relays) is fetched over it; calls will use the same machinery.
//
//	signal = version(1)=1 | op(1) | sid(16) | body
//	op 0 OFFER   expires(4) | key(32) | offer      from the side that starts
//	op 1 ANSWER  offer
//	op 2 CLOSE   reason(1)                         0 done, 1 no path, 2 refused
//
// The key travels inside the end-to-end encrypted message; it keys the
// probes and the link.

// DirectConfig configures direct links.
type DirectConfig struct {
	Off bool `toml:"off" json:"off"`
	// NoPortMapping: don't ask the router for a port (PCP, NAT-PMP, UPnP).
	NoPortMapping bool `toml:"no_port_mapping" json:"no_port_mapping"`
	// Reflectors overrides the STUN servers (default: every relay's UDP
	// 3478 and 3479).
	Reflectors []string `toml:"reflectors" json:"reflectors"`
}

const (
	sigVersion  = 1
	sigOffer    = 0
	sigAnswer   = 1
	sigClose    = 2
	offerLife   = 60 * time.Second
	retryDirect = 2 * time.Minute // after a failed attempt to the same contact
	fragHead    = 4 + 1 + 1       // id | index | count
)

type directLink struct {
	n *Node

	mu       sync.Mutex
	sess     map[string]*directSess // by contact hash
	lastTry  map[string]time.Time
	lastFail map[string]string
	net      direct.Net // tests
}

type directSess struct {
	peer   identity.Public
	sid    [16]byte
	key    [32]byte
	agent  *direct.Agent
	link   *direct.Link
	answer chan direct.Offer
	ctx    context.Context
	cancel context.CancelFunc

	rmu   sync.Mutex
	parts map[uint32]*reasm
	fragN uint32
}

type reasm struct {
	got   [][]byte
	left  int
	start time.Time
}

func newDirectLink(n *Node) *directLink {
	return &directLink{n: n, sess: map[string]*directSess{}, lastTry: map[string]time.Time{}, lastFail: map[string]string{}}
}

// SetSocketBinder sets the hook that ties a socket to the network of its
// local address (Android, direct.OSNet.Bind). Call it before Start.
func (n *Node) SetSocketBinder(bind func(fd int, ip string)) {
	if n.direct == nil {
		return
	}
	n.direct.mu.Lock()
	n.direct.net = direct.OSNet{Bind: func(fd int, ip netip.Addr) { bind(fd, ip.String()) }}
	n.direct.mu.Unlock()
}

func (d *directLink) Name() string          { return "direct" }
func (d *directLink) Kind() router.LinkKind { return router.PointToPoint }
func (d *directLink) MTU() int              { return packet.MaxSize }

// Send splits raw into datagrams on the contact's link.
func (d *directLink) Send(peer router.PeerID, raw []byte) {
	d.mu.Lock()
	s := d.sess[string(peer)]
	d.mu.Unlock()
	if s == nil || s.link == nil {
		return
	}
	chunk := direct.MaxPayload - fragHead
	cnt := (len(raw) + chunk - 1) / chunk
	if cnt == 0 || cnt > 255 {
		return
	}
	s.rmu.Lock()
	s.fragN++
	id := s.fragN
	s.rmu.Unlock()
	for i := 0; i < cnt; i++ {
		part := raw[i*chunk : min((i+1)*chunk, len(raw))]
		b := make([]byte, fragHead, fragHead+len(part))
		binary.BigEndian.PutUint32(b, id)
		b[4], b[5] = byte(i), byte(cnt)
		if s.link.Send(append(b, part...)) != nil {
			return
		}
	}
}

func (s *directSess) reassemble(b []byte) []byte {
	if len(b) < fragHead {
		return nil
	}
	id, idx, cnt := binary.BigEndian.Uint32(b), int(b[4]), int(b[5])
	if cnt == 0 || idx >= cnt {
		return nil
	}
	if cnt == 1 {
		return b[fragHead:]
	}
	s.rmu.Lock()
	defer s.rmu.Unlock()
	now := time.Now()
	for k, r := range s.parts {
		if now.Sub(r.start) > 10*time.Second {
			delete(s.parts, k)
		}
	}
	r := s.parts[id]
	if r == nil {
		if len(s.parts) >= 64 {
			return nil
		}
		r = &reasm{got: make([][]byte, cnt), left: cnt, start: now}
		s.parts[id] = r
	}
	if len(r.got) != cnt || r.got[idx] != nil {
		return nil
	}
	r.got[idx] = append([]byte(nil), b[fragHead:]...)
	r.left--
	if r.left > 0 {
		return nil
	}
	delete(s.parts, id)
	var out []byte
	for _, p := range r.got {
		out = append(out, p...)
	}
	return out
}

// Connected tells whether a direct link to the contact is up.
func (d *directLink) connected(peer string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sess[peer]
	return s != nil && s.link != nil
}

// Ensure starts a session to the contact unless one is up, starting, or
// failed a moment ago.
func (d *directLink) ensure(peer identity.Public) {
	h := peer.Hash().String()
	d.mu.Lock()
	if d.sess[h] != nil || time.Since(d.lastTry[h]) < retryDirect {
		d.mu.Unlock()
		return
	}
	d.lastTry[h] = time.Now()
	s := d.newSess(peer)
	rand.Read(s.sid[:])
	rand.Read(s.key[:])
	d.sess[h] = s
	d.mu.Unlock()
	go d.run(s, true, direct.Offer{})
}

func (d *directLink) newSess(peer identity.Public) *directSess {
	ctx, cancel := context.WithCancel(context.Background())
	return &directSess{peer: peer, answer: make(chan direct.Offer, 1), parts: map[uint32]*reasm{}, ctx: ctx, cancel: cancel}
}

func (d *directLink) config(ctx context.Context) direct.Config {
	cfg := d.n.cfg.Direct
	d.mu.Lock()
	dnet := d.net
	d.mu.Unlock()
	c := direct.Config{Net: dnet, PortMapping: !cfg.NoPortMapping, Reflectors: cfg.Reflectors, Logf: d.n.cfg.Logf}
	d.n.mu.Lock()
	var relays []*relayLink
	for _, rl := range d.n.relays {
		relays = append(relays, rl)
	}
	d.n.mu.Unlock()
	if len(c.Reflectors) == 0 {
		seen := map[string]bool{}
		for _, rl := range relays {
			if h := rl.c.Host(); h != "" && !seen[h] {
				seen[h] = true
				for _, p := range []int{wire.PortReflector, wire.PortReflectorAlt} {
					c.Reflectors = append(c.Reflectors, net.JoinHostPort(h, strconv.Itoa(p)))
				}
			}
		}
	}
	// TURN: only a relay's admins get credentials; others get 403 at once.
	for i, rl := range relays {
		if i >= 3 || !rl.isUp() {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		tc, err := rl.c.TurnCredentials(tctx, d.n.id)
		cancel()
		if err == nil {
			c.TURN = &direct.TURNServer{Addr: net.JoinHostPort(rl.c.Host(), strconv.Itoa(tc.Port)), Username: tc.Username, Password: tc.Password}
			break
		}
	}
	return c
}

// run drives one session: gather, signal, check, then carry packets.
func (d *directLink) run(s *directSess, controlling bool, remote direct.Offer) {
	h := s.peer.Hash().String()
	ctx, cancel := s.ctx, s.cancel
	fail := func(why error) {
		cancel()
		if s.agent != nil {
			s.agent.Close()
		}
		d.mu.Lock()
		if d.sess[h] == s {
			delete(d.sess, h)
		}
		d.lastFail[h] = why.Error()
		d.mu.Unlock()
		d.n.cfg.Logf("node: direct link to %s: %v", h[:8], why)
	}
	a, err := direct.NewAgent(d.config(ctx), s.sid, s.key, controlling)
	if err != nil {
		fail(err)
		return
	}
	s.agent = a
	mine := a.Gather(ctx)
	if controlling {
		body := binary.BigEndian.AppendUint32(nil, uint32(time.Now().Add(offerLife).Unix()))
		body = append(body, s.key[:]...)
		d.signal(s, sigOffer, append(body, mine.Encode()...))
		select {
		case remote = <-s.answer:
		case <-time.After(offerLife):
			fail(errors.New("no answer"))
			return
		case <-ctx.Done():
			fail(errors.New("closed"))
			return
		}
	} else {
		d.signal(s, sigAnswer, mine.Encode())
	}
	l, err := a.Connect(ctx, remote)
	if err != nil {
		if controlling {
			d.signal(s, sigClose, []byte{1})
		}
		fail(err)
		return
	}
	d.mu.Lock()
	if d.sess[h] != s {
		d.mu.Unlock()
		l.Close()
		return
	}
	s.link = l
	delete(d.lastFail, h)
	d.mu.Unlock()
	d.n.setLinkStatus("direct", fmt.Sprintf("%s: %s", h[:8], l.Path()))
	d.n.router.PeerUp("direct", router.PeerID(h))
	d.n.mediaLinkUp()
	go func() {
		for b := range l.Recv() {
			if raw := s.reassemble(b); raw != nil {
				d.n.Inbound("direct", router.PeerID(h), raw)
			}
		}
	}()
	<-l.Done()
	d.n.router.PeerDown("direct", router.PeerID(h))
	d.mu.Lock()
	if d.sess[h] == s {
		delete(d.sess, h)
	}
	d.mu.Unlock()
	cancel()
}

func (d *directLink) signal(s *directSess, op byte, body []byte) {
	b := append([]byte{sigVersion, op}, s.sid[:]...)
	b = append(b, body...)
	if _, err := d.n.router.SendContent(d.n.id, s.peer, e2e.ContentDirect, b, true, true); err != nil {
		d.n.cfg.Logf("node: direct signaling: %v", err)
	}
}

// onSignal handles a signaling message from a saved contact.
func (d *directLink) onSignal(from identity.Public, b []byte) {
	if len(b) < 18 || b[0] < sigVersion {
		return
	}
	op := b[1]
	var sid [16]byte
	copy(sid[:], b[2:18])
	body := b[18:]
	h := from.Hash().String()
	switch op {
	case sigOffer:
		if len(body) < 36 || d.n.cfg.Direct.Off {
			return
		}
		if time.Unix(int64(binary.BigEndian.Uint32(body)), 0).Before(time.Now()) {
			return // arrived late (stored by a relay): the other side gave up
		}
		offer, _, err := direct.DecodeOffer(body[36:])
		if err != nil {
			return
		}
		d.mu.Lock()
		if old := d.sess[h]; old != nil {
			// Both started at once: the smaller hash's offer wins.
			if old.link != nil || d.n.id.Hash().String() < h {
				d.mu.Unlock()
				return
			}
			old.cancel()
			delete(d.sess, h)
		}
		s := d.newSess(from)
		s.sid = sid
		copy(s.key[:], body[4:36])
		d.sess[h] = s
		d.lastTry[h] = time.Now()
		d.mu.Unlock()
		go d.run(s, false, offer)
	case sigAnswer:
		offer, _, err := direct.DecodeOffer(body)
		if err != nil {
			return
		}
		d.mu.Lock()
		s := d.sess[h]
		d.mu.Unlock()
		if s != nil && s.sid == sid {
			select {
			case s.answer <- offer:
			default:
			}
		}
	case sigClose:
		d.mu.Lock()
		s := d.sess[h]
		d.mu.Unlock()
		if s != nil && s.sid == sid && s.link == nil {
			s.cancel()
		}
	}
}

func (d *directLink) close() {
	d.mu.Lock()
	ss := d.sess
	d.sess = map[string]*directSess{}
	d.mu.Unlock()
	for _, s := range ss {
		s.cancel()
		if s.link != nil {
			s.link.Close()
		} else if s.agent != nil {
			s.agent.Close()
		}
	}
}

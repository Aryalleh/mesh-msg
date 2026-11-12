package node

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// lanLink meshes with other nodes on the same local/private network over
// TCP. Peers are found by UDP multicast beacons and/or static addresses.
// Session IDs are random per run (not the user's identity), and packets are
// already end-to-end encrypted.
type lanLink struct {
	n       *Node
	cfg     LANConfig
	session string
	ln      net.Listener

	mu    sync.Mutex
	conns map[router.PeerID]*lanConn
}

type lanConn struct {
	c  net.Conn
	mu sync.Mutex
}

const (
	lanGroup     = "239.255.77.77:47777"
	lanMagic     = "MESHMSG1"
	lanHandshake = 10 * time.Second
)

func newLANLink(n *Node, cfg LANConfig) (*lanLink, error) {
	var b [8]byte
	rand.Read(b[:])
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return nil, err
	}
	return &lanLink{n: n, cfg: cfg, session: hex.EncodeToString(b[:]), ln: ln, conns: map[router.PeerID]*lanConn{}}, nil
}

func (l *lanLink) Name() string          { return "lan" }
func (l *lanLink) Kind() router.LinkKind { return router.PointToPoint }
func (l *lanLink) MTU() int              { return packet.MaxSize }
func (l *lanLink) addr() string          { return l.ln.Addr().String() }

func (l *lanLink) Send(peer router.PeerID, raw []byte) {
	l.mu.Lock()
	lc := l.conns[peer]
	l.mu.Unlock()
	if lc == nil {
		return
	}
	lc.mu.Lock()
	lc.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := packet.WriteFrame(lc.c, raw)
	lc.mu.Unlock()
	if err != nil {
		lc.c.Close() // reader goroutine reports PeerDown
	}
}

func (l *lanLink) port() int { return l.ln.Addr().(*net.TCPAddr).Port }

func (l *lanLink) run(ctx context.Context) {
	l.n.setLinkStatus("lan", "listening on "+l.addr())
	go func() { <-ctx.Done(); l.ln.Close() }()
	go l.acceptLoop(ctx)
	if l.cfg.Discovery {
		go l.beacon(ctx)
		go l.listenBeacons(ctx)
	}
	for ctx.Err() == nil {
		for _, addr := range l.cfg.StaticPeers {
			if !l.connectedTo(addr) {
				go l.dial(ctx, addr)
			}
		}
		sleepCtx(ctx, 15*time.Second)
	}
	l.mu.Lock()
	for _, lc := range l.conns {
		lc.c.Close()
	}
	l.mu.Unlock()
}

func (l *lanLink) connectedTo(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, lc := range l.conns {
		if lc.c.RemoteAddr().String() == addr {
			return true
		}
	}
	return false
}

func (l *lanLink) acceptLoop(ctx context.Context) {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			return
		}
		go l.serve(ctx, c)
	}
}

func (l *lanLink) dial(ctx context.Context, addr string) {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return
	}
	l.serve(ctx, c)
}

// serve runs the handshake ("MESHMSG1 <session>\n" both ways) and the read loop.
func (l *lanLink) serve(ctx context.Context, c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(lanHandshake))
	if _, err := fmt.Fprintf(c, "%s %s\n", lanMagic, l.session); err != nil {
		return
	}
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	f := strings.Fields(line)
	if len(f) != 2 || f[0] != lanMagic || f[1] == l.session {
		return
	}
	c.SetDeadline(time.Time{})
	peer := router.PeerID(f[1])

	l.mu.Lock()
	if _, dup := l.conns[peer]; dup {
		l.mu.Unlock()
		return // already connected (both sides dialled)
	}
	lc := &lanConn{c: c}
	l.conns[peer] = lc
	count := len(l.conns)
	l.mu.Unlock()
	l.n.setLinkStatus("lan", fmt.Sprintf("%d peers", count))
	l.n.router.PeerUp("lan", peer)
	l.n.mediaLinkUp()

	defer func() {
		l.mu.Lock()
		if l.conns[peer] == lc {
			delete(l.conns, peer)
		}
		count := len(l.conns)
		l.mu.Unlock()
		l.n.router.PeerDown("lan", peer)
		l.n.setLinkStatus("lan", fmt.Sprintf("%d peers", count))
	}()
	for ctx.Err() == nil {
		raw, err := packet.ReadFrame(br)
		if err != nil {
			return
		}
		l.n.Inbound("lan", peer, raw)
	}
}

func (l *lanLink) beacon(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp4", lanGroup)
	if err != nil {
		return
	}
	c, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		l.n.cfg.Logf("lan: beacon: %v", err)
		return
	}
	defer c.Close()
	msg := fmt.Sprintf("%s %s %d", lanMagic, l.session, l.port())
	for {
		c.Write([]byte(msg))
		if !sleepCtx(ctx, 5*time.Second) {
			return
		}
	}
}

func (l *lanLink) listenBeacons(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp4", lanGroup)
	if err != nil {
		return
	}
	c, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		l.n.cfg.Logf("lan: discovery: %v", err)
		return
	}
	go func() { <-ctx.Done(); c.Close() }()
	buf := make([]byte, 256)
	for {
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		f := strings.Fields(string(buf[:n]))
		if len(f) != 3 || f[0] != lanMagic || f[1] == l.session {
			continue
		}
		// The smaller session ID dials, so each pair makes one connection.
		if l.session > f[1] {
			continue
		}
		port, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		l.mu.Lock()
		_, have := l.conns[router.PeerID(f[1])]
		l.mu.Unlock()
		if !have {
			go l.dial(ctx, net.JoinHostPort(src.IP.String(), strconv.Itoa(port)))
		}
	}
}

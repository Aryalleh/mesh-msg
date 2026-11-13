package direct

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// Path describes the chosen pair.
type Path struct {
	Local, Remote Candidate
	RTT           time.Duration
	Class         int // connectivity.md §5: 1 local network … 6 relayed
}

func (p Path) String() string {
	return fmt.Sprintf("%s ↔ %s (class %d, %v)", p.Local, p.Remote, p.Class, p.RTT.Round(time.Millisecond))
}

// Relayed tells whether the path goes through a relay's TURN.
func (p Path) Relayed() bool { return p.Class == 6 }

// MaxPayload is the largest datagram Send takes (stays under 1200 bytes).
const MaxPayload = 1200 - dataHead - chacha20poly1305.Overhead

// followAfter: how long the controlling side's pair must be silent before it
// follows data arriving on another of its sockets.
const followAfter = 3 * time.Second

const (
	dataMagic = "MDD\x01"
	dataHead  = 4 + 8 // magic | seq
	idleAfter = 45 * time.Second
	keepEvery = 15 * time.Second
)

// Link carries datagrams over the chosen path, sealed with
// ChaCha20-Poly1305 (one key per direction, from the session key). It
// follows the peer when its address changes (a network change on its side).
type Link struct {
	a    *Agent
	path Path

	mu     sync.Mutex
	s      *sock
	remote netip.AddrPort
	seal   cipher.AEAD
	open   cipher.AEAD
	seq    uint64
	maxRx  uint64
	window uint64 // replay bitmap below maxRx
	lastRx time.Time
	curRx  time.Time // last data on the pair in use (l.s, l.remote)

	recv chan []byte
	done chan struct{}
	once sync.Once
}

func newLink(a *Agent, s *sock, remote netip.AddrPort, p Path) *Link {
	out, in := "mava direct data a", "mava direct data b"
	if !a.controlling {
		out, in = in, out
	}
	seal, _ := chacha20poly1305.New(derive(a.key, out))
	open, _ := chacha20poly1305.New(derive(a.key, in))
	return &Link{a: a, path: p, s: s, remote: remote, seal: seal, open: open,
		lastRx: time.Now(), curRx: time.Now(), recv: make(chan []byte, 512), done: make(chan struct{})}
}

// Path is the pair in use.
func (l *Link) Path() Path {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.path
	p.Remote.Addr = l.remote
	return p
}

// Recv delivers received datagrams; it is closed with the link.
func (l *Link) Recv() <-chan []byte { return l.recv }

// Done is closed when the link ends (closed, or the peer went silent).
func (l *Link) Done() <-chan struct{} { return l.done }

var errClosed = errors.New("direct: link closed")

// Send seals and sends one datagram (≤ MaxPayload bytes).
func (l *Link) Send(p []byte) error {
	if len(p) > MaxPayload {
		return fmt.Errorf("direct: datagram of %d bytes over %d", len(p), MaxPayload)
	}
	select {
	case <-l.done:
		return errClosed
	default:
	}
	l.mu.Lock()
	l.seq++
	h := make([]byte, dataHead, dataHead+len(p)+chacha20poly1305.Overhead)
	copy(h, dataMagic)
	binary.BigEndian.PutUint64(h[4:], l.seq)
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], l.seq)
	b := l.seal.Seal(h, nonce[:], p, h)
	s, to := l.s, l.remote
	l.mu.Unlock()
	_, err := s.pc.WriteTo(b, udpAddr(to))
	return err
}

func (l *Link) onData(s *sock, b []byte, from netip.AddrPort) {
	if len(b) < dataHead+chacha20poly1305.Overhead {
		return
	}
	seq := binary.BigEndian.Uint64(b[4:])
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], seq)
	l.mu.Lock()
	if !l.fresh(seq) {
		l.mu.Unlock()
		return
	}
	p, err := l.open.Open(nil, nonce[:], b[dataHead:], b[:dataHead])
	if err != nil {
		l.mu.Unlock()
		return
	}
	l.mark(seq)
	now := time.Now()
	l.lastRx = now
	if from != l.remote || s != l.s {
		// Authenticated data from another address. On the same socket the
		// peer moved (a NAT rebinding): follow it at once. On another socket
		// the two sides chose different pairs on their own: the controlled
		// side takes the controlling side's pair, and the controlling side
		// keeps its own unless that has gone quiet. If both switched at
		// once they could swap pairs back and forth and lose data.
		if s == l.s || !l.a.controlling || now.Sub(l.curRx) > followAfter {
			l.remote, l.s = from, s
			l.curRx = now
		}
	} else {
		l.curRx = now
	}
	l.mu.Unlock()
	if len(p) == 0 {
		return // keepalive
	}
	select {
	case l.recv <- p:
	case <-l.done:
	default: // receiver too slow: drop, like the network would
	}
}

// fresh: not a replay (64-packet window); l.mu held.
func (l *Link) fresh(seq uint64) bool {
	switch {
	case seq == 0:
		return false
	case seq > l.maxRx:
		return true
	case l.maxRx-seq >= 64:
		return false
	}
	return l.window&(1<<(l.maxRx-seq)) == 0
}

func (l *Link) mark(seq uint64) {
	if seq > l.maxRx {
		shift := seq - l.maxRx
		if shift >= 64 {
			l.window = 0
		} else {
			l.window <<= shift
		}
		l.window |= 1
		l.maxRx = seq
		return
	}
	l.window |= 1 << (l.maxRx - seq)
}

func (l *Link) keepalive() {
	t := time.NewTicker(keepEvery)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
		}
		l.mu.Lock()
		idle := time.Since(l.lastRx)
		l.mu.Unlock()
		if idle > idleAfter {
			l.Close()
			return
		}
		l.Send(nil)
	}
}

// Close ends the link and the session (sockets, port mappings, TURN).
func (l *Link) Close() {
	l.once.Do(func() {
		close(l.done)
		l.a.Close()
	})
}

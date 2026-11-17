// Package blemesh joins meshd to the Bluetooth mesh of phones and Mava boards
// (android/app/.../ble/BleProto.kt, firmware/src/ble.cpp): the same GATT
// service, hello and len(3) || packet stream. meshd only connects out (GATT
// client, central_linux.go): it doesn't advertise, so phones and boards never
// have to connect to it, and every device it sees becomes a peer.
package blemesh

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

const (
	// LinkName is the router link and status line.
	LinkName = "ble"
	// MaxPeers matches the phones' limit.
	MaxPeers    = 4
	frameHeader = 3
	helloMagic  = 0x4D
	queueChunks = 1024
)

// Host is what the link needs from the node (node.Node satisfies it via NodeHost).
type Host interface {
	Inbound(link string, peer router.PeerID, raw []byte)
	PeerUp(peer router.PeerID)
	PeerDown(peer router.PeerID)
	Status(s string)
}

type peer struct {
	id    string
	chunk int
	out   chan []byte
	close func()
	rx    []byte
	done  chan struct{}
}

// Link is every Bluetooth peer as one point-to-point router link.
type Link struct {
	host  Host
	mu    sync.Mutex
	peers map[string]*peer
}

func New(h Host) *Link { return &Link{host: h, peers: map[string]*peer{}} }

func (l *Link) Name() string          { return LinkName }
func (l *Link) Kind() router.LinkKind { return router.PointToPoint }
func (l *Link) MTU() int              { return packet.MaxSize }

// Has reports whether id is connected.
func (l *Link) Has(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peers[id] != nil
}

// Count is the number of peers.
func (l *Link) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.peers)
}

// Up adds a connected peer. write sends one GATT write (with response) and
// blocks until it is acknowledged; it is called from one goroutine per peer,
// first with hello. mtu is the ATT MTU; close disconnects the peer.
func (l *Link) Up(id string, mtu int, hello []byte, write func([]byte) error, close func()) {
	p := &peer{id: id, chunk: min(max(mtu-3, 20), 244), out: make(chan []byte, queueChunks), close: close, done: make(chan struct{})}
	l.mu.Lock()
	if old := l.peers[id]; old != nil {
		l.mu.Unlock()
		l.Down(id)
		l.mu.Lock()
	}
	l.peers[id] = p
	n := len(l.peers)
	l.mu.Unlock()
	p.out <- hello
	go l.writer(p, write)
	l.host.PeerUp(router.PeerID(id))
	l.host.Status(peerCount(n))
}

func (l *Link) writer(p *peer, write func([]byte) error) {
	for {
		select {
		case <-p.done:
			return
		case b := <-p.out:
			if err := write(b); err != nil {
				l.Down(p.id)
				return
			}
		}
	}
}

// Down removes a peer (disconnected, a write failed, or a corrupt stream).
func (l *Link) Down(id string) {
	l.mu.Lock()
	p := l.peers[id]
	delete(l.peers, id)
	n := len(l.peers)
	l.mu.Unlock()
	if p == nil {
		return
	}
	close(p.done)
	if p.close != nil {
		p.close()
	}
	l.host.PeerDown(router.PeerID(id))
	l.host.Status(peerCount(n))
}

// Send frames raw for the peer and queues it in write-sized pieces.
func (l *Link) Send(to router.PeerID, raw []byte) {
	if len(raw) == 0 || len(raw) > packet.MaxSize {
		return
	}
	l.mu.Lock()
	p := l.peers[string(to)]
	l.mu.Unlock()
	if p == nil {
		return
	}
	buf := append([]byte{byte(len(raw) >> 16), byte(len(raw) >> 8), byte(len(raw))}, raw...)
	for len(buf) > 0 {
		k := min(p.chunk, len(buf))
		select {
		case p.out <- buf[:k:k]:
		default:
			l.Down(p.id) // a peer that stopped reading
			return
		}
		buf = buf[k:]
	}
}

var errFrame = errors.New("blemesh: bad frame length")

// Receive feeds one notification from a peer; a corrupt stream drops it.
func (l *Link) Receive(id string, chunk []byte) error {
	l.mu.Lock()
	p := l.peers[id]
	if p == nil {
		l.mu.Unlock()
		return fmt.Errorf("blemesh: chunk from unknown peer %q", id)
	}
	buf := append(p.rx, chunk...)
	var pkts [][]byte
	var err error
	for len(buf) >= frameHeader {
		n := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
		if n == 0 || n > packet.MaxSize {
			err = errFrame
			buf = nil
			break
		}
		if len(buf) < frameHeader+n {
			break
		}
		pkts = append(pkts, append([]byte(nil), buf[frameHeader:frameHeader+n]...))
		buf = buf[frameHeader+n:]
	}
	p.rx = append([]byte(nil), buf...)
	l.mu.Unlock()
	for _, raw := range pkts {
		l.host.Inbound(LinkName, router.PeerID(id), raw)
	}
	if err != nil {
		l.Down(id)
	}
	return err
}

func (l *Link) ids() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.peers))
	for id := range l.peers {
		out = append(out, id)
	}
	return out
}

// Hello is the first write: magic + 4-byte session ID.
func Hello(session [4]byte) []byte { return append([]byte{helloMagic}, session[:]...) }

func peerCount(n int) string {
	switch n {
	case 0:
		return "no peers"
	case 1:
		return "1 peer"
	}
	return fmt.Sprintf("%d peers", n)
}

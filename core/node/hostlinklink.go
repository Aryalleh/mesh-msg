package node

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// LoRaConfig attaches a LoRa node over USB serial (docs/firmware.md §2b).
type LoRaConfig struct {
	Enabled bool   `toml:"enabled" json:"enabled"`
	Port    string `toml:"port" json:"port"` // e.g. /dev/ttyACM0
}

// PortOpener opens the byte stream to a node: a serial port, a BLE
// characteristic pair on Android, or a pipe in tests.
type PortOpener func() (io.ReadWriteCloser, error)

// LoRaState is what the host knows about an attached node.
type LoRaState struct {
	Name      string               `json:"name"`
	Connected bool                 `json:"connected"`
	Error     string               `json:"error,omitempty"`
	Node      string               `json:"node,omitempty"` // node identity hash
	Status    *hostlink.Status     `json:"status,omitempty"`
	Config    *hostlink.NodeConfig `json:"config,omitempty"`
	Queued    int                  `json:"queued"` // packets waiting to be paced out
	Sent      int                  `json:"sent"`
	Received  int                  `json:"received"`
	BadFrames int                  `json:"bad_frames"`
}

// loraRetry is how long to wait before reopening a failed port.
var loraRetry = 5 * time.Second

const (
	loraQueueMax = 512

	loraStatusEvery = 30 * time.Second
	loraReplyWait   = 5 * time.Second
)

// hostLinkLink is a router.Broadcast link to a LoRa node speaking
// core/hostlink. Packets for the air are queued by priority and paced to the
// node's duty-cycle budget; packets the node heard come back as Inbound.
type hostLinkLink struct {
	n    *Node
	name string
	open PortOpener

	mu      sync.Mutex
	st      LoRaState
	queue   [][]byte
	notify  chan struct{}
	port    io.ReadWriteCloser
	wmu     sync.Mutex
	req     byte
	waiters map[byte]chan hostlink.Message
}

func newHostLinkLink(n *Node, name string, open PortOpener) *hostLinkLink {
	return &hostLinkLink{
		n: n, name: name, open: open, notify: make(chan struct{}, 1),
		st: LoRaState{Name: name}, waiters: map[byte]chan hostlink.Message{},
	}
}

func (l *hostLinkLink) Name() string          { return l.name }
func (l *hostLinkLink) Kind() router.LinkKind { return router.Broadcast }
func (l *hostLinkLink) MTU() int              { return packet.LoRaMTU }

func txPriority(raw []byte) int {
	p, err := packet.Decode(raw)
	if err != nil {
		return 9
	}
	switch p.Type {
	case packet.TypeReceipt:
		return 0
	case packet.TypeData:
		if p.Priority() {
			return 1
		}
		return 2
	case packet.TypeAnnounce:
		return 4
	}
	return 3
}

// Send queues raw for the air (called by the router).
func (l *hostLinkLink) Send(_ router.PeerID, raw []byte) {
	l.mu.Lock()
	if len(l.queue) >= loraQueueMax {
		// Drop the least urgent, newest packet (possibly raw itself).
		worst := len(l.queue) - 1
		for i := len(l.queue) - 1; i >= 0; i-- {
			if txPriority(l.queue[i]) > txPriority(l.queue[worst]) {
				worst = i
			}
		}
		if txPriority(raw) >= txPriority(l.queue[worst]) {
			l.mu.Unlock()
			return
		}
		l.queue = append(l.queue[:worst], l.queue[worst+1:]...)
	}
	// Insert after every packet at least as urgent (stable priority order).
	pr := txPriority(raw)
	i := len(l.queue)
	for i > 0 && txPriority(l.queue[i-1]) > pr {
		i--
	}
	l.queue = append(l.queue, nil)
	copy(l.queue[i+1:], l.queue[i:])
	l.queue[i] = append([]byte(nil), raw...)
	l.st.Queued = len(l.queue)
	l.mu.Unlock()
	select {
	case l.notify <- struct{}{}:
	default:
	}
}

func (l *hostLinkLink) state() LoRaState {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.st
	if s.Status != nil {
		c := *s.Status
		s.Status = &c
	}
	if s.Config != nil {
		c := *s.Config
		s.Config = &c
	}
	return s
}

func (l *hostLinkLink) write(kind byte, payload []byte) error {
	l.mu.Lock()
	port := l.port
	l.mu.Unlock()
	if port == nil {
		return errors.New("lora: not connected")
	}
	b, err := hostlink.Encode(kind, payload)
	if err != nil {
		return err
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	_, err = port.Write(b)
	return err
}

// command sends a command and, if wait, returns the reply event.
func (l *hostLinkLink) command(op byte, args []byte, wait bool) (hostlink.Message, error) {
	l.mu.Lock()
	l.req++
	if l.req == 0 {
		l.req = 1
	}
	req := l.req
	var ch chan hostlink.Message
	if wait {
		ch = make(chan hostlink.Message, 1)
		l.waiters[req] = ch
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.waiters, req)
		l.mu.Unlock()
	}()
	if err := l.write(hostlink.KindCommand, hostlink.Command(op, req, args)); err != nil || !wait {
		return hostlink.Message{}, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-time.After(loraReplyWait):
		return hostlink.Message{}, errors.New("lora: node did not reply")
	}
}

func (l *hostLinkLink) setError(err error) {
	l.mu.Lock()
	l.st.Connected = false
	l.st.Error = err.Error()
	l.mu.Unlock()
	l.n.setLinkStatus(l.name, "disconnected: "+err.Error())
}

func (l *hostLinkLink) run(ctx context.Context) {
	for ctx.Err() == nil {
		port, err := l.open()
		if err != nil {
			l.setError(err)
		} else {
			err = l.session(ctx, port)
			if ctx.Err() == nil && err != nil {
				l.setError(err)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(loraRetry):
		}
	}
}

func (l *hostLinkLink) session(ctx context.Context, port io.ReadWriteCloser) error {
	l.mu.Lock()
	l.port = port
	l.st.Connected, l.st.Error = true, ""
	l.mu.Unlock()
	defer func() {
		port.Close()
		l.mu.Lock()
		l.port = nil
		l.st.Connected = false
		l.mu.Unlock()
	}()

	readErr := make(chan error, 1)
	go func() { readErr <- l.readLoop(port) }()
	go func() {
		<-ctx.Done()
		port.Close()
	}()

	// Introduce ourselves: the node learns the time (for its own announces),
	// then streams what it stored while we were away.
	hello := binary.BigEndian.AppendUint32(nil, uint32(time.Now().Unix()))
	if _, err := l.command(hostlink.OpHello, hello, false); err != nil {
		return err
	}
	go func() {
		l.command(hostlink.OpGetIdentity, nil, true)
		l.command(hostlink.OpGetConfig, nil, true)
		l.n.setLinkStatus(l.name, l.describe())
		l.n.router.LinkUp(l.name) // stored packets this node hasn't carried yet
	}()

	statusT := time.NewTicker(loraStatusEvery)
	defer statusT.Stop()
	var nextSend time.Time
	for {
		var wait <-chan time.Time
		l.mu.Lock()
		pending := len(l.queue) > 0
		l.mu.Unlock()
		if pending {
			wait = time.After(time.Until(nextSend))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-statusT.C:
			go l.command(hostlink.OpGetStatus, nil, false)
		case <-l.notify:
		case <-wait:
			l.mu.Lock()
			if len(l.queue) == 0 {
				l.mu.Unlock()
				continue
			}
			raw := l.queue[0]
			l.queue = l.queue[1:]
			l.st.Queued = len(l.queue)
			cfg := hostlink.DefaultNodeConfig()
			if l.st.Config != nil {
				cfg = *l.st.Config
			}
			l.mu.Unlock()
			if err := l.write(hostlink.KindPacket, raw); err != nil {
				l.Send("", raw) // back in the queue for the next session
				return err
			}
			l.mu.Lock()
			l.st.Sent++
			l.mu.Unlock()
			nextSend = time.Now().Add(hostlink.SendInterval(cfg, len(raw)))
		}
	}
}

func (l *hostLinkLink) readLoop(port io.Reader) error {
	var dec hostlink.Decoder
	buf := make([]byte, 1024)
	for {
		n, err := port.Read(buf)
		if n > 0 {
			for _, f := range dec.Feed(buf[:n]) {
				l.frame(f)
			}
			l.mu.Lock()
			l.st.BadFrames = dec.Errors
			l.mu.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				return errors.New("lora: port closed")
			}
			return err
		}
	}
}

func (l *hostLinkLink) frame(f hostlink.Frame) {
	switch f.Kind {
	case hostlink.KindPacket:
		l.mu.Lock()
		l.st.Received++
		l.mu.Unlock()
		l.n.router.Inbound(l.name, "", f.Payload)
	case hostlink.KindEvent:
		m, err := hostlink.ParseMessage(f.Payload)
		if err != nil {
			return
		}
		l.event(m)
	}
}

func (l *hostLinkLink) event(m hostlink.Message) {
	l.mu.Lock()
	switch m.Op {
	case hostlink.EvStatus:
		if s, err := hostlink.ParseStatus(m.Data); err == nil {
			l.st.Status = &s
		}
	case hostlink.EvConfig:
		c := hostlink.DefaultNodeConfig()
		if err := c.ApplyTLV(m.Data); err == nil {
			l.st.Config = &c
		}
	case hostlink.EvIdentity:
		if pub, err := identity.PublicFromBytes(m.Data); err == nil {
			l.st.Node = pub.Hash().String()
		}
	case hostlink.EvLog:
		l.n.cfg.Logf("%s: node: %s", l.name, string(m.Data))
	}
	ch := l.waiters[m.Req]
	l.mu.Unlock()
	if ch != nil && m.Req != 0 {
		select {
		case ch <- m:
		default:
		}
	}
}

func (l *hostLinkLink) describe() string {
	s := l.state()
	if !s.Connected {
		return "disconnected"
	}
	if s.Status == nil {
		return "connected"
	}
	st := s.Status
	region, preset := "?", "?"
	if int(st.Region) < len(hostlink.Regions) {
		region = hostlink.Regions[st.Region].Name
	}
	if int(st.Preset) < len(hostlink.Presets) {
		preset = hostlink.Presets[st.Preset].Name
	}
	return fmt.Sprintf("node %s, %s %.3f MHz %s, %d neighbours, air %d/%d ms",
		short(s.Node), region, float64(st.FreqHz)/1e6, preset, st.Neighbours, st.AirUsedMs, st.AirBudgetMs)
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// configure applies (possibly partial) config TLVs on the node.
func (l *hostLinkLink) configure(tlv []byte) error {
	m, err := l.command(hostlink.OpSetConfig, tlv, true)
	if err != nil {
		return err
	}
	if m.Op != hostlink.EvAck || len(m.Data) != 2 {
		return errors.New("lora: unexpected reply")
	}
	if m.Data[1] != hostlink.AckOK {
		return fmt.Errorf("lora: node rejected config (status %d)", m.Data[1])
	}
	_, err = l.command(hostlink.OpGetConfig, nil, true)
	l.n.setLinkStatus(l.name, l.describe())
	return err
}

// ---- Node API ----

// AttachLoRa runs a LoRa node link over open until ctx is cancelled. It is
// used for the configured serial port and by platforms (Android BLE, tests).
func (n *Node) AttachLoRa(ctx context.Context, name string, open PortOpener) {
	l := newHostLinkLink(n, name, open)
	n.mu.Lock()
	n.loras[name] = l
	n.mu.Unlock()
	n.router.AddLink(l)
	n.setLinkStatus(name, "connecting")
	l.run(ctx)
	n.router.RemoveLink(name)
	n.mu.Lock()
	delete(n.loras, name)
	n.mu.Unlock()
}

// LoRa returns the state of every attached LoRa node.
func (n *Node) LoRa() []LoRaState {
	n.mu.Lock()
	ls := make([]*hostLinkLink, 0, len(n.loras))
	for _, l := range n.loras {
		ls = append(ls, l)
	}
	n.mu.Unlock()
	out := make([]LoRaState, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.state())
	}
	return out
}

// LoRaConfigure changes settings of an attached node. Only non-nil fields
// of the patch are sent.
func (n *Node) LoRaConfigure(name string, patch LoRaPatch) error {
	n.mu.Lock()
	l := n.loras[name]
	n.mu.Unlock()
	if l == nil {
		return fmt.Errorf("lora: no node %q", name)
	}
	tlv, err := patch.tlv()
	if err != nil {
		return err
	}
	return l.configure(tlv)
}

// LoRaPatch is a partial node configuration (JSON from the UI / CLI).
type LoRaPatch struct {
	Role        *string `json:"role,omitempty"`   // client | repeater | gateway
	Region      *string `json:"region,omitempty"` // e.g. EU_868
	Preset      *string `json:"preset,omitempty"` // FAST | BALANCED | LONG
	FreqHz      *uint32 `json:"freq_hz,omitempty"`
	TxDBm       *int8   `json:"tx_dbm,omitempty"`
	Name        *string `json:"name,omitempty"`
	AnnounceMin *uint16 `json:"announce_min,omitempty"`
}

func (p LoRaPatch) tlv() ([]byte, error) {
	var b []byte
	if p.Role != nil {
		roles := map[string]byte{"client": hostlink.RoleClient, "repeater": hostlink.RoleRepeater, "gateway": hostlink.RoleGateway}
		r, ok := roles[*p.Role]
		if !ok {
			return nil, fmt.Errorf("lora: unknown role %q", *p.Role)
		}
		b = append(b, hostlink.TagRole, 1, r)
	}
	if p.Region != nil {
		r, ok := hostlink.RegionByName(*p.Region)
		if !ok {
			return nil, fmt.Errorf("lora: unknown region %q", *p.Region)
		}
		b = append(b, hostlink.TagRegion, 1, r.ID)
	}
	if p.Preset != nil {
		found := false
		for _, ps := range hostlink.Presets {
			if ps.Name == *p.Preset {
				b = append(b, hostlink.TagPreset, 1, ps.ID)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("lora: unknown preset %q", *p.Preset)
		}
	}
	if p.FreqHz != nil {
		b = append(b, hostlink.TagFreqHz, 4)
		b = binary.BigEndian.AppendUint32(b, *p.FreqHz)
	}
	if p.TxDBm != nil {
		b = append(b, hostlink.TagTxDBm, 1, byte(*p.TxDBm))
	}
	if p.Name != nil {
		if len(*p.Name) > hostlink.MaxNameLen {
			return nil, errors.New("lora: name longer than 32 bytes")
		}
		b = append(b, hostlink.TagName, byte(len(*p.Name)))
		b = append(b, *p.Name...)
	}
	if p.AnnounceMin != nil {
		b = append(b, hostlink.TagAnnounceMin, 2)
		b = binary.BigEndian.AppendUint16(b, *p.AnnounceMin)
	}
	if len(b) == 0 {
		return nil, errors.New("lora: nothing to change")
	}
	return b, nil
}

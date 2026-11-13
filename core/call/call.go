package call

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/identity"
)

// Call states.
const (
	StateOutgoing   = "outgoing"   // offered, the other phone rings
	StateRinging    = "ringing"    // incoming, ringing here
	StateConnecting = "connecting" // answered, finding a path
	StateActive     = "active"
	StateEnded      = "ended"
)

const (
	ringFor      = 45 * time.Second
	pathTimeout  = 10 * time.Second // calls.md §3: no path within 10 s ends the call
	silentEnd    = 20 * time.Second // no frame at all from the other side
	keepalive    = 2 * time.Second
	audioPeriod  = 20 * time.Millisecond
	helloRepeat  = 250 * time.Millisecond
	maxEndedKept = 8
)

// Env connects the manager to the node.
type Env struct {
	Self *identity.Identity
	// Send delivers a signal to the peer (content type 7, any path).
	Send func(peer identity.Public, sig []byte) error
	// Direct gives the path configuration (reflectors, port mapping, TURN).
	Direct func(ctx context.Context) direct.Config
	// Allowed decides whether peer may call (default: contacts only).
	Allowed func(peer identity.Public) bool
	// Event reports every state change.
	Event func(Info)
	Logf  func(format string, args ...any)
	Now   func() time.Time
}

// Info is what the UI shows.
type Info struct {
	ID       string `json:"id"`
	Peer     string `json:"peer"` // contact hash
	Incoming bool   `json:"incoming"`
	State    string `json:"state"`
	Codec    int    `json:"codec,omitempty"`
	Code     string `json:"code,omitempty"`    // compare by voice
	Path     string `json:"path,omitempty"`    // how the audio goes
	Relayed  bool   `json:"relayed,omitempty"` // through a relay's TURN (admins)
	// PathClass: connectivity.md §5, 1 local network … 5 peer to peer across
	// NAT, 6 through a relay; 0 while no path is chosen yet.
	PathClass int    `json:"path_class,omitempty"`
	Reason    string `json:"reason,omitempty"` // why it ended
	Missed    bool   `json:"missed,omitempty"`
	Started   int64  `json:"started,omitempty"` // unix ms when it became active
	Muted     bool   `json:"peer_muted,omitempty"`
	// Video: both sides can send video (camera on or off); VideoCall: the
	// caller started it with the camera on.
	Video      bool `json:"video,omitempty"`
	VideoCodec int  `json:"video_codec,omitempty"`
	VideoCall  bool `json:"video_call,omitempty"`
}

// Manager runs this device's calls (one at a time).
type Manager struct {
	env Env

	mu    sync.Mutex
	calls map[[16]byte]*Call
}

// NewManager makes a manager.
func NewManager(env Env) *Manager {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Logf == nil {
		env.Logf = func(string, ...any) {}
	}
	if env.Event == nil {
		env.Event = func(Info) {}
	}
	return &Manager{env: env, calls: map[[16]byte]*Call{}}
}

// Call is one call.
type Call struct {
	m        *Manager
	id       [16]byte
	peer     identity.Public
	incoming bool
	caps     Caps // ours
	offer    Signal
	key      [32]byte

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	state   string
	codec   int
	reason  string
	missed  bool
	started time.Time
	answer  chan Signal
	agent   *direct.Agent
	link    *direct.Link
	cr      *crypto
	seq     uint32
	t0      time.Time
	lastRx  time.Time
	jitter  *Jitter
	peerMut bool
	// video, when both sides offered it with a common codec
	video      *video
	videoCodec int
}

// ErrBusy: there is already a call.
var ErrBusy = errors.New("call: already in a call")

func (m *Manager) live() *Call {
	for _, c := range m.calls {
		if c.State() != StateEnded {
			return c
		}
	}
	return nil
}

// Current is the call in progress, if any.
func (m *Manager) Current() *Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live()
}

// Get finds a call by id.
func (m *Manager) Get(id string) *Call {
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 16 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[[16]byte(b)]
}

func (m *Manager) newCall(id [16]byte, peer identity.Public, incoming bool, caps Caps) *Call {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Call{m: m, id: id, peer: peer, incoming: incoming, caps: caps, ctx: ctx, cancel: cancel,
		answer: make(chan Signal, 1), jitter: NewJitter(audioPeriod)}
	// Forget old ended calls.
	if len(m.calls) >= maxEndedKept {
		for k, o := range m.calls {
			if o.State() == StateEnded {
				delete(m.calls, k)
			}
		}
	}
	m.calls[id] = c
	return c
}

// Start calls peer with our caps (main audio stream 0, codecs in order of
// preference).
func (m *Manager) Start(peer identity.Public, caps Caps) (*Call, error) {
	m.mu.Lock()
	if m.live() != nil {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	var id [16]byte
	rand.Read(id[:])
	c := m.newCall(id, peer, false, caps)
	rand.Read(c.key[:])
	c.state = StateOutgoing
	m.mu.Unlock()
	m.env.Event(c.Info())
	go c.runCaller()
	return c, nil
}

// OnSignal handles a call signal from peer.
func (m *Manager) OnSignal(from identity.Public, b []byte) {
	s, err := Decode(b)
	if err != nil {
		return // unknown ops and broken signals are ignored
	}
	m.mu.Lock()
	c := m.calls[s.CallID]
	if c != nil && c.peer != from {
		m.mu.Unlock()
		return
	}
	if s.Op == OpOffer {
		if c != nil {
			m.mu.Unlock()
			return // a repeat
		}
		c = m.newCall(s.CallID, from, true, Caps{})
		c.offer, c.key = s, s.Key
		now := m.env.Now()
		expired := time.Unix(int64(s.Expires), 0).Before(now)
		busy := false
		for _, o := range m.calls {
			if o != c && o.State() != StateEnded {
				busy = true
			}
		}
		switch {
		case m.env.Allowed != nil && !m.env.Allowed(from):
			c.state, c.reason = StateEnded, "not a contact"
			m.mu.Unlock()
			m.send(c, Signal{CallID: s.CallID, Op: OpReject, Reason: RejectNotContact})
			return
		case expired:
			// Delivered late (stored by a relay): a missed call, never rings.
			c.state, c.reason, c.missed = StateEnded, "missed", true
		case busy:
			c.state, c.reason, c.missed = StateEnded, "busy", true
			m.mu.Unlock()
			m.send(c, Signal{CallID: s.CallID, Op: OpReject, Reason: RejectBusy})
			m.env.Event(c.Info())
			return
		default:
			c.state = StateRinging
			go c.ringTimeout()
		}
		m.mu.Unlock()
		m.env.Event(c.Info())
		return
	}
	m.mu.Unlock()
	if c == nil {
		return
	}
	switch s.Op {
	case OpAnswer:
		if !c.incoming {
			select {
			case c.answer <- s:
			default:
			}
		}
	case OpReject:
		why := map[byte]string{RejectDeclined: "declined", RejectBusy: "busy", RejectNotContact: "not accepted"}[s.Reason]
		c.end(orDefault(why, "declined"), false)
	case OpCancel:
		c.mu.Lock()
		missed := c.state == StateRinging
		c.missed = missed
		c.mu.Unlock()
		c.end("missed", false)
	case OpEnd:
		why := map[byte]string{EndHangup: "hung up", EndNoPath: "no direct path", EndTimeout: "timed out", EndError: "error"}[s.Reason]
		c.end(orDefault(why, "ended"), false)
	case OpControl:
		if s.Stream == 0 && s.Kind == 2 && len(s.Body) > 0 { // mute state
			c.mu.Lock()
			c.peerMut = s.Body[0] != 0
			c.mu.Unlock()
			m.env.Event(c.Info())
		}
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (m *Manager) send(c *Call, s Signal) {
	if err := m.env.Send(c.peer, s.Encode()); err != nil {
		m.env.Logf("call: signal: %v", err)
	}
}

// Close ends every call (the app is stopping).
func (m *Manager) Close() {
	m.mu.Lock()
	var cs []*Call
	for _, c := range m.calls {
		cs = append(cs, c)
	}
	m.mu.Unlock()
	for _, c := range cs {
		c.Hangup()
	}
}

// ---- one call ----

// ID is the call id (hex).
func (c *Call) ID() string { return hex.EncodeToString(c.id[:]) }

// State is the current state.
func (c *Call) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Info describes the call for the UI.
func (c *Call) Info() Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	in := Info{ID: hex.EncodeToString(c.id[:]), Peer: c.peer.Hash().String(), Incoming: c.incoming, State: c.state,
		Codec: c.codec, Reason: c.reason, Missed: c.missed, Muted: c.peerMut,
		Video: c.video != nil, VideoCodec: c.videoCodec}
	// A "video call": the caller starts with the camera on.
	if c.incoming {
		if v, ok := c.offer.Caps.Video(); ok {
			in.VideoCall = ParseVideoParams(v.Params).CameraOn
		}
	} else if v, ok := c.caps.Video(); ok {
		in.VideoCall = ParseVideoParams(v.Params).CameraOn
	}
	if c.cr != nil {
		in.Code = c.cr.code()
	}
	if c.link != nil {
		p := c.link.Path()
		in.Path, in.Relayed, in.PathClass = p.String(), p.Relayed(), p.Class
	}
	if !c.started.IsZero() {
		in.Started = c.started.UnixMilli()
	}
	return in
}

// Offered is the caller's caps (for an incoming call).
func (c *Call) Offered() Caps { return c.offer.Caps }

func (c *Call) setState(s string) {
	c.mu.Lock()
	if c.state == StateEnded {
		c.mu.Unlock()
		return
	}
	c.state = s
	if s == StateActive {
		c.started = c.m.env.Now()
	}
	c.mu.Unlock()
	c.m.env.Event(c.Info())
}

// end finishes the call; tell sends END/CANCEL to the other side.
func (c *Call) end(reason string, tell bool) {
	c.mu.Lock()
	if c.state == StateEnded {
		c.mu.Unlock()
		return
	}
	prev := c.state
	c.state, c.reason = StateEnded, reason
	link, agent := c.link, c.agent
	c.mu.Unlock()
	if tell {
		switch {
		case prev == StateOutgoing:
			c.m.send(c, Signal{CallID: c.id, Op: OpCancel})
		case prev == StateRinging:
			c.m.send(c, Signal{CallID: c.id, Op: OpReject, Reason: RejectDeclined})
		default:
			r := byte(EndHangup)
			switch reason {
			case "no direct path":
				r = EndNoPath
			case "timed out":
				r = EndTimeout
			case "error":
				r = EndError
			}
			c.m.send(c, Signal{CallID: c.id, Op: OpEnd, Reason: r})
		}
	}
	c.cancel()
	if link != nil {
		link.Close()
	} else if agent != nil {
		agent.Close()
	}
	c.m.env.Event(c.Info())
}

// Hangup ends the call (or cancels / declines it).
func (c *Call) Hangup() {
	c.end("hung up", true)
}

// Reject declines an incoming call.
func (c *Call) Reject() { c.end("declined", true) }

// Answer accepts an incoming call with our caps; the codec is the first of
// the caller's audio codecs that we also have.
func (c *Call) Answer(caps Caps) error {
	c.mu.Lock()
	if c.state != StateRinging {
		c.mu.Unlock()
		return errors.New("call: not ringing")
	}
	theirs, _ := c.offer.Caps.Audio()
	ours, _ := caps.Audio()
	codec := pick(theirs.Codecs, ours.Codecs)
	if codec == 0 {
		c.mu.Unlock()
		c.end("no common codec", true)
		return errors.New("call: no common audio codec")
	}
	c.codec = codec
	ours.Codecs = []byte{byte(codec)}
	c.caps = Caps{Version: Version, Streams: []StreamCap{ours}}
	tv, ok1 := c.offer.Caps.Video()
	ov, ok2 := caps.Video()
	if vc := pick(tv.Codecs, ov.Codecs); ok1 && ok2 && vc != 0 {
		ov.Codecs = []byte{byte(vc)}
		c.caps.Streams = append(c.caps.Streams, ov)
		c.video, c.videoCodec = newVideo(), vc
	}
	c.state = StateConnecting
	c.mu.Unlock()
	c.m.env.Event(c.Info())
	go c.runCallee()
	return nil
}

func pick(pref, have []byte) int {
	for _, p := range pref {
		for _, h := range have {
			if p == h {
				return int(p)
			}
		}
	}
	return 0
}

func (c *Call) ringTimeout() {
	select {
	case <-time.After(ringFor):
		c.mu.Lock()
		ringing := c.state == StateRinging
		if ringing {
			c.missed = true
		}
		c.mu.Unlock()
		if ringing {
			c.end("missed", false)
		}
	case <-c.ctx.Done():
	}
}

func (c *Call) runCaller() {
	a, err := direct.NewAgent(c.m.env.Direct(c.ctx), c.id, c.key, true)
	if err != nil {
		c.end("error", true)
		return
	}
	c.mu.Lock()
	c.agent = a
	c.mu.Unlock()
	mine := a.Gather(c.ctx)
	c.m.send(c, Signal{CallID: c.id, Op: OpOffer, Expires: uint32(c.m.env.Now().Add(ringFor).Unix()), Caps: c.caps, Key: c.key, Offer: mine})
	var ans Signal
	select {
	case ans = <-c.answer:
	case <-time.After(ringFor):
		c.end("no answer", true)
		return
	case <-c.ctx.Done():
		return
	}
	theirs, _ := ans.Caps.Audio()
	if len(theirs.Codecs) == 0 {
		c.end("no common codec", true)
		return
	}
	c.mu.Lock()
	c.codec = int(theirs.Codecs[0])
	// Video if we offered it and the answer kept it (an audio-only app
	// leaves it out: the call runs as voice).
	if tv, ok := ans.Caps.Video(); ok && len(tv.Codecs) > 0 {
		if ov, ok := c.caps.Video(); ok && pick(tv.Codecs[:1], ov.Codecs) != 0 {
			c.video, c.videoCodec = newVideo(), int(tv.Codecs[0])
		}
	}
	c.mu.Unlock()
	c.setState(StateConnecting)
	c.connect(a, ans.Offer)
}

func (c *Call) runCallee() {
	a, err := direct.NewAgent(c.m.env.Direct(c.ctx), c.id, c.key, false)
	if err != nil {
		c.end("error", true)
		return
	}
	c.mu.Lock()
	c.agent = a
	c.mu.Unlock()
	mine := a.Gather(c.ctx)
	c.m.send(c, Signal{CallID: c.id, Op: OpAnswer, Caps: c.caps, Offer: mine})
	c.connect(a, c.offer.Offer)
}

// connect finds the path, runs the handshake and starts the media loops.
func (c *Call) connect(a *direct.Agent, remote direct.Offer) {
	ctx, cancel := context.WithTimeout(c.ctx, pathTimeout+20*time.Second) // + symmetric-NAT methods
	defer cancel()
	l, err := a.Connect(ctx, remote)
	if err != nil {
		if c.ctx.Err() == nil {
			c.m.env.Logf("call: %v", err)
			c.end("no direct path", true)
		}
		return
	}
	cr, err := newCrypto(c.m.env.Self, c.peer, c.id, !c.incoming)
	if err != nil {
		c.end("error", true)
		return
	}
	c.mu.Lock()
	if c.state == StateEnded {
		c.mu.Unlock()
		l.Close()
		return
	}
	c.link, c.cr, c.t0, c.lastRx = l, cr, time.Now(), time.Now()
	c.mu.Unlock()
	go c.recvLoop(l, cr)
	// Handshake: the caller repeats message 1 until the answer comes.
	deadline := time.Now().Add(pathTimeout)
	for !cr.ready() {
		if cr.initiator {
			l.Send(cr.hello1)
		}
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(helloRepeat):
		}
		if time.Now().After(deadline) {
			c.end("error", true)
			return
		}
	}
	c.setState(StateActive)
	go c.keepaliveLoop(l, cr)
}

func (c *Call) recvLoop(l *direct.Link, cr *crypto) {
	for {
		select {
		case b, ok := <-l.Recv():
			if !ok {
				return
			}
			if len(b) == 0 {
				continue
			}
			switch b[0] {
			case dgHello1, dgHello2:
				reply, _, err := cr.onHello(b)
				if err != nil {
					c.m.env.Logf("call: %v", err)
					continue
				}
				if reply != nil {
					l.Send(reply)
				}
			case dgFrame:
				if !cr.ready() {
					continue
				}
				f, err := cr.open(b)
				if err != nil {
					continue
				}
				now := time.Now()
				c.mu.Lock()
				c.lastRx = now
				j := c.jitter
				c.mu.Unlock()
				switch {
				case f.Stream == 0 && len(f.Payload) > 0:
					j.Push(f, now)
				case f.Stream == StreamVideo:
					c.onVideo(f, now)
				case f.Stream == streamControl:
					c.onControl(f)
				}
			}
		case <-l.Done():
			c.end("connection lost", false)
			return
		case <-c.ctx.Done():
			return
		}
	}
}

// keepaliveLoop sends empty frames so NAT mappings stay open in silence, and
// ends a call whose other side went silent.
func (c *Call) keepaliveLoop(l *direct.Link, cr *crypto) {
	t := time.NewTicker(keepalive)
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
		}
		c.mu.Lock()
		silent := time.Since(c.lastRx)
		c.mu.Unlock()
		if silent > silentEnd {
			c.end("connection lost", true)
			return
		}
		l.Send(cr.seal(Frame{Stream: 0, Seq: c.nextSeq(), TS: c.ts()}))
	}
}

func (c *Call) nextSeq() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *Call) ts() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return uint32(time.Since(c.t0).Milliseconds())
}

// ErrNotActive: audio only flows in an active call.
var ErrNotActive = errors.New("call: not active")

// SendAudio sends one encoded audio frame (20 ms).
func (c *Call) SendAudio(payload []byte) error {
	c.mu.Lock()
	l, cr, st := c.link, c.cr, c.state
	c.mu.Unlock()
	if st != StateActive || l == nil {
		return ErrNotActive
	}
	if len(payload) > direct.MaxPayload-1-frameHead-16 {
		return errors.New("call: audio frame too big")
	}
	return l.Send(cr.seal(Frame{Stream: 0, Seq: c.nextSeq(), TS: c.ts(), Payload: payload}))
}

// PullAudio gives the next frame to play (call it every 20 ms).
func (c *Call) PullAudio() ([]byte, int) {
	c.mu.Lock()
	j := c.jitter
	c.mu.Unlock()
	f, r := j.Pull()
	return f.Payload, r
}

// Stats reports the jitter buffer counters and delay.
func (c *Call) Stats() (JitterStats, time.Duration) {
	c.mu.Lock()
	j := c.jitter
	c.mu.Unlock()
	j.mu.Lock()
	st := j.Stats
	j.mu.Unlock()
	return st, j.Delay()
}

// SetMuted tells the other side we muted (it shows it).
func (c *Call) SetMuted(muted bool) {
	b := byte(0)
	if muted {
		b = 1
	}
	c.m.send(c, Signal{CallID: c.id, Op: OpControl, Stream: 0, Kind: 2, Body: []byte{b}})
}

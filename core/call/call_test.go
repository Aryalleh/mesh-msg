package call

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/identity"
)

type phone struct {
	id     *identity.Identity
	m      *Manager
	events chan Info
}

// twoPhones wires two managers: signals go straight to the other side
// (standing in for the mesh), paths run over loopback.
func twoPhones(t *testing.T) (*phone, *phone) {
	a, b := &phone{events: make(chan Info, 64)}, &phone{events: make(chan Info, 64)}
	a.id, _ = identity.Generate()
	b.id, _ = identity.Generate()
	var mu sync.Mutex
	drop := false
	mk := func(p, other *phone) {
		p.m = NewManager(Env{
			Self: p.id,
			Send: func(peer identity.Public, sig []byte) error {
				mu.Lock()
				d := drop
				mu.Unlock()
				if !d {
					go other.m.OnSignal(p.id.Public, sig)
				}
				return nil
			},
			Direct: func(context.Context) direct.Config {
				return direct.Config{Net: direct.OSNet{Loopback: true}, CheckTimeout: 3 * time.Second}
			},
			Allowed: func(peer identity.Public) bool { return peer == other.id.Public },
			Event:   func(in Info) { p.events <- in },
		})
	}
	mk(a, b)
	mk(b, a)
	t.Cleanup(func() { a.m.Close(); b.m.Close() })
	return a, b
}

func waitState(t *testing.T, p *phone, state string) Info {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case in := <-p.events:
			if in.State == state {
				return in
			}
		case <-deadline:
			t.Fatalf("no %q state", state)
		}
	}
}

func voice(codecs ...byte) Caps {
	return Caps{Version: Version, Streams: []StreamCap{{ID: 0, Media: MediaAudio, Dir: DirBoth, Codecs: codecs, MaxKbps: 24}}}
}

func TestCallRingAnswerTalkHangup(t *testing.T) {
	a, b := twoPhones(t)
	ca, err := a.m.Start(b.id.Public, voice(CodecOpus, CodecAMRWB))
	if err != nil {
		t.Fatal(err)
	}
	ring := waitState(t, b, StateRinging)
	if !ring.Incoming || ring.Peer != a.id.Hash().String() {
		t.Fatalf("ring %+v", ring)
	}
	cb := b.m.Get(ring.ID)
	if err := cb.Answer(voice(CodecAMRWB)); err != nil { // an older phone: AMR-WB only
		t.Fatal(err)
	}
	ia, ib := waitState(t, a, StateActive), waitState(t, b, StateActive)
	if ia.Code == "" || ia.Code != ib.Code {
		t.Fatalf("codes %q %q", ia.Code, ib.Code)
	}
	if ia.Codec != CodecAMRWB || ib.Codec != CodecAMRWB {
		t.Fatalf("codec %d %d", ia.Codec, ib.Codec)
	}
	// 30 frames each way.
	for i := 0; i < 30; i++ {
		ca.SendAudio([]byte{byte(i), 'a'})
		cb.SendAudio([]byte{byte(i), 'b'})
		time.Sleep(audioPeriod)
	}
	time.Sleep(200 * time.Millisecond)
	for _, tc := range []struct {
		c    *Call
		from byte
	}{{cb, 'a'}, {ca, 'b'}} {
		// All 30 arrived at once here, so the buffer skips some to cut the
		// delay; what plays must be in order and nothing may be lost.
		got, last := 0, -1
		for i := 0; i < 60; i++ {
			p, r := tc.c.PullAudio()
			if r == PullFrame {
				if p[1] != tc.from || int(p[0]) <= last {
					t.Fatalf("frame %v after %d", p, last)
				}
				last = int(p[0])
				got++
			}
		}
		st, _ := tc.c.Stats()
		if got+st.Dropped != 30 || st.Lost != 0 {
			t.Fatalf("played %d, dropped %d, lost %d of 30", got, st.Dropped, st.Lost)
		}
	}
	ca.Hangup()
	if in := waitState(t, b, StateEnded); in.Reason != "hung up" {
		t.Fatalf("reason %q", in.Reason)
	}
}

func TestCallDeclinedBusyCancelled(t *testing.T) {
	a, b := twoPhones(t)
	// Declined.
	a.m.Start(b.id.Public, voice(CodecOpus))
	ring := waitState(t, b, StateRinging)
	b.m.Get(ring.ID).Reject()
	if in := waitState(t, a, StateEnded); in.Reason != "declined" {
		t.Fatalf("reason %q", in.Reason)
	}
	// Cancelled before an answer: a missed call on b.
	ca, _ := a.m.Start(b.id.Public, voice(CodecOpus))
	waitState(t, b, StateRinging)
	ca.Hangup()
	if in := waitState(t, b, StateEnded); !in.Missed {
		t.Fatalf("not missed: %+v", in)
	}
	// Busy: b is in a call with a (ringing), c calls b.
	a.m.Start(b.id.Public, voice(CodecOpus))
	waitState(t, b, StateRinging)
	c, _ := identity.Generate()
	var got []byte
	var mu sync.Mutex
	cm := NewManager(Env{Self: c, Send: func(_ identity.Public, s []byte) error { mu.Lock(); got = s; mu.Unlock(); return nil },
		Direct: func(context.Context) direct.Config { return direct.Config{Net: direct.OSNet{Loopback: true}} }})
	defer cm.Close()
	b.m.env.Allowed = nil // calls from anyone
	off := Signal{Op: OpOffer, Expires: uint32(time.Now().Add(time.Minute).Unix()), Caps: voice(CodecOpus)}
	off.CallID[0] = 9
	b.m.OnSignal(c.Public, off.Encode())
	mu.Lock()
	defer mu.Unlock()
	_ = got
	if in := b.m.Get("09000000000000000000000000000000").Info(); in.Reason != "busy" || !in.Missed {
		t.Fatalf("busy: %+v", in)
	}
}

func TestLateOfferIsMissedCall(t *testing.T) {
	_, b := twoPhones(t)
	a2 := b.m.env.Allowed
	b.m.env.Allowed = nil
	defer func() { b.m.env.Allowed = a2 }()
	x, _ := identity.Generate()
	s := Signal{Op: OpOffer, Expires: uint32(time.Now().Add(-time.Second).Unix()), Caps: voice(CodecOpus)}
	s.CallID[0] = 1
	b.m.OnSignal(x.Public, s.Encode())
	in := waitState(t, b, StateEnded)
	if !in.Missed {
		t.Fatalf("%+v", in)
	}
}

func TestSignalEncoding(t *testing.T) {
	o := direct.Offer{NAT: direct.NATInfo{Type: direct.NATIndep}}
	s := Signal{Op: OpOffer, Expires: 99, Caps: voice(CodecOpus, CodecAMRWB), Offer: o}
	s.Key[3], s.CallID[5] = 7, 8
	d, err := Decode(append(s.Encode(), 1, 2, 3)) // trailing bytes ignored
	if err != nil || d.Expires != 99 || d.Key != s.Key || d.CallID != s.CallID || !bytes.Equal(d.Caps.Streams[0].Codecs, []byte{1, 2}) {
		t.Fatalf("%v %+v", err, d)
	}
	if _, err := Decode(append(make([]byte, 16), 99)); err != ErrUnknownOp {
		t.Fatalf("unknown op: %v", err)
	}
	c := Signal{Op: OpControl, Stream: 0, Kind: 2, Body: []byte{1}}
	if d, _ := Decode(c.Encode()); d.Kind != 2 || d.Body[0] != 1 {
		t.Fatal("control")
	}
}

func TestJitterReorderLossAndUnderrun(t *testing.T) {
	j := NewJitter(audioPeriod)
	now := time.Now()
	push := func(seq uint32) {
		j.Push(Frame{Seq: seq, TS: seq * 20, Payload: []byte{byte(seq)}}, now.Add(time.Duration(seq)*audioPeriod))
	}
	// Out of order; 4 is lost.
	for _, s := range []uint32{1, 3, 2, 5, 6, 7} {
		push(s)
	}
	var seq []int
	for i := 0; i < 7; i++ {
		f, r := j.Pull()
		switch r {
		case PullFrame:
			seq = append(seq, int(f.Payload[0]))
		case PullLost:
			seq = append(seq, -1)
		case PullEmpty:
			seq = append(seq, 0)
		}
	}
	want := []int{1, 2, 3, -1, 5, 6, 7}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("played %v, want %v", seq, want)
		}
	}
	if _, r := j.Pull(); r != PullEmpty || j.Stats.Underruns != 1 || j.Stats.Lost != 1 {
		t.Fatalf("after the end: %d %+v", r, j.Stats)
	}
	push(2) // late
	if j.Stats.Late != 1 {
		t.Fatal("late frame not counted")
	}
}

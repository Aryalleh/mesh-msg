package call

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/identity"
)

// boardPeer runs the board's call manager (firmware/lib/meshcall, built as
// firmware/build/callpeer by `make -C firmware callpeer`) as a child
// process: signals go over its stdin/stdout, media over loopback UDP.
type boardPeer struct {
	pub    identity.Public
	in     io.WriteCloser
	mu     sync.Mutex
	states chan []string // state|name|reason|code|codec|class
	rx     atomic.Int64
	muted  atomic.Bool
}

func (b *boardPeer) cmd(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Fprintln(b.in, s)
}

func (b *boardPeer) wait(t *testing.T, state string) []string {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case f := <-b.states:
			if f[1] == state {
				return f
			}
		case <-deadline:
			t.Fatalf("board: no %q state", state)
		}
	}
}

// startBoard starts the board peer and a Go phone that knows it.
func startBoard(t *testing.T, bin string) (*boardPeer, *phone) {
	cmd := exec.Command(bin)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	b := &boardPeer{in: in, states: make(chan []string, 64)}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 64*1024), 64*1024)
	if !lines.Scan() || !strings.HasPrefix(lines.Text(), "id ") {
		t.Fatalf("board: no id line")
	}
	raw, _ := hex.DecodeString(strings.TrimPrefix(lines.Text(), "id "))
	pub, err := identity.PublicFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	b.pub = pub
	p := &phone{events: make(chan Info, 64)}
	p.id, _ = identity.Generate()
	p.m = NewManager(Env{
		Self: p.id,
		Send: func(peer identity.Public, sig []byte) error {
			b.cmd("sig " + hex.EncodeToString(sig))
			return nil
		},
		Direct: func(context.Context) direct.Config {
			return direct.Config{Net: direct.OSNet{Loopback: true}, CheckTimeout: 3 * time.Second}
		},
		Allowed: func(peer identity.Public) bool { return peer == pub },
		Event:   func(in Info) { p.events <- in },
	})
	t.Cleanup(p.m.Close)
	b.cmd("peer " + hex.EncodeToString(p.id.Public.Bytes()))
	go func() {
		for lines.Scan() {
			l := lines.Text()
			switch {
			case strings.HasPrefix(l, "sig "):
				sig, _ := hex.DecodeString(l[4:])
				p.m.OnSignal(pub, sig)
			case strings.HasPrefix(l, "state|"):
				b.states <- strings.Split(l, "|")
			case strings.HasPrefix(l, "rx "):
				n, _ := strconv.Atoi(l[3:])
				b.rx.Store(int64(n))
			case l == "muted":
				b.muted.Store(true)
			}
		}
	}()
	return b, p
}

// talkToBoard sends 1.5 s of phone audio and counts the board's frames.
func talkToBoard(t *testing.T, c *Call, b *boardPeer) {
	t.Helper()
	got := 0
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for i := 0; i < 75; i++ {
		<-tick.C
		if err := c.SendAudio([]byte(fmt.Sprintf("phone%04d", i))); err != nil {
			t.Fatal(err)
		}
		if f, r := c.PullAudio(); r == PullFrame && strings.HasPrefix(string(f), "board") {
			got++
		}
	}
	time.Sleep(200 * time.Millisecond)
	if got < 40 || b.rx.Load() < 25 {
		t.Fatalf("audio: phone got %d board frames, board got %d phone frames", got, b.rx.Load())
	}
}

// TestBoardPeer: a Go phone and the board's C++ call manager call each
// other, agree on the code, talk and hang up.
func TestBoardPeer(t *testing.T) {
	bin := os.Getenv("MAVA_CALLPEER")
	if bin == "" {
		t.Skip("MAVA_CALLPEER not set (make -C firmware callpeer; MAVA_CALLPEER=$PWD/firmware/build/callpeer)")
	}
	t.Run("phone calls board", func(t *testing.T) {
		b, p := startBoard(t, bin)
		c, err := p.m.Start(b.pub, voice(CodecAMRWB, CodecOpus))
		if err != nil {
			t.Fatal(err)
		}
		b.wait(t, "ringing")
		b.cmd("answer")
		ba := b.wait(t, "active")
		pa := waitState(t, p, StateActive)
		if pa.Code == "" || pa.Code != ba[3] {
			t.Fatalf("codes differ: phone %q board %q", pa.Code, ba[3])
		}
		if pa.Codec != CodecAMRWB || ba[4] != strconv.Itoa(CodecAMRWB) {
			t.Fatalf("codec: phone %d board %s", pa.Codec, ba[4])
		}
		talkToBoard(t, c, b)
		c.SetMuted(true)
		deadline := time.Now().Add(2 * time.Second)
		for !b.muted.Load() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !b.muted.Load() {
			t.Fatal("board did not see the mute")
		}
		c.Hangup()
		if e := b.wait(t, "ended"); e[2] != "hung up" {
			t.Fatalf("board ended: %q", e[2])
		}
	})
	t.Run("board calls phone", func(t *testing.T) {
		b, p := startBoard(t, bin)
		b.cmd("call")
		ring := waitState(t, p, StateRinging)
		if !ring.Incoming {
			t.Fatalf("ring %+v", ring)
		}
		c := p.m.Get(ring.ID)
		if err := c.Answer(voice(CodecAMRWB, CodecOpus)); err != nil {
			t.Fatal(err)
		}
		ba := b.wait(t, "active")
		pa := waitState(t, p, StateActive)
		if pa.Code == "" || pa.Code != ba[3] {
			t.Fatalf("codes differ: phone %q board %q", pa.Code, ba[3])
		}
		if pa.Codec != CodecOpus {
			t.Fatalf("codec %d", pa.Codec)
		}
		talkToBoard(t, c, b)
		b.cmd("mute 1")
		for {
			in := waitState(t, p, StateActive)
			if in.Muted {
				break
			}
		}
		b.cmd("hangup")
		if e := waitState(t, p, StateEnded); e.Reason != "hung up" {
			t.Fatalf("phone ended: %q", e.Reason)
		}
	})
}

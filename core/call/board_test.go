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
	pub     identity.Public
	in      io.WriteCloser
	mu      sync.Mutex
	states  chan []string // state|name|reason|code|codec|class
	rx      atomic.Int64
	muted   atomic.Bool
	videoMu sync.Mutex
	video   []string // "<bytes> <fnv-1a hex>" per frame the board took in
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
			case strings.HasPrefix(l, "video "):
				b.videoMu.Lock()
				b.video = append(b.video, l[6:])
				b.videoMu.Unlock()
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

func fnv32(b []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range b {
		h = (h ^ uint32(c)) * 16777619
	}
	return h
}

// sendPictures sends n "JPEG" frames from the phone and checks that the
// board took each one in whole.
func sendPictures(t *testing.T, c *Call, b *boardPeer, n int) {
	t.Helper()
	var want []string
	for i := 0; i < n; i++ {
		f := make([]byte, 15000+i*997)
		for j := range f {
			f[j] = byte(j*7 + i*13 + j>>8)
		}
		if err := c.SendVideo(f, true); err != nil {
			t.Fatal(err)
		}
		want = append(want, fmt.Sprintf("%d %08x", len(f), fnv32(f)))
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	b.videoMu.Lock()
	got := append([]string(nil), b.video...)
	b.videoMu.Unlock()
	if len(got) != n {
		t.Fatalf("board took %d video frames, want %d", len(got), n)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("frame %d: board %q, phone %q", i, got[i], want[i])
		}
	}
}

// withMJPEG is a phone's offer or answer with video it can send as MJPEG.
func withMJPEG(c Caps, cameraOn bool) Caps {
	c.Streams = append(c.Streams, VideoCap([]byte{CodecH264, CodecMJPEG}, VideoParams{Width: 640, Height: 480, FPS: 20, CameraOn: cameraOn}))
	return c
}

// TestBoardPeer: a Go phone and the board's C++ call manager call each
// other, agree on the code, talk and hang up; the phone's camera shows on
// the board (MJPEG, receive-only on the board).
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
	t.Run("video call to the board", func(t *testing.T) {
		b, p := startBoard(t, bin)
		c, err := p.m.Start(b.pub, withMJPEG(voice(CodecOpus), true))
		if err != nil {
			t.Fatal(err)
		}
		b.wait(t, "ringing")
		b.cmd("answer")
		ba := b.wait(t, "active")
		pa := waitState(t, p, StateActive)
		if !pa.Video || pa.VideoCodec != CodecMJPEG || ba[6] != strconv.Itoa(CodecMJPEG) {
			t.Fatalf("video: phone %v codec %d, board %s", pa.Video, pa.VideoCodec, ba[6])
		}
		sendPictures(t, c, b, 8)
		c.Hangup()
		b.wait(t, "ended")
	})
	t.Run("board calls, phone turns its camera on", func(t *testing.T) {
		b, p := startBoard(t, bin)
		b.cmd("call")
		ring := waitState(t, p, StateRinging)
		if ring.VideoCall {
			t.Fatal("the board's call shows as a video call")
		}
		c := p.m.Get(ring.ID)
		if err := c.Answer(withMJPEG(voice(CodecOpus), false)); err != nil {
			t.Fatal(err)
		}
		b.wait(t, "active")
		pa := waitState(t, p, StateActive)
		if !pa.Video || pa.VideoCodec != CodecMJPEG {
			t.Fatalf("video: %v codec %d", pa.Video, pa.VideoCodec)
		}
		sendPictures(t, c, b, 5)
		b.cmd("hangup")
		waitState(t, p, StateEnded)
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

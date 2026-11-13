package call

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"testing"
	"time"
)

func videoCaps(cameraOn bool, vcodecs ...byte) Caps {
	c := voice(CodecOpus)
	c.Streams = append(c.Streams, VideoCap(vcodecs, VideoParams{Width: 640, Height: 480, FPS: 15, CameraOn: cameraOn}))
	return c
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func pullVideo(t *testing.T, c *Call) VideoFrame {
	t.Helper()
	f, ok := c.PullVideo(5 * time.Second)
	if !ok {
		t.Fatal("no video frame")
	}
	return f
}

func TestVideoCall(t *testing.T) {
	a, b := twoPhones(t)
	ca, err := a.m.Start(b.id.Public, videoCaps(true, CodecH264, CodecVP8))
	if err != nil {
		t.Fatal(err)
	}
	ring := waitState(t, b, StateRinging)
	if !ring.VideoCall {
		t.Fatalf("not shown as a video call: %+v", ring)
	}
	cb := b.m.Get(ring.ID)
	if err := cb.Answer(videoCaps(false, CodecVP8, CodecH264)); err != nil {
		t.Fatal(err)
	}
	ia, ib := waitState(t, a, StateActive), waitState(t, b, StateActive)
	if !ia.Video || !ib.Video || ia.VideoCodec != CodecH264 || ib.VideoCodec != CodecH264 {
		t.Fatalf("video not negotiated: %+v / %+v", ia, ib)
	}
	// A keyframe of many datagrams, then small frames, both directions.
	for _, dir := range [][2]*Call{{ca, cb}, {cb, ca}} {
		from, to := dir[0], dir[1]
		frames := [][]byte{randBytes(40_000), randBytes(3000), randBytes(900), randBytes(5000)}
		for i, f := range frames {
			if err := from.SendVideo(f, i == 0); err != nil {
				t.Fatal(err)
			}
			got := pullVideo(t, to)
			if !bytes.Equal(got.Data, f) || got.Keyframe != (i == 0) {
				t.Fatalf("frame %d: %d bytes, key %v", i, len(got.Data), got.Keyframe)
			}
		}
	}
	if _, kbps := ca.VideoNeeds(); kbps < videoMinKbps {
		t.Fatal(kbps)
	}
	// Audio still flows next to video.
	if err := ca.SendAudio([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
}

// An app without video answers voice-only: the call runs as voice.
func TestVideoOfferAnsweredWithoutVideo(t *testing.T) {
	a, b := twoPhones(t)
	ca, _ := a.m.Start(b.id.Public, videoCaps(true, CodecH264))
	ring := waitState(t, b, StateRinging)
	cb := b.m.Get(ring.ID)
	if err := cb.Answer(voice(CodecOpus)); err != nil {
		t.Fatal(err)
	}
	ia, ib := waitState(t, a, StateActive), waitState(t, b, StateActive)
	if ia.Video || ib.Video {
		t.Fatalf("video on: %+v / %+v", ia, ib)
	}
	if err := ca.SendVideo([]byte{1}, true); err != errNoVideo {
		t.Fatal(err)
	}
}

// fakeCall has video state but no link (control replies are dropped).
func fakeCall() *Call {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel
	return &Call{ctx: ctx, video: newVideo()}
}

func frag(ts uint32, seq uint32, idx, n int, key bool, data []byte) Frame {
	p := binary.BigEndian.AppendUint16(nil, uint16(idx))
	p = binary.BigEndian.AppendUint16(p, uint16(n))
	f := byte(FlagFragmented)
	if key {
		f |= FlagKeyframe
	}
	return Frame{Stream: StreamVideo, Seq: seq, TS: ts, Flags: f, Payload: append(p, data...)}
}

// A lost fragment breaks the chain: frames wait for the next keyframe.
func TestVideoLossWaitsForKeyframe(t *testing.T) {
	c := fakeCall()
	now := time.Now()
	got := func() []uint32 {
		var ts []uint32
		for {
			select {
			case f := <-c.video.out:
				ts = append(ts, f.TS)
			default:
				return ts
			}
		}
	}
	// Delta frame before any keyframe: dropped.
	c.onVideo(frag(10, 1, 0, 1, false, []byte("p")), now)
	// Keyframe in 2 parts, out of order.
	c.onVideo(frag(20, 3, 1, 2, true, []byte("b")), now)
	c.onVideo(frag(20, 2, 0, 2, true, []byte("a")), now)
	// Frame 30 loses one of 2 parts; frame 40 arrives whole: chain broken.
	c.onVideo(frag(30, 4, 0, 2, false, []byte("x")), now)
	c.onVideo(frag(40, 6, 0, 1, false, []byte("y")), now)
	// Next keyframe repairs it, then deltas flow.
	c.onVideo(frag(50, 7, 0, 1, true, []byte("k")), now)
	c.onVideo(frag(60, 8, 0, 1, false, []byte("z")), now)
	// A late fragment of frame 30 is ignored.
	c.onVideo(frag(30, 5, 1, 2, false, []byte("x2")), now)
	if ts := got(); len(ts) != 3 || ts[0] != 20 || ts[1] != 50 || ts[2] != 60 {
		t.Fatalf("delivered %v, want [20 50 60]", ts)
	}
	// A frame stuck past frameGiveUp is dropped and needs a keyframe.
	c.onVideo(frag(70, 9, 0, 2, false, []byte("s")), now)
	c.onVideo(frag(80, 11, 0, 1, false, []byte("t")), now.Add(frameGiveUp+time.Millisecond))
	if ts := got(); len(ts) != 0 {
		t.Fatalf("delivered %v after a lost frame", ts)
	}
}

func TestVideoControl(t *testing.T) {
	c := fakeCall()
	ctl := func(kind byte, body ...byte) {
		c.onControl(Frame{Stream: streamControl, Payload: append([]byte{kind, StreamVideo}, body...)})
	}
	ctl(ctlKeyframe)
	if k, _ := c.VideoNeeds(); !k {
		t.Fatal("keyframe request lost")
	}
	_, k0 := c.VideoNeeds()
	ctl(ctlReport, 0, 200) // 20 % loss
	if _, k := c.VideoNeeds(); k >= k0 {
		t.Fatalf("no slowdown on loss: %d → %d", k0, k)
	}
	for i := 0; i < 100; i++ {
		ctl(ctlReport, 1, 44) // 30 %
	}
	if _, k := c.VideoNeeds(); k != videoMinKbps {
		t.Fatal(k)
	}
	for i := 0; i < 200; i++ {
		ctl(ctlReport, 0, 0)
	}
	if _, k := c.VideoNeeds(); k != videoMaxKbps {
		t.Fatal(k)
	}
	if p := ParseVideoParams(VideoParams{Width: 1280, Height: 720, FPS: 24, CameraOn: true}.encode()); p.Width != 1280 || !p.CameraOn || p.FPS != 24 {
		t.Fatalf("%+v", p)
	}
}

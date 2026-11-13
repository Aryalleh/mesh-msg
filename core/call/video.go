package call

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
)

// Video (calls.md §7a). Stream 1 carries video frames; the app encodes and
// decodes (H.264 from the phone's hardware codec), the core only moves
// frames:
//
//   - A frame is split into datagrams on stream 1, each with its own seq and
//     the frame's timestamp; the payload starts with frag_index(2) |
//     frag_count(2) (inside the sealed part). Flags: FlagFragmented always,
//     FlagKeyframe on every fragment of a keyframe, FlagEnd on the last.
//   - The receiver reassembles by timestamp. A frame that never completes
//     breaks the decoder's chain: frames are dropped until the next keyframe
//     and a keyframe is asked for.
//   - In-band control on stream 255 (old voice-only apps ignore streams they
//     don't know): kind(1) | stream(1) | body. Kind 0 keyframe request,
//     kind 1 receiver report: loss permille(2) over the last second.
//   - The sender keeps a target bitrate from the reports (down on loss, up
//     slowly when clean) that the app reads with VideoNeeds and gives to its
//     encoder.
//
// What a frame contains is up to the app; Android sends orientation(1) |
// H.264 Annex-B access unit (SPS/PPS before each keyframe).

const (
	StreamVideo   = 1
	streamControl = 255

	ctlKeyframe = 0
	ctlReport   = 1

	fragHead = 4
	// MaxVideoFrame is the largest encoded frame SendVideo takes.
	MaxVideoFrame = 512 << 10

	videoStartKbps = 500
	videoMinKbps   = 120
	videoMaxKbps   = 2000
	keyReqEvery    = 400 * time.Millisecond
	reportEvery    = time.Second
	frameGiveUp    = 800 * time.Millisecond
	videoQueue     = 6
)

// VideoParams are a video stream's limits (StreamCap.Params).
type VideoParams struct {
	Width, Height uint16
	FPS           byte
	CameraOn      bool // the caller starts with the camera on: a "video call"
}

func (p VideoParams) encode() []byte {
	b := binary.BigEndian.AppendUint16(nil, p.Width)
	b = binary.BigEndian.AppendUint16(b, p.Height)
	b = append(b, p.FPS, 0)
	if p.CameraOn {
		b[5] = 1
	}
	return b
}

// ParseVideoParams reads a video stream's params (missing fields stay 0).
func ParseVideoParams(b []byte) VideoParams {
	var p VideoParams
	if len(b) >= 4 {
		p.Width, p.Height = binary.BigEndian.Uint16(b), binary.BigEndian.Uint16(b[2:])
	}
	if len(b) >= 5 {
		p.FPS = b[4]
	}
	if len(b) >= 6 {
		p.CameraOn = b[5]&1 != 0
	}
	return p
}

// VideoCap is the video stream offer for these codecs (best first).
func VideoCap(codecs []byte, p VideoParams) StreamCap {
	return StreamCap{ID: StreamVideo, Media: MediaVideo, Dir: DirBoth, Codecs: codecs, MaxKbps: videoMaxKbps, Params: p.encode()}
}

// Video returns the video stream (id 1), if present.
func (c Caps) Video() (StreamCap, bool) {
	for _, s := range c.Streams {
		if s.ID == StreamVideo && s.Media == MediaVideo {
			return s, true
		}
	}
	return StreamCap{}, false
}

// VideoFrame is one reassembled frame.
type VideoFrame struct {
	TS       uint32
	Keyframe bool
	Data     []byte
}

type partial struct {
	key   bool
	count int
	got   int
	parts [][]byte
	first time.Time
}

// video is one call's video state, both directions.
type video struct {
	mu sync.Mutex

	// sending
	seq     uint32
	ctlSeq  uint32
	kbps    int
	wantKey bool

	// receiving
	frames   map[uint32]*partial
	lastDone uint32 // timestamp of the newest delivered frame
	haveDone bool
	needKey  bool
	lastReq  time.Time
	out      chan VideoFrame
	// loss counting for reports
	winFirst, winMax uint32
	winGot           int
	winStarted       bool
	lastReport       time.Time
}

func newVideo() *video {
	return &video{kbps: videoStartKbps, frames: map[uint32]*partial{}, out: make(chan VideoFrame, videoQueue), needKey: true}
}

var (
	errNoVideo       = errors.New("call: no video in this call")
	errVideoTooLarge = errors.New("call: video frame too large")
)

// maxFrag is the payload of one video datagram.
const maxFrag = direct.MaxPayload - 1 - frameHead - 16 - fragHead

// SendVideo sends one encoded video frame.
func (c *Call) SendVideo(data []byte, keyframe bool) error {
	c.mu.Lock()
	l, cr, st, v := c.link, c.cr, c.state, c.video
	c.mu.Unlock()
	if st != StateActive || l == nil {
		return ErrNotActive
	}
	if v == nil {
		return errNoVideo
	}
	if len(data) == 0 || len(data) > MaxVideoFrame {
		return errVideoTooLarge
	}
	n := (len(data) + maxFrag - 1) / maxFrag
	ts := c.ts()
	flags := byte(FlagFragmented)
	if keyframe {
		flags |= FlagKeyframe
		v.mu.Lock()
		v.wantKey = false
		v.mu.Unlock()
	}
	for i := 0; i < n; i++ {
		chunk := data[i*maxFrag : min(len(data), (i+1)*maxFrag)]
		p := make([]byte, fragHead, fragHead+len(chunk))
		binary.BigEndian.PutUint16(p, uint16(i))
		binary.BigEndian.PutUint16(p[2:], uint16(n))
		p = append(p, chunk...)
		f := flags
		if i == n-1 {
			f |= FlagEnd
		}
		v.mu.Lock()
		v.seq++
		seq := v.seq
		v.mu.Unlock()
		if err := l.Send(cr.seal(Frame{Stream: StreamVideo, Seq: seq, TS: ts, Flags: f, Payload: p})); err != nil {
			return err
		}
	}
	return nil
}

// VideoNeeds tells the sending app whether the other side needs a keyframe
// now and the bitrate to encode at (kbps).
func (c *Call) VideoNeeds() (keyframe bool, kbps int) {
	c.mu.Lock()
	v := c.video
	c.mu.Unlock()
	if v == nil {
		return false, 0
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.wantKey, v.kbps
}

// PullVideo waits up to timeout for the next frame to decode.
func (c *Call) PullVideo(timeout time.Duration) (VideoFrame, bool) {
	c.mu.Lock()
	v := c.video
	c.mu.Unlock()
	if v == nil {
		return VideoFrame{}, false
	}
	select {
	case f := <-v.out:
		return f, true
	case <-time.After(timeout):
		return VideoFrame{}, false
	case <-c.ctx.Done():
		return VideoFrame{}, false
	}
}

// sendControl sends an in-band control datagram.
func (c *Call) sendControl(kind, stream byte, body []byte) {
	c.mu.Lock()
	l, cr, v := c.link, c.cr, c.video
	c.mu.Unlock()
	if l == nil || cr == nil || v == nil {
		return
	}
	v.mu.Lock()
	v.ctlSeq++
	seq := v.ctlSeq
	v.mu.Unlock()
	l.Send(cr.seal(Frame{Stream: streamControl, Seq: seq, TS: c.ts(), Payload: append([]byte{kind, stream}, body...)}))
}

// onControl handles an in-band control datagram from the other side.
func (c *Call) onControl(f Frame) {
	c.mu.Lock()
	v := c.video
	c.mu.Unlock()
	if v == nil || len(f.Payload) < 2 || f.Payload[1] != StreamVideo {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch f.Payload[0] {
	case ctlKeyframe:
		v.wantKey = true
	case ctlReport:
		if len(f.Payload) < 4 {
			return
		}
		loss := int(binary.BigEndian.Uint16(f.Payload[2:]))
		switch {
		case loss > 100:
			v.kbps = v.kbps * 7 / 10
		case loss > 30:
			v.kbps = v.kbps * 9 / 10
		case loss < 20:
			v.kbps = v.kbps * 108 / 100
		}
		v.kbps = min(max(v.kbps, videoMinKbps), videoMaxKbps)
	}
}

// onVideo handles one video datagram; it may complete a frame.
func (c *Call) onVideo(f Frame, now time.Time) {
	c.mu.Lock()
	v := c.video
	c.mu.Unlock()
	if v == nil || f.Flags&FlagFragmented == 0 || len(f.Payload) < fragHead {
		return
	}
	idx := int(binary.BigEndian.Uint16(f.Payload))
	n := int(binary.BigEndian.Uint16(f.Payload[2:]))
	if n == 0 || idx >= n || n > MaxVideoFrame/maxFrag+1 {
		return
	}
	askKey := false
	var report []byte

	v.mu.Lock()
	// loss window
	if !v.winStarted {
		v.winStarted, v.winFirst, v.winMax, v.lastReport = true, f.Seq, f.Seq, now
	}
	if f.Seq > v.winMax {
		v.winMax = f.Seq
	}
	v.winGot++
	if now.Sub(v.lastReport) >= reportEvery {
		exp := int(v.winMax-v.winFirst) + 1
		lossPm := 0
		if exp > v.winGot {
			lossPm = (exp - v.winGot) * 1000 / exp
		}
		report = binary.BigEndian.AppendUint16(nil, uint16(lossPm))
		v.winStarted, v.winGot = false, 0
	}

	if v.haveDone && int32(f.TS-v.lastDone) <= 0 {
		v.mu.Unlock() // a late fragment of a frame already given up or shown
		if report != nil {
			c.sendControl(ctlReport, StreamVideo, report)
		}
		return
	}
	p := v.frames[f.TS]
	if p == nil {
		p = &partial{count: n, parts: make([][]byte, n), first: now}
		v.frames[f.TS] = p
	}
	if f.Flags&FlagKeyframe != 0 {
		p.key = true
	}
	if p.count == n && p.parts[idx] == nil {
		p.parts[idx] = append([]byte(nil), f.Payload[fragHead:]...)
		p.got++
	}
	if p.got == p.count {
		delete(v.frames, f.TS)
		// Older unfinished frames are lost now: the chain is broken.
		for ts := range v.frames {
			if int32(ts-f.TS) < 0 {
				delete(v.frames, ts)
				v.needKey = true
			}
		}
		if p.key {
			v.needKey = false
		}
		if !v.needKey {
			size := 0
			for _, b := range p.parts {
				size += len(b)
			}
			data := make([]byte, 0, size)
			for _, b := range p.parts {
				data = append(data, b...)
			}
			select {
			case v.out <- VideoFrame{TS: f.TS, Keyframe: p.key, Data: data}:
			default:
				// The app is behind: dropping breaks the chain too.
				v.needKey = true
			}
		}
		v.lastDone, v.haveDone = f.TS, true
	}
	// Frames stuck for too long are lost.
	for ts, o := range v.frames {
		if now.Sub(o.first) > frameGiveUp {
			delete(v.frames, ts)
			v.needKey = true
		}
	}
	if v.needKey && now.Sub(v.lastReq) >= keyReqEvery {
		v.lastReq = now
		askKey = true
	}
	v.mu.Unlock()

	if askKey {
		c.sendControl(ctlKeyframe, StreamVideo, nil)
	}
	if report != nil {
		c.sendControl(ctlReport, StreamVideo, report)
	}
}

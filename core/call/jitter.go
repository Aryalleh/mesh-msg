package call

import (
	"sync"
	"time"
)

// Pull results.
const (
	PullFrame = 0 // a frame to decode
	PullLost  = 1 // a frame is missing: conceal (decoder PLC)
	PullEmpty = 2 // nothing yet (start, or the sender is silent): play silence
)

// Jitter is an adaptive jitter buffer for one stream (calls.md §3): the
// player pulls one frame per frame period; the delay target follows the
// measured inter-arrival jitter between 40 and 200 ms.
type Jitter struct {
	mu      sync.Mutex
	period  time.Duration
	frames  map[uint32]Frame
	next    uint32
	started bool
	playing bool
	target  int // frames

	// RFC 3550 inter-arrival jitter, in ms.
	jitter   float64
	lastTS   uint32
	lastAt   time.Time
	haveLast bool

	Stats JitterStats
}

// JitterStats counts what happened, for the call screen and bitrate control.
type JitterStats struct {
	Received, Late, Lost, Underruns, Dropped int
}

// NewJitter makes a buffer for frames of the given period (20 ms for audio).
func NewJitter(period time.Duration) *Jitter {
	return &Jitter{period: period, frames: map[uint32]Frame{}, target: framesFor(60*time.Millisecond, period)}
}

func framesFor(d, period time.Duration) int { return max(1, int((d+period-1)/period)) }

// Push adds a received frame.
func (j *Jitter) Push(f Frame, at time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.started {
		j.started, j.next = true, f.Seq
	}
	if int32(f.Seq-j.next) < 0 {
		j.Stats.Late++
		return
	}
	if _, dup := j.frames[f.Seq]; dup {
		return
	}
	if len(j.frames) > 200 { // far behind: start over from this frame
		j.frames = map[uint32]Frame{}
		j.next, j.playing = f.Seq, false
	}
	j.frames[f.Seq] = f
	j.Stats.Received++
	if j.haveLast {
		d := float64(at.Sub(j.lastAt).Milliseconds()) - float64(int32(f.TS-j.lastTS))
		if d < 0 {
			d = -d
		}
		j.jitter += (d - j.jitter) / 16
	}
	j.lastTS, j.lastAt, j.haveLast = f.TS, at, true
	want := time.Duration(2*j.jitter)*time.Millisecond + j.period
	want = min(max(want, 40*time.Millisecond), 200*time.Millisecond)
	j.target = framesFor(want, j.period)
}

// Pull returns the next frame to play, or why there is none.
func (j *Jitter) Pull() (Frame, int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.playing {
		if len(j.frames) < j.target {
			return Frame{}, PullEmpty
		}
		j.playing = true
	}
	// Far more queued than the target (a burst after a stall): skip one
	// frame per pull to bring the delay back down.
	if len(j.frames) > j.target+10 {
		if _, ok := j.frames[j.next]; ok {
			delete(j.frames, j.next)
			j.Stats.Dropped++
		}
		j.next++
	}
	if f, ok := j.frames[j.next]; ok {
		delete(j.frames, j.next)
		j.next++
		return f, PullFrame
	}
	if len(j.frames) == 0 {
		// Underrun: rebuffer up to the target before playing again.
		j.playing = false
		j.Stats.Underruns++
		return Frame{}, PullEmpty
	}
	j.next++
	j.Stats.Lost++
	return Frame{}, PullLost
}

// Delay is the current target delay.
func (j *Jitter) Delay() time.Duration {
	j.mu.Lock()
	defer j.mu.Unlock()
	return time.Duration(j.target) * j.period
}

// Package sim is a discrete-event simulator that runs the real core/router
// and server/relay code in virtual time: phones moving between places
// (Bluetooth), LoRa radios with airtime and duty-cycle limits, relays,
// gateways and intermittent bridges.
package sim

import (
	"container/heap"
	"math/rand"
	"time"
)

// Engine is a deterministic event loop over virtual time.
type Engine struct {
	now   time.Time
	q     eventQueue
	seq   int64
	Rand  *rand.Rand
	steps int
}

type event struct {
	at  time.Time
	seq int64
	fn  func()
}

type eventQueue []*event

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	if q[i].at.Equal(q[j].at) {
		return q[i].seq < q[j].seq
	}
	return q[i].at.Before(q[j].at)
}
func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(x any)   { *q = append(*q, x.(*event)) }
func (q *eventQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	*q = old[:len(old)-1]
	return e
}

// NewEngine starts the clock at a fixed date.
func NewEngine(seed int64) *Engine {
	return &Engine{now: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), Rand: rand.New(rand.NewSource(seed))}
}

// Now is the virtual time.
func (e *Engine) Now() time.Time { return e.now }

// After schedules fn after d.
func (e *Engine) After(d time.Duration, fn func()) {
	e.seq++
	heap.Push(&e.q, &event{at: e.now.Add(d), seq: e.seq, fn: fn})
}

// Every schedules fn every d (first run after an offset).
func (e *Engine) Every(offset, d time.Duration, fn func()) {
	var tick func()
	tick = func() { fn(); e.After(d, tick) }
	e.After(offset, tick)
}

// RunFor processes events for d of virtual time.
func (e *Engine) RunFor(d time.Duration) {
	end := e.now.Add(d)
	for e.q.Len() > 0 {
		next := e.q[0]
		if next.at.After(end) {
			break
		}
		heap.Pop(&e.q)
		e.now = next.at
		e.steps++
		next.fn()
	}
	e.now = end
}

// Exp returns an exponentially distributed duration with the given mean.
func (e *Engine) Exp(mean time.Duration) time.Duration {
	return time.Duration(e.Rand.ExpFloat64() * float64(mean))
}

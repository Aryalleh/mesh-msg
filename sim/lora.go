package sim

import (
	"math"
	"sort"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// LoRaParams describe a radio preset.
type LoRaParams struct {
	SF        int
	BWHz      float64
	CR        int     // 1..4 → 4/5..4/8
	DutyCycle float64 // e.g. 0.01
	RangeM    float64
}

// Balanced is the default backbone preset (docs/firmware.md §4).
var Balanced = LoRaParams{SF: 9, BWHz: 125e3, CR: 1, DutyCycle: 0.01, RangeM: 12000}

// Airtime is the Semtech LoRa time-on-air formula (explicit header, CRC on).
func (p LoRaParams) Airtime(payload int) time.Duration {
	tsym := math.Pow(2, float64(p.SF)) / p.BWHz
	de := 0.0
	if tsym > 0.016 {
		de = 1 // low data rate optimisation
	}
	preamble := (8 + 4.25) * tsym
	n := math.Ceil((8*float64(payload)-4*float64(p.SF)+28+16)/(4*(float64(p.SF)-2*de))) * float64(p.CR+4)
	symbols := 8 + math.Max(n, 0)
	return time.Duration((preamble + symbols*tsym) * float64(time.Second))
}

// Radio is a LoRa node's broadcast link with a priority queue, half-duplex
// transmit, a sliding one-hour duty-cycle budget, a cap on announce airtime,
// and implicit-ACK retransmission (docs/firmware.md §2a, §5).
type Radio struct {
	w         *World
	node      *Node
	X, Y      float64
	P         LoRaParams
	queue     []queued
	busy      time.Time
	tx        []txRecord
	sched     bool
	Stats     RadioStats
	maxQue    int
	awaiting  map[packet.Key]int // frames waiting for a neighbour's rebroadcast → tries
	heardFrom map[packet.Key]map[*Radio]bool
	neighbors map[*Radio]time.Time // learned from the link-header sender ID
}

const (
	maxRetries       = 2
	announceAirShare = 0.10 // announces may use at most 10% of the duty budget
)

type queued struct {
	raw  []byte
	prio int
	at   time.Time
}

type txRecord struct {
	at       time.Time
	dur      time.Duration
	announce bool
}

// RadioStats are per-radio counters.
type RadioStats struct {
	Frames      int
	Airtime     time.Duration
	QueueDrops  int
	BudgetWaits int
	Received    int
	MissedBusy  int
	Retries     int
	AnnDropped  int
}

func (r *Radio) Name() string          { return "lora" }
func (r *Radio) Kind() router.LinkKind { return router.Broadcast }
func (r *Radio) MTU() int              { return packet.LoRaMTU }

func priorityOf(raw []byte) int {
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
		return 3
	}
	return 9
}

// Send queues a frame for transmission.
func (r *Radio) Send(_ router.PeerID, raw []byte) { r.enqueue(raw) }

func (r *Radio) enqueue(raw []byte) {
	r.init()
	if len(r.queue) >= r.maxQue {
		r.Stats.QueueDrops++
		return
	}
	r.queue = append(r.queue, queued{raw: raw, prio: priorityOf(raw), at: r.w.E.Now()})
	sort.SliceStable(r.queue, func(i, j int) bool { return r.queue[i].prio < r.queue[j].prio })
	r.schedule(0)
}

func (r *Radio) schedule(d time.Duration) {
	if r.sched {
		return
	}
	r.sched = true
	r.w.E.After(d, func() { r.sched = false; r.tryTx() })
}

func (r *Radio) usedLastHour(now time.Time) (all, announces time.Duration) {
	keep := r.tx[:0]
	for _, t := range r.tx {
		if now.Sub(t.at) < time.Hour {
			all += t.dur
			if t.announce {
				announces += t.dur
			}
			keep = append(keep, t)
		}
	}
	r.tx = keep
	return all, announces
}

func (r *Radio) init() {
	if r.awaiting == nil {
		r.awaiting = map[packet.Key]int{}
		r.heardFrom = map[packet.Key]map[*Radio]bool{}
		r.neighbors = map[*Radio]time.Time{}
	}
}

// heard is called for every frame received from sender (whose short ID is in
// the link header). Any neighbour transmitting a frame we sent is its
// implicit ACK.
func (r *Radio) heard(raw []byte, sender *Radio) {
	r.init()
	r.neighbors[sender] = r.w.E.Now()
	if p, err := packet.Decode(raw); err == nil {
		k := p.Key()
		delete(r.awaiting, k)
		if r.heardFrom[k] == nil {
			r.heardFrom[k] = map[*Radio]bool{}
		}
		r.heardFrom[k][sender] = true
	}
}

// expectAck arms a retransmission if some recently heard neighbour that has
// not transmitted this frame itself should rebroadcast it. A leaf node
// re-broadcasting what its only neighbour sent waits for nobody.
func (r *Radio) expectAck(raw []byte, air time.Duration) {
	p, err := packet.Decode(raw)
	if err != nil || p.Type == packet.TypeAnnounce || p.HopCount >= p.MaxHops {
		return // announces are best-effort; at the hop limit nobody rebroadcasts
	}
	k := p.Key()
	waitingFor := false
	for nb, seen := range r.neighbors {
		if r.w.E.Now().Sub(seen) < time.Hour && !r.heardFrom[k][nb] {
			waitingFor = true
			break
		}
	}
	if !waitingFor {
		return
	}
	tries := r.awaiting[k]
	if tries >= maxRetries {
		delete(r.awaiting, k)
		return
	}
	r.awaiting[k] = tries + 1
	wait := 3*air + 20*time.Second + time.Duration(r.w.E.Rand.Intn(10000))*time.Millisecond
	r.w.E.After(wait, func() {
		if t, ok := r.awaiting[k]; ok && t == tries+1 {
			r.Stats.Retries++
			r.enqueue(raw)
		}
	})
}

func (r *Radio) tryTx() {
	now := r.w.E.Now()
	if len(r.queue) == 0 {
		return
	}
	if now.Before(r.busy) {
		r.schedule(r.busy.Sub(now))
		return
	}
	q := r.queue[0]
	air := r.P.Airtime(len(q.raw))
	budget := time.Duration(r.P.DutyCycle * float64(time.Hour))
	used, annUsed := r.usedLastHour(now)
	isAnn := q.prio == 3
	if isAnn && annUsed+air > time.Duration(announceAirShare*float64(budget)) {
		r.queue = r.queue[1:] // announces are refreshed later anyway
		r.Stats.AnnDropped++
		r.schedule(0)
		return
	}
	if used+air > budget {
		r.Stats.BudgetWaits++
		r.schedule(time.Minute)
		return
	}
	r.queue = r.queue[1:]
	// Random backoff stands in for listen-before-talk.
	jitter := time.Duration(r.w.E.Rand.Intn(500)) * time.Millisecond
	r.busy = now.Add(air + jitter)
	r.tx = append(r.tx, txRecord{now, air, isAnn})
	r.expectAck(q.raw, air)
	r.Stats.Frames++
	r.Stats.Airtime += air
	r.w.Stats.LoRaFrames++
	r.w.Stats.LoRaAirtime += air
	for _, other := range r.w.Radios {
		if other == r || math.Hypot(other.X-r.X, other.Y-r.Y) > math.Min(r.P.RangeM, other.P.RangeM) {
			continue
		}
		o, raw := other, q.raw
		r.w.E.After(air, func() {
			// Half-duplex: a radio transmitting during reception misses it.
			if o.lastTxOverlaps(r.w.E.Now(), air) {
				o.Stats.MissedBusy++
				return
			}
			o.Stats.Received++
			o.heard(raw, r)
			o.node.R.Inbound("lora", "", raw)
		})
	}
	if len(r.queue) > 0 {
		r.schedule(air + jitter)
	}
}

func (r *Radio) lastTxOverlaps(end time.Time, air time.Duration) bool {
	start := end.Add(-air)
	for _, t := range r.tx {
		if t.at.Before(end) && t.at.Add(t.dur).After(start) {
			return true
		}
	}
	return false
}

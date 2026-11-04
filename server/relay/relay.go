// Package relay is the transport-independent logic of a relay server: an
// untrusted store-and-forward mailbox that federates with other relays and
// serves gateways (docs/server.md). The HTTP layer lives in server/api; the
// simulator drives this package directly.
package relay

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/store"
)

// Config configures a relay.
type Config struct {
	Name   string // stable relay name; used by peers as the origin label
	Region string // e.g. "ir", "global"
	Store  store.Store
	Now    func() time.Time
	// DataTTL is how long undelivered packets are kept (default 7 days).
	DataTTL time.Duration
	// CarrierMinAge: packets whose destination has no known home region are
	// handed to carrier gateways once they are this old (default 10 min).
	CarrierMinAge time.Duration
	Logf          func(format string, args ...any)
}

// Relay is safe for concurrent use.
type Relay struct {
	cfg        Config
	mu         sync.Mutex
	changed    chan struct{}
	challenges map[string]time.Time
	stats      Stats
}

// Stats are cumulative counters.
type Stats struct {
	Accepted, Duplicate, Rejected, Purged int
}

// New creates a relay.
func New(cfg Config) *Relay {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DataTTL == 0 {
		cfg.DataTTL = 7 * 24 * time.Hour
	}
	if cfg.CarrierMinAge == 0 {
		cfg.CarrierMinAge = 10 * time.Minute
	}
	if cfg.Store == nil {
		cfg.Store = store.NewMemory()
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Relay{cfg: cfg, changed: make(chan struct{}), challenges: map[string]time.Time{}}
}

func (r *Relay) Name() string   { return r.cfg.Name }
func (r *Relay) Region() string { return r.cfg.Region }

// Changed returns a channel closed on the next accepted packet (long-poll).
func (r *Relay) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

func (r *Relay) notify() {
	r.mu.Lock()
	close(r.changed)
	r.changed = make(chan struct{})
	r.mu.Unlock()
}

// Ingest accepts raw packets. origin labels where they came from (a peer
// relay or bridge name, or "client:<addr>"); region is the home region to
// record for announces arriving this way ("" = unknown).
func (r *Relay) Ingest(raws [][]byte, origin, region string) (accepted int) {
	now := r.cfg.Now()
	for _, raw := range raws {
		switch r.ingestOne(raw, origin, region, now) {
		case resAccepted:
			accepted++
		case resDuplicate:
			r.mu.Lock()
			r.stats.Duplicate++
			r.mu.Unlock()
		case resRejected:
			r.mu.Lock()
			r.stats.Rejected++
			r.mu.Unlock()
		}
	}
	if accepted > 0 {
		r.mu.Lock()
		r.stats.Accepted += accepted
		r.mu.Unlock()
		r.notify()
	}
	return accepted
}

type result int

const (
	resAccepted result = iota
	resDuplicate
	resRejected
)

func (r *Relay) ingestOne(raw []byte, origin, region string, now time.Time) result {
	p, err := packet.Decode(raw)
	if err != nil {
		return resRejected
	}
	k := p.Key()
	if ok, _ := r.cfg.Store.Has(k); ok {
		return resDuplicate
	}
	it := store.Item{
		Key: k, Type: p.Type, Dest: p.Dest, Raw: append([]byte(nil), raw...),
		Origin: origin, Received: now, Expires: now.Add(r.cfg.DataTTL),
	}
	switch p.Type {
	case packet.TypeAnnounce:
		a, err := announce.Parse(p)
		if err != nil {
			return resRejected
		}
		// Keep only announces newer than what we have.
		if old, ok, _ := r.cfg.Store.LatestAnnounce(p.Dest); ok {
			if op, err := packet.Decode(old.Raw); err == nil {
				if oa, err := announce.Parse(op); err == nil && !a.Time.After(oa.Time) {
					return resDuplicate
				}
			}
			if region == "" {
				region = old.Region
			}
		}
		it.Region = region
	case packet.TypeReceipt:
		acked, err := announce.ReceiptAcked(p)
		if err != nil {
			return resRejected
		}
		if pub, ok := r.publicFor(p.Dest); ok {
			if _, err := announce.VerifyReceipt(p, pub); err != nil {
				return resRejected
			}
			n, _ := r.cfg.Store.DeleteData(acked)
			r.mu.Lock()
			r.stats.Purged += n
			r.mu.Unlock()
		}
	case packet.TypeData:
		if ok, _ := r.cfg.Store.Has(packet.Key{ID: announce.ReceiptID(p.ID)}); ok {
			return resDuplicate // already delivered
		}
	default:
		return resRejected // hop-local types never reach relays
	}
	if _, added, err := r.cfg.Store.Put(it); err != nil || !added {
		if err != nil {
			r.cfg.Logf("relay: store: %v", err)
		}
		return resDuplicate
	}
	return resAccepted
}

// announceFor returns the latest verified announce for dest.
func (r *Relay) announceFor(dest packet.Hash) (announce.Announce, store.Item, bool) {
	it, ok, _ := r.cfg.Store.LatestAnnounce(dest)
	if !ok {
		return announce.Announce{}, it, false
	}
	p, err := packet.Decode(it.Raw)
	if err != nil {
		return announce.Announce{}, it, false
	}
	a, err := announce.Parse(p)
	return a, it, err == nil
}

func (r *Relay) publicFor(dest packet.Hash) (pub identity.Public, ok bool) {
	a, _, ok := r.announceFor(dest)
	return a.Public, ok
}

// Delegated reports whether dest allows any gateway to fetch its packets.
func (r *Relay) Delegated(dest packet.Hash) bool {
	a, _, ok := r.announceFor(dest)
	return ok && a.App.Flags&announce.FlagDelegate != 0
}

// NewChallenge returns a single-use nonce for signed mailbox fetches.
func (r *Relay) NewChallenge() string {
	var b [16]byte
	rand.Read(b[:])
	n := hex.EncodeToString(b[:])
	now := r.cfg.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, t := range r.challenges {
		if now.Sub(t) > 5*time.Minute {
			delete(r.challenges, k)
		}
	}
	r.challenges[n] = now
	return n
}

// AuthorizeFetch checks access to dest's mailbox: either dest delegated
// fetching to gateways, or sig signs a fresh challenge with dest's key.
func (r *Relay) AuthorizeFetch(dest packet.Hash, nonce string, sig []byte) bool {
	if r.Delegated(dest) {
		return true
	}
	r.mu.Lock()
	t, ok := r.challenges[nonce]
	if ok {
		delete(r.challenges, nonce)
	}
	r.mu.Unlock()
	if !ok || r.cfg.Now().Sub(t) > 5*time.Minute {
		return false
	}
	a, _, ok := r.announceFor(dest)
	if !ok {
		return false
	}
	return announce.VerifyFetch(a.Public, []byte(nonce), sig)
}

// Mailbox returns DATA for dest after cursor, plus the new cursor.
func (r *Relay) Mailbox(dest packet.Hash, after int64, limit int) ([][]byte, int64) {
	items, err := r.cfg.Store.ByDest(dest, after, limit)
	if err != nil {
		r.cfg.Logf("relay: mailbox: %v", err)
	}
	return raws(items, after)
}

// Pull returns everything after cursor not originating from excludeOrigin
// (federation / bridge sync), the new cursor and whether more is pending.
func (r *Relay) Pull(after int64, limit int, excludeOrigin string) ([][]byte, int64, bool) {
	if limit <= 0 {
		limit = 500
	}
	// Read the high-water mark *before* querying: anything inserted after
	// this point has a larger seq and will be picked up next time.
	maxBefore, _ := r.cfg.Store.MaxSeq()
	items, err := r.cfg.Store.Since(after, limit+1, store.Filter{ExcludeOrigin: excludeOrigin})
	if err != nil {
		r.cfg.Logf("relay: pull: %v", err)
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	out, cur := raws(items, after)
	if !more && maxBefore > cur {
		// Skip past excluded items too, so the cursor doesn't stall.
		cur = maxBefore
	}
	return out, cur, more
}

func raws(items []store.Item, after int64) ([][]byte, int64) {
	out := make([][]byte, len(items))
	cur := after
	for i, it := range items {
		out[i] = it.Raw
		cur = it.Seq
	}
	return out, cur
}

// GatewayRequest describes what a gateway wants (docs/server.md §4).
type GatewayRequest struct {
	Origin     string        // gateway label (its uploads are not echoed back)
	Serve      []packet.Hash // destinations in the gateway's local mesh
	Carrier    bool          // also accept undelivered traffic for this region
	QuotaBytes int           // carrier budget for this call
	After      int64
	Limit      int
}

// Gateway returns packets for a gateway: receipts, DATA for served
// destinations that delegated fetching, and (carrier mode) undelivered DATA
// whose home region is this relay's region.
func (r *Relay) Gateway(req GatewayRequest) ([][]byte, int64) {
	if req.Limit <= 0 {
		req.Limit = 500
	}
	serve := map[packet.Hash]bool{}
	for _, h := range req.Serve {
		if r.Delegated(h) {
			serve[h] = true
		}
	}
	items, err := r.cfg.Store.Since(req.After, 2000, store.Filter{
		ExcludeOrigin: req.Origin, Types: []packet.Type{packet.TypeData, packet.TypeReceipt},
	})
	if err != nil {
		r.cfg.Logf("relay: gateway: %v", err)
	}
	now := r.cfg.Now()
	var out [][]byte
	cur := req.After
	quota := req.QuotaBytes
	for _, it := range items {
		if len(out) >= req.Limit {
			break
		}
		include := false
		switch {
		case it.Type == packet.TypeReceipt:
			include = true
		case serve[it.Dest]:
			include = true
		case req.Carrier && quota >= len(it.Raw):
			_, ann, ok := r.announceFor(it.Dest)
			home := ""
			if ok {
				home = ann.Region
			}
			if home == r.cfg.Region || (home == "" && now.Sub(it.Received) >= r.cfg.CarrierMinAge) {
				include = true
				quota -= len(it.Raw)
			}
		}
		if include {
			out = append(out, it.Raw)
		}
		cur = it.Seq
	}
	return out, cur
}

// Expire drops expired packets.
func (r *Relay) Expire() {
	if _, err := r.cfg.Store.Expire(r.cfg.Now()); err != nil {
		r.cfg.Logf("relay: expire: %v", err)
	}
}

// Stats returns counters.
func (r *Relay) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

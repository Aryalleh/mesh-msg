package relayclient

import (
	"context"
	"sort"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Side is one end of a sync: a remote relay (via Client) or the local relay
// inside a relay server (federation).
type Side interface {
	Name() string
	// Pull returns items after cursor that did not come from the other side.
	Pull(ctx context.Context, after int64, limit int) ([]wire.SyncItem, int64, bool, error)
	Push(ctx context.Context, items []wire.SyncItem) error
}

// RemoteSide adapts a Client. The client's Origin must be the syncer's label
// so the relay excludes what the syncer pushed.
type RemoteSide struct {
	C     *Client
	Label string
}

func (r RemoteSide) Name() string { return r.Label }
func (r RemoteSide) Pull(ctx context.Context, after int64, limit int) ([]wire.SyncItem, int64, bool, error) {
	return r.C.SyncPull(ctx, after, limit, 0)
}
func (r RemoteSide) Push(ctx context.Context, items []wire.SyncItem) error {
	_, err := r.C.SyncPush(ctx, items)
	return err
}

// Cursors is the persisted sync position on both sides.
type Cursors struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// Budget limits bytes moved per sync direction (scarce satellite / white-SIM
// data). Nil means unlimited.
type Budget struct {
	Remaining int64
}

// priority orders items for a constrained link: receipts free everybody's
// storage, announces make routing and auth possible, then data.
func priority(it wire.SyncItem) int {
	p, err := packet.Decode(it.Raw)
	if err != nil {
		return 9
	}
	switch p.Type {
	case packet.TypeReceipt:
		return 0
	case packet.TypeAnnounce:
		return 1
	case packet.TypeData:
		if p.Priority() {
			return 2
		}
		if p.NoLoRa() { // media last
			return 4
		}
		return 3
	}
	return 9
}

// SyncOnce moves everything pending from a to b and from b to a. It returns
// the number of items moved in each direction.
func SyncOnce(ctx context.Context, a, b Side, cur *Cursors, budget *Budget) (ab, ba int, err error) {
	ab, err = syncDir(ctx, a, b, &cur.A, budget)
	if err != nil {
		return
	}
	ba, err = syncDir(ctx, b, a, &cur.B, budget)
	return
}

func syncDir(ctx context.Context, from, to Side, cursor *int64, budget *Budget) (int, error) {
	moved := 0
	for {
		items, next, more, err := from.Pull(ctx, *cursor, 500)
		if err != nil {
			return moved, err
		}
		dropped := 0
		if budget != nil {
			sort.SliceStable(items, func(i, j int) bool { return priority(items[i]) < priority(items[j]) })
			kept := make([]wire.SyncItem, 0, len(items))
			for _, it := range items {
				// Control packets always pass; data only within budget.
				if priority(it) <= 1 || budget.Remaining >= int64(len(it.Raw)) {
					budget.Remaining -= int64(len(it.Raw))
					kept = append(kept, it)
				} else {
					dropped++
				}
			}
			items = kept
		}
		if len(items) > 0 {
			if err := to.Push(ctx, items); err != nil {
				return moved, err // cursor not advanced: retried next round
			}
		}
		moved += len(items)
		if dropped > 0 {
			// Out of budget: keep the cursor so skipped data is retried when
			// the budget refills (already-pushed items deduplicate).
			return moved, nil
		}
		*cursor = next
		if !more {
			return moved, nil
		}
	}
}

// Package store is the store-and-forward packet store used by every router
// and relay. Every stored item gets a monotonically increasing sequence
// number so peers can sync incrementally with a cursor.
package store

import (
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Item is one stored packet (or fragment).
type Item struct {
	Seq      int64
	Key      packet.Key
	Type     packet.Type
	Dest     packet.Hash
	Raw      []byte
	Origin   string // where it came from (peer/session label), for echo suppression
	Region   string // relays: home region learned for announces
	Received time.Time
	Expires  time.Time
}

// Filter narrows Since queries.
type Filter struct {
	ExcludeOrigin string        // skip items that came from this origin
	Types         []packet.Type // empty = all types
}

func (f Filter) match(it *Item) bool {
	if f.ExcludeOrigin != "" && it.Origin == f.ExcludeOrigin {
		return false
	}
	if len(f.Types) == 0 {
		return true
	}
	for _, t := range f.Types {
		if it.Type == t {
			return true
		}
	}
	return false
}

// Store is implemented by Memory and SQLite.
type Store interface {
	// Put stores it unless an item with the same key exists. For announces,
	// older announces for the same destination are replaced.
	Put(it Item) (seq int64, added bool, err error)
	Has(k packet.Key) (bool, error)
	Get(k packet.Key) (Item, bool, error)
	// Since returns items with Seq > after in Seq order (at most limit).
	Since(after int64, limit int, f Filter) ([]Item, error)
	// ByDest returns DATA items for dest with Seq > after.
	ByDest(dest packet.Hash, after int64, limit int) ([]Item, error)
	// LatestAnnounce returns the newest announce for dest.
	LatestAnnounce(dest packet.Hash) (Item, bool, error)
	// DeleteData removes all fragments of a DATA packet (on receipt).
	DeleteData(id packet.ID) (int, error)
	// Expire removes items whose Expires is before now.
	Expire(now time.Time) (int, error)
	// MaxSeq returns the highest sequence number assigned so far.
	MaxSeq() (int64, error)
	Count() (int, error)
	Close() error
}

package store

import (
	"sort"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Memory is an in-memory Store (tests, simulator, small devices).
type Memory struct {
	mu    sync.Mutex
	seq   int64
	items map[packet.Key]*Item
}

func NewMemory() *Memory { return &Memory{items: map[packet.Key]*Item{}} }

func (m *Memory) Put(it Item) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.items[it.Key]; ok {
		return old.Seq, false, nil
	}
	if it.Type == packet.TypeAnnounce {
		for k, old := range m.items {
			if old.Type == packet.TypeAnnounce && old.Dest == it.Dest {
				delete(m.items, k)
			}
		}
	}
	m.seq++
	it.Seq = m.seq
	it.Raw = append([]byte(nil), it.Raw...)
	m.items[it.Key] = &it
	return it.Seq, true, nil
}

func (m *Memory) Has(k packet.Key) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.items[k]
	return ok, nil
}

func (m *Memory) sorted(match func(*Item) bool, after int64, limit int) []Item {
	var out []Item
	for _, it := range m.items {
		if it.Seq > after && match(it) {
			out = append(out, *it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Memory) Since(after int64, limit int, f Filter) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(f.match, after, limit), nil
}

func (m *Memory) ByDest(dest packet.Hash, after int64, limit int) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(func(it *Item) bool { return it.Type == packet.TypeData && it.Dest == dest }, after, limit), nil
}

func (m *Memory) LatestAnnounce(dest packet.Hash) (Item, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.items {
		if it.Type == packet.TypeAnnounce && it.Dest == dest {
			return *it, true, nil
		}
	}
	return Item{}, false, nil
}

func (m *Memory) DeleteData(id packet.ID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, it := range m.items {
		if k.ID == id && it.Type == packet.TypeData {
			delete(m.items, k)
			n++
		}
	}
	return n, nil
}

func (m *Memory) Expire(now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, it := range m.items {
		if !it.Expires.IsZero() && it.Expires.Before(now) {
			delete(m.items, k)
			n++
		}
	}
	return n, nil
}

func (m *Memory) MaxSeq() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq, nil
}

func (m *Memory) Count() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items), nil
}

func (m *Memory) Close() error { return nil }

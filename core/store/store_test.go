package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

func each(t *testing.T, fn func(t *testing.T, s Store)) {
	t.Run("memory", func(t *testing.T) { fn(t, NewMemory()) })
	t.Run("sqlite", func(t *testing.T) {
		s, err := OpenSQLite(filepath.Join(t.TempDir(), "s.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		fn(t, s)
	})
}

func item(id byte, frag uint8, typ packet.Type, dest byte, origin string, exp time.Time) Item {
	return Item{
		Key:  packet.Key{ID: packet.ID{id}, Frag: frag},
		Type: typ, Dest: packet.Hash{dest}, Raw: []byte{id, frag},
		Origin: origin, Received: time.Unix(1000, 0), Expires: exp,
	}
}

func TestStore(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		future := time.Unix(5000, 0)
		seq1, added, err := s.Put(item(1, 0, packet.TypeData, 9, "a", future))
		if err != nil || !added || seq1 != 1 {
			t.Fatal(seq1, added, err)
		}
		if _, added, _ := s.Put(item(1, 0, packet.TypeData, 9, "a", future)); added {
			t.Fatal("duplicate added")
		}
		s.Put(item(1, 1, packet.TypeData, 9, "b", future))
		s.Put(item(2, 0, packet.TypeData, 8, "b", time.Unix(1500, 0)))
		s.Put(item(3, 0, packet.TypeReceipt, 9, "a", future))

		if ok, _ := s.Has(packet.Key{ID: packet.ID{1}, Frag: 1}); !ok {
			t.Fatal("Has failed")
		}
		if it, ok, _ := s.Get(packet.Key{ID: packet.ID{1}, Frag: 1}); !ok || it.Origin != "b" || it.Raw[1] != 1 {
			t.Fatal("Get failed")
		}
		if _, ok, _ := s.Get(packet.Key{ID: packet.ID{99}}); ok {
			t.Fatal("Get found missing item")
		}
		all, _ := s.Since(0, 0, Filter{})
		if len(all) != 4 || all[0].Seq >= all[3].Seq {
			t.Fatalf("since: %d", len(all))
		}
		ex, _ := s.Since(0, 0, Filter{ExcludeOrigin: "a"})
		if len(ex) != 2 {
			t.Fatalf("exclude origin: %d", len(ex))
		}
		rc, _ := s.Since(0, 0, Filter{Types: []packet.Type{packet.TypeReceipt}})
		if len(rc) != 1 {
			t.Fatal("type filter")
		}
		bd, _ := s.ByDest(packet.Hash{9}, 0, 0)
		if len(bd) != 2 {
			t.Fatalf("by dest: %d", len(bd))
		}
		page, _ := s.Since(0, 2, Filter{})
		rest, _ := s.Since(page[1].Seq, 0, Filter{})
		if len(page) != 2 || len(rest) != 2 {
			t.Fatal("paging")
		}

		if n, _ := s.DeleteData(packet.ID{1}); n != 2 {
			t.Fatalf("delete data: %d", n)
		}
		if n, _ := s.Expire(time.Unix(2000, 0)); n != 1 {
			t.Fatalf("expire: %d", n)
		}
		if n, _ := s.Count(); n != 1 {
			t.Fatalf("count: %d", n)
		}
		if m, _ := s.MaxSeq(); m != 4 {
			t.Fatalf("max seq after deletes: %d", m)
		}
	})
}

func TestAnnounceReplace(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		s.Put(item(1, 0, packet.TypeAnnounce, 7, "", time.Time{}))
		s.Put(item(2, 0, packet.TypeAnnounce, 7, "", time.Time{}))
		it, ok, err := s.LatestAnnounce(packet.Hash{7})
		if err != nil || !ok || it.Key.ID != (packet.ID{2}) {
			t.Fatal("latest announce", ok, err)
		}
		if n, _ := s.Count(); n != 1 {
			t.Fatal("old announce not replaced")
		}
	})
}

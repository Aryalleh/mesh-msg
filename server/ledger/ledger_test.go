package ledger_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/ledger"
)

func open(t *testing.T) *ledger.Ledger {
	l, err := ledger.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

// upload makes the entry a relay writes for an account's signed upload.
func upload(t *testing.T, relay, acct *identity.Identity, media string, day uint32) ledger.Entry {
	t.Helper()
	sig := acct.SignDomain(wire.DomainMediaUpload, wire.MediaSigBody(media, day))
	e, err := ledger.New(relay, day, media, acct.Public, sig)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const (
	f1 = "00112233445566778899aabbccddeeff"
	f2 = "ffeeddccbbaa99887766554433221100"
)

func TestEntriesVerifyCountAndSpread(t *testing.T) {
	relayA, _ := identity.Generate()
	relayB, _ := identity.Generate()
	ali, _ := identity.Generate()
	day := wire.Day(time.Now())

	e := upload(t, relayA, ali, f1, day)
	d, err := ledger.Decode(e.Encode())
	if err != nil || !bytes.Equal(d.Encode(), e.Encode()) || len(e.Encode()) != ledger.EntrySize {
		t.Fatal("encode/decode", err)
	}
	if _, err := d.Verify(); err != nil {
		t.Fatal(err)
	}

	l := open(t)
	// The same file through two relays counts once.
	if n, _ := l.Add([]ledger.Entry{e, upload(t, relayB, ali, f1, day)}, ""); n != 1 {
		t.Fatalf("added %d, want 1 (deduplicated per account and file)", n)
	}
	l.Add([]ledger.Entry{upload(t, relayB, ali, f2, day)}, "b")
	if u := l.Used(ali.Hash().String(), day); u != 2 {
		t.Fatalf("used %d", u)
	}
	if !l.Has(ali.Public.Bytes(), d.Media) {
		t.Fatal("Has")
	}

	// Another relay pulls everything, page by page.
	l2 := open(t)
	var cur int64
	for {
		es, next, more, err := l.Since(cur, 1)
		if err != nil {
			t.Fatal(err)
		}
		l2.Add(es, "a")
		cur = next
		if !more {
			break
		}
	}
	if u := l2.Used(ali.Hash().String(), day); u != 2 {
		t.Fatalf("copy: used %d", u)
	}
}

func TestForgedEntriesRejected(t *testing.T) {
	relay, _ := identity.Generate()
	ali, _ := identity.Generate()
	mallory, _ := identity.Generate()
	day := wire.Day(time.Now())
	l := open(t)

	// A relay can't invent an upload: the account signature doesn't match.
	forged, _ := ledger.New(relay, day, f1, ali.Public, mallory.SignDomain(wire.DomainMediaUpload, wire.MediaSigBody(f1, day)))
	if n, _ := l.Add([]ledger.Entry{forged}, "x"); n != 0 || l.Used(ali.Hash().String(), day) != 0 {
		t.Fatal("forged upload counted")
	}
	// …and that relay is ignored from then on, even for real entries.
	if n, _ := l.Add([]ledger.Entry{upload(t, relay, ali, f2, day)}, "x"); n != 0 {
		t.Fatal("entry from a blocked relay accepted")
	}
	// A broken relay signature counts against the sender, not the relay.
	other, _ := identity.Generate()
	e := upload(t, other, ali, f2, day)
	e.RelaySig[0] ^= 1
	if n, _ := l.Add([]ledger.Entry{e}, "y"); n != 0 {
		t.Fatal("bad relay signature accepted")
	}
	// Signed for another day: not this upload.
	e = upload(t, other, ali, f2, day)
	e.Day--
	if n, _ := l.Add([]ledger.Entry{e}, "y"); n != 0 {
		t.Fatal("entry with a changed day accepted")
	}
	st := l.Stats()
	if st.BadSource < 2 {
		t.Fatalf("panel stats: %+v", st)
	}
}

func TestExpireAndAges(t *testing.T) {
	relay, _ := identity.Generate()
	ali, _ := identity.Generate()
	l := open(t)
	now := time.Now()
	l.Now = func() time.Time { return now }
	day := wire.Day(now)
	l.Add([]ledger.Entry{upload(t, relay, ali, f1, day-1), upload(t, relay, ali, f2, day)}, "")
	// A day later: today and yesterday are kept, the day before goes.
	l.Now = func() time.Time { return now.Add(24 * time.Hour) }
	l.Expire()
	if l.Used(ali.Hash().String(), day-1) != 0 || l.Used(ali.Hash().String(), day) != 1 {
		t.Fatal("expire after one day")
	}
	l.Now = func() time.Time { return now.Add(48 * time.Hour) }
	l.Expire()
	if l.Used(ali.Hash().String(), day) != 0 {
		t.Fatal("expire after two days")
	}
	l.Now = func() time.Time { return now }
	// Too old to matter: not taken.
	if n, _ := l.Add([]ledger.Entry{upload(t, relay, ali, f1, day-3)}, ""); n != 0 {
		t.Fatal("old entry taken")
	}

	h := ali.Hash().String()
	if _, ok := l.FirstSeen(h); ok {
		t.Fatal("unknown account has an age")
	}
	l.Seen(h, now)
	l.Seen(h, now.Add(time.Hour))     // later: ignored
	l.Seen(h, now.Add(-48*time.Hour)) // earlier (a peer saw it first): kept
	if t0, _ := l.FirstSeen(h); !t0.Equal(now.Add(-48 * time.Hour).Truncate(time.Second)) {
		t.Fatalf("first seen %v", t0)
	}
}

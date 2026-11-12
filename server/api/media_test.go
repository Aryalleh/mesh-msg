package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/media"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/ledger"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

// easy is a policy for tests: little work, no new-account limits.
func easy(perDay int) *api.MediaPolicy {
	p := api.DefaultMediaPolicy()
	p.PerDay, p.NewAccountDays, p.PowBaseBits, p.AgeTiers = perDay, 0, 4, nil
	return &p
}

func mediaRelay(t *testing.T, cfg api.MediaConfig) *relayclient.Client {
	if cfg.Policy == nil {
		cfg.Policy = easy(5)
	}
	key, _ := identity.Generate()
	cfg.Dir = t.TempDir()
	srv := httptest.NewServer(api.New(api.Config{
		Relay: relay.New(relay.Config{Name: "m-1", Region: "global"}), Identity: key, PowBits: 8, RateLimit: 1000, Media: cfg,
	}))
	t.Cleanup(srv.Close)
	c, _ := relayclient.New(srv.URL, relayclient.Options{})
	return c
}

func blob(t *testing.T, n int) ([]byte, string) {
	f := make([]byte, n)
	rand.Read(f)
	b, _, id, err := media.Seal(f)
	if err != nil {
		t.Fatal(err)
	}
	return b, id.String()
}

func is404(err error) bool {
	var se *relayclient.StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

func TestMediaDeletedAfterAllReaders(t *testing.T) {
	ctx := context.Background()
	c := mediaRelay(t, api.MediaConfig{MaxMB: 2})
	me, _ := identity.Generate()
	if c.MediaMaxBytes(ctx) != 2<<20 {
		t.Fatal("info does not announce media")
	}
	b, id := blob(t, 300_000)
	if err := c.PutMedia(ctx, id, b, 2, me); err != nil {
		t.Fatal(err)
	}
	// Reader 1 is cut off half way and resumes.
	var got bytes.Buffer
	got.Write(b[:1000])
	if total, err := c.GetMedia(ctx, id, 1000, &got); err != nil || total != int64(len(b)) || !bytes.Equal(got.Bytes(), b) {
		t.Fatal("resume", err, total)
	}
	// Reader 2 takes it whole: then it is gone.
	got.Reset()
	if _, err := c.GetMedia(ctx, id, 0, &got); err != nil || !bytes.Equal(got.Bytes(), b) {
		t.Fatal(err)
	}
	if _, err := c.GetMedia(ctx, id, 0, &got); !is404(err) {
		t.Fatalf("still there after every reader: %v", err)
	}
}

func TestMediaLimits(t *testing.T) {
	ctx := context.Background()
	c := mediaRelay(t, api.MediaConfig{MaxMB: 1, StoreMB: 1})
	me, _ := identity.Generate()
	big, id := blob(t, 1<<20+100)
	if err := c.PutMedia(ctx, id, big, 1, me); err == nil {
		t.Fatal("file over the relay's limit accepted")
	}
	b1, id1 := blob(t, 700_000)
	if err := c.PutMedia(ctx, id1, b1, 1, me); err != nil {
		t.Fatal(err)
	}
	b2, id2 := blob(t, 700_000)
	var se *relayclient.StatusError
	if err := c.PutMedia(ctx, id2, b2, 1, me); !errors.As(err, &se) || se.Code != http.StatusInsufficientStorage {
		t.Fatalf("store cap: %v", err)
	}
	// Content must match its id.
	if err := c.PutMedia(ctx, id2, b1, 1, me); err == nil {
		t.Fatal("blob under another id accepted")
	}
	// No media relay: refused before uploading.
	off := mediaRelay(t, api.MediaConfig{})
	if err := off.PutMedia(ctx, id1, b1, 1, me); err == nil {
		t.Fatal("media accepted by a relay without media")
	}
}

func TestMediaAllowance(t *testing.T) {
	ctx := context.Background()
	c := mediaRelay(t, api.MediaConfig{MaxMB: 1, Policy: easy(2)})
	me, _ := identity.Generate()
	other, _ := identity.Generate()
	if in, _ := c.Info(ctx); in.MediaPerDay != 2 || in.MediaImportantHours != 168 {
		t.Fatalf("info: %+v", in)
	}
	q, err := c.MediaQuota(ctx, me)
	if err != nil || q.PerDay != 2 || q.Left != 2 || !q.Allowed {
		t.Fatalf("quota %+v %v", q, err)
	}
	b1, id1 := blob(t, 1000)
	b2, id2 := blob(t, 1000)
	b3, id3 := blob(t, 1000)
	for _, f := range []struct {
		b  []byte
		id string
	}{{b1, id1}, {b2, id2}} {
		if err := c.PutMedia(ctx, f.id, f.b, 3, me); err != nil {
			t.Fatal(err)
		}
	}
	// The same file again (another try, or to a group) counts once.
	if err := c.PutMedia(ctx, id1, b1, 3, me); err != nil {
		t.Fatal(err)
	}
	err = c.PutMedia(ctx, id3, b3, 1, me)
	if !relayclient.IsAllowanceUsed(err) {
		t.Fatalf("third file: %v", err)
	}
	if q, _ := c.MediaQuota(ctx, me); q.Used != 2 || q.Left != 0 || q.Resets <= time.Now().Unix() {
		t.Fatalf("quota after: %+v", q)
	}
	// Another account has its own allowance.
	if err := c.PutMedia(ctx, id3, b3, 1, other); err != nil {
		t.Fatal(err)
	}
	// Unsigned uploads are refused.
	if err := c.PutMedia(ctx, id3, b3, 1, nil); err == nil {
		t.Fatal("unsigned upload accepted")
	}
	b4, id4 := blob(t, 1000)
	req, _ := http.NewRequest("PUT", c.URL()+"/v1/media/"+id4, bytes.NewReader(b4))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("raw unsigned PUT: %v %v", resp, err)
	}
}

func TestMediaAccounts(t *testing.T) {
	ctx := context.Background()
	member, _ := identity.Generate()
	stranger, _ := identity.Generate()
	c := mediaRelay(t, api.MediaConfig{MaxMB: 1, Accounts: []string{member.Hash().String()}})
	b, id := blob(t, 1000)
	var se *relayclient.StatusError
	if err := c.PutMedia(ctx, id, b, 1, stranger); !errors.As(err, &se) || se.Code != http.StatusForbidden {
		t.Fatalf("stranger: %v", err)
	}
	if err := c.PutMedia(ctx, id, b, 1, nil); err == nil {
		t.Fatal("unsigned upload accepted")
	}
	if err := c.PutMedia(ctx, id, b, 1, member); err != nil {
		t.Fatal(err)
	}
	// Anyone may fetch it.
	var got bytes.Buffer
	if _, err := c.GetMedia(ctx, id, 0, &got); err != nil || !bytes.Equal(got.Bytes(), b) {
		t.Fatal(err)
	}
}

// Step 2: account age, proof-of-work that grows, and one allowance across
// relays through the shared ledger.
func TestMediaAgeWorkAndLedger(t *testing.T) {
	ctx := context.Background()
	p := api.DefaultMediaPolicy()
	p.PerDay, p.NewAccountPerDay, p.PowBaseBits, p.PowMaxBits = 3, 1, 2, 12
	p.AgeTiers = []api.AgeTier{{3, 3}, {30, 2}}
	newRelay := func() (*relayclient.Client, *ledger.Ledger) {
		l, _ := ledger.Open(":memory:")
		t.Cleanup(func() { l.Close() })
		key, _ := identity.Generate()
		srv := httptest.NewServer(api.New(api.Config{
			Relay: relay.New(relay.Config{Name: "m", Region: "global"}), Identity: key, RateLimit: 1000, Ledger: l,
			Media: api.MediaConfig{Dir: t.TempDir(), MaxMB: 1, Policy: &p},
		}))
		t.Cleanup(srv.Close)
		c, _ := relayclient.New(srv.URL, relayclient.Options{})
		return c, l
	}
	a, la := newRelay()
	b, lb := newRelay()
	newbie, _ := identity.Generate()
	old, _ := identity.Generate()
	la.Seen(old.Hash().String(), time.Now().Add(-10*24*time.Hour))
	lb.Seen(old.Hash().String(), time.Now().Add(-10*24*time.Hour))

	// A brand-new account: 1 file a day and 3 more bits of work.
	q, _ := a.MediaQuota(ctx, newbie)
	if q.PerDay != 1 || q.PowBits != 2+3 || q.AgeDays != 0 {
		t.Fatalf("new account: %+v", q)
	}
	// Ten days old: the full allowance, 2 age bits; each file adds a bit.
	q, _ = a.MediaQuota(ctx, old)
	if q.PerDay != 3 || q.PowBits != 2+2 || q.AgeDays != 10 {
		t.Fatalf("old account: %+v", q)
	}
	b1, id1 := blob(t, 1000)
	if err := a.PutMedia(ctx, id1, b1, 1, old); err != nil {
		t.Fatal(err)
	}
	if q, _ = a.MediaQuota(ctx, old); q.Used != 1 || q.PowBits != 2+2+1 {
		t.Fatalf("after one: %+v", q)
	}

	// Relay b hears of it through the ledger (pull from a, as the sync loop does).
	es, _, _, err := a.PullUsage(ctx, 0)
	if err != nil || len(es) != ledger.EntrySize {
		t.Fatalf("pull: %d bytes, %v", len(es), err)
	}
	if n, err := b.PushUsage(ctx, es); err != nil || n != 1 {
		t.Fatalf("push: %d %v", n, err)
	}
	if q, _ = b.MediaQuota(ctx, old); q.Used != 1 || q.Left != 2 {
		t.Fatalf("relay b doesn't count the upload on a: %+v", q)
	}
	// The same file to b counts once; two new ones fill the allowance.
	if err := b.PutMedia(ctx, id1, b1, 1, old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		bb, id := blob(t, 1000)
		if err := b.PutMedia(ctx, id, bb, 1, old); err != nil {
			t.Fatal(i, err)
		}
	}
	bb, id := blob(t, 1000)
	if err := b.PutMedia(ctx, id, bb, 1, old); !relayclient.IsAllowanceUsed(err) {
		t.Fatalf("fourth file across relays: %v", err)
	}
	if lb.Used(old.Hash().String(), wire.Day(time.Now())) != 3 {
		t.Fatal("ledger count on b")
	}
	// Garbage pushed to the ledger is refused.
	if _, err := b.PushUsage(ctx, []byte("not an entry")); err == nil {
		t.Fatal("garbage accepted")
	}
}

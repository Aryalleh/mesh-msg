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
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

func mediaRelay(t *testing.T, cfg api.MediaConfig) *relayclient.Client {
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
	if c.MediaMaxBytes(ctx) != 2<<20 {
		t.Fatal("info does not announce media")
	}
	b, id := blob(t, 300_000)
	if err := c.PutMedia(ctx, id, b, 2, nil); err != nil {
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
	big, id := blob(t, 1<<20+100)
	if err := c.PutMedia(ctx, id, big, 1, nil); err == nil {
		t.Fatal("file over the relay's limit accepted")
	}
	b1, id1 := blob(t, 700_000)
	if err := c.PutMedia(ctx, id1, b1, 1, nil); err != nil {
		t.Fatal(err)
	}
	b2, id2 := blob(t, 700_000)
	var se *relayclient.StatusError
	if err := c.PutMedia(ctx, id2, b2, 1, nil); !errors.As(err, &se) || se.Code != http.StatusInsufficientStorage {
		t.Fatalf("store cap: %v", err)
	}
	// Content must match its id.
	if err := c.PutMedia(ctx, id2, b1, 1, nil); err == nil {
		t.Fatal("blob under another id accepted")
	}
	// No media relay: refused before uploading.
	off := mediaRelay(t, api.MediaConfig{})
	if err := off.PutMedia(ctx, id1, b1, 1, nil); err == nil {
		t.Fatal("media accepted by a relay without media")
	}
}

func TestMediaTTL(t *testing.T) {
	c := api.MediaConfig{MaxMB: 16}
	for size, want := range map[int64]time.Duration{
		10_000: 8 * time.Hour, 1 << 20: 8 * time.Hour, 2 << 20: 4 * time.Hour, 4 << 20: 2 * time.Hour, 16 << 20: time.Hour,
	} {
		if got := c.TTL(size); got != want {
			t.Errorf("%d bytes: %v, want %v", size, got, want)
		}
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

package update

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Cache is what a relay keeps to answer GET /v1/release: the newest verified
// manifest per channel, refreshed from GitHub about once an hour, so apps in
// places where GitHub is blocked still learn about releases.
type Cache struct {
	Source string // releases API; "" = GitHubReleasesAPI
	// Upstream: another relay's base URL to take manifests from instead of
	// GitHub (for relays in places where GitHub is blocked).
	Upstream string
	Key      ed25519.PublicKey // nil = ReleaseKey()
	Client   *http.Client
	Logf     func(string, ...any)

	mu  sync.RWMutex
	raw map[string][]byte    // channel → release.json bytes
	man map[string]*Manifest // channel → manifest
}

func (c *Cache) key() ed25519.PublicKey {
	if c.Key != nil {
		return c.Key
	}
	return ReleaseKey()
}

// Get returns the cached release.json for a channel ("beta" or "stable").
func (c *Cache) Get(ch string) ([]byte, *Manifest) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.raw[ch], c.man[ch]
}

// Put stores a manifest if it verifies and is newer than what the channel
// holds. It returns whether anything changed.
func (c *Cache) Put(data []byte) bool {
	m, err := Verify(data, c.key())
	if err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raw == nil {
		c.raw, c.man = map[string][]byte{}, map[string]*Manifest{}
	}
	changed := false
	for _, ch := range []string{ChannelBeta, ChannelStable} {
		if !Accepts(ch, m.Channel) {
			continue
		}
		if old := c.man[ch]; old == nil || Compare(m.Version, old.Version) > 0 {
			c.raw[ch], c.man[ch] = append([]byte(nil), data...), m
			changed = true
		}
	}
	return changed
}

// Refresh fetches the newest manifests for both channels once.
func (c *Cache) Refresh(ctx context.Context) error {
	cl := c.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	var firstErr error
	for _, ch := range []string{ChannelStable, ChannelBeta} {
		var raw []byte
		var err error
		if c.Upstream != "" {
			_, raw, err = FetchManifest(ctx, cl, strings.TrimRight(c.Upstream, "/")+"/v1/release?channel="+ch, c.key())
		} else {
			_, raw, err = FetchGitHub(ctx, cl, cmp.Or(c.Source, GitHubReleasesAPI), ch, c.key())
		}
		if err != nil {
			firstErr = cmp.Or(firstErr, err)
			continue
		}
		if c.Put(raw) && c.Logf != nil {
			_, m := c.Get(ch)
			c.Logf("release manifest: %s now %s", ch, m.Version)
		}
	}
	return firstErr
}

// Run refreshes at start and then every ~10 minutes (with jitter) until ctx
// ends: a new release reaches clients quickly (GitHub allows 60 unauthenticated
// API calls per hour per address).
func (c *Cache) Run(ctx context.Context) {
	for {
		if err := c.Refresh(ctx); err != nil && c.Logf != nil {
			c.Logf("release manifest refresh: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(9*time.Minute + time.Duration(rand.Int64N(int64(2*time.Minute)))):
		}
	}
}

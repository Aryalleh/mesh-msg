package meshcore

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/Aryalleh/mesh-msg/core/update"
)

// Update check (roadmap B7). The app calls CheckUpdate at start and about once
// a day; the manifest is verified against the release key built into the core.

// updateRelays: configured relays reached directly, then discovered relays
// that work. With a relay behind Tor or a proxy, only the direct ones are asked.
func (c *Core) updateRelays() []string {
	var out []string
	proxied := false
	for _, r := range c.relays {
		if r.Via == "" || r.Via == "direct" {
			out = append(out, r.URL)
		} else {
			proxied = true
		}
	}
	if proxied {
		return out
	}
	for _, k := range c.n.KnownRelays() {
		if k.State == "ok" && !slices.Contains(out, k.URL) {
			out = append(out, k.URL)
		}
	}
	return out
}

// CheckUpdate asks the relays, then GitHub, for the newest signed release and
// returns the result for the running version as JSON:
// {"current","channel","checked","source","latest":{manifest}|null,"available","required","error"}.
func (c *Core) CheckUpdate(current string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	chk := &update.Checker{Current: current, Relays: c.updateRelays}
	s, _ := toJSON(chk.Check(ctx), nil)
	return s
}

// DownloadUpdate downloads url to path and checks it against the SHA-256 and
// size from the signed manifest; on a mismatch the file is deleted and an
// error returned.
func DownloadUpdate(url, sha256 string, size int64, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	f := &update.File{Name: path, URL: url, SHA256: sha256, Size: size}
	return update.Download(ctx, &http.Client{}, f, path)
}

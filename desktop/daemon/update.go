package daemon

import (
	"context"
	"net/http"
	"slices"
	"sync"

	"github.com/Aryalleh/mesh-msg/core/update"
)

// Update check (roadmap B7): meshd asks its relays (then GitHub) for the
// newest signed release manifest at start and about once a day, and shows it
// in the web UI, `mesh status` and `mesh update`.
type updater struct {
	mu  sync.RWMutex
	st  update.Status
	chk *update.Checker
	now chan struct{}
}

// SetVersion sets the running version (meshd's -X main.version) and turns on
// the update check. Development builds (0.0.0-dev…) don't check.
func (d *Daemon) SetVersion(v string) {
	d.version = v
}

// updateRelays are the relays the update check may ask: configured relays
// reached directly (relays behind Tor or a proxy are skipped, so the check
// doesn't bypass the path the user chose), then discovered relays that work.
func (d *Daemon) updateRelays() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []string
	proxied := false
	for _, r := range d.cfg.Relays {
		if r.Via == "" || r.Via == "direct" {
			out = append(out, r.URL)
		} else {
			proxied = true
		}
	}
	if proxied {
		return out // the user hides their address from relays; don't contact others directly
	}
	for _, k := range d.node.KnownRelays() {
		if k.State == "ok" && !k.Config && !slices.Contains(out, k.URL) {
			out = append(out, k.URL)
		}
	}
	return out
}

func (d *Daemon) startUpdater(ctx context.Context) {
	u := &updater{now: make(chan struct{}, 1)}
	u.st = update.Status{Current: d.version, Channel: update.ChannelOf(d.version)}
	d.upd = u
	if d.cfg.NoUpdateCheck || !update.IsRelease(d.version) {
		return
	}
	u.chk = &update.Checker{Current: d.version, Relays: d.updateRelays, Logf: d.cfg.Logf}
	go u.chk.Run(ctx, u.set)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-u.now:
				u.set(u.chk.Check(ctx))
			}
		}
	}()
}

func (u *updater) set(st update.Status) {
	u.mu.Lock()
	u.st = st
	u.mu.Unlock()
}

func (u *updater) get() update.Status {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.st
}

func (d *Daemon) updateAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /update", func(w http.ResponseWriter, r *http.Request) {
		if d.upd == nil {
			writeJSON(w, update.Status{Current: d.version})
			return
		}
		writeJSON(w, d.upd.get())
	})
	// POST /update/check runs a check now and answers with its result.
	mux.HandleFunc("POST /update/check", func(w http.ResponseWriter, r *http.Request) {
		if d.upd == nil || d.upd.chk == nil {
			writeJSON(w, update.Status{Current: d.version, Error: "update check is off for this build"})
			return
		}
		st := d.upd.chk.Check(r.Context())
		d.upd.set(st)
		writeJSON(w, st)
	})
}

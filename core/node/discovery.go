package node

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/network"
)

// Relay discovery: the mesh keeps its relay list up to date by itself.
//
// Candidates come from
//   - the official list (network/relays.json) compiled into the build and
//     refreshed from the repository,
//   - the signed lists relays serve (GET /v1/relays),
//   - nearby devices: every node puts a relay it reaches right now into its
//     announces (announce.TagRelay); phones, PCs and LoRa nodes pass announces
//     on, so a hint crosses Bluetooth, LAN and LoRa hops.
//
// A candidate is only used after this node reached it (GET /v1/info). The
// relay's key is pinned: from the official list when it is there, otherwise
// on first contact. A relay that later shows another key is dropped.
// Messages stay end-to-end encrypted; a bad relay can at most drop packets or
// see metadata, which is why candidates from strangers are capped and private
// network addresses are refused.

// DiscoveryConfig controls relay discovery (on by default).
type DiscoveryConfig struct {
	Off bool `toml:"off" json:"off"` // don't learn or share relays
	// ListURL is where the official list is refreshed from; "" means
	// network.SourceURL, "none" skips the official list (tests, closed meshes).
	ListURL string `toml:"list_url" json:"list_url,omitempty"`
	// MaxAuto caps relays used on top of the configured ones (default 6).
	MaxAuto int `toml:"max_auto" json:"max_auto,omitempty"`
	// AllowPrivate accepts relay hints on private / loopback addresses
	// (tests, LAN-only deployments).
	AllowPrivate bool `toml:"-" json:"-"`
}

// KnownRelay is a relay this node has heard of.
type KnownRelay struct {
	URL    string    `json:"url"`
	Name   string    `json:"name"`
	Region string    `json:"region"`
	PubKey string    `json:"pubkey,omitempty"`
	Source string    `json:"source"` // bootstrap | list | relay | mesh
	State  string    `json:"state"`  // new | ok | bad | forgotten
	Active bool      `json:"active"` // in use as an automatic relay
	Config bool      `json:"config"` // also in the configured relays
	LastOK time.Time `json:"last_ok,omitempty"`
}

const relaysSchema = `
CREATE TABLE IF NOT EXISTS relays_known (
	url        TEXT PRIMARY KEY,
	name       TEXT NOT NULL DEFAULT '',
	region     TEXT NOT NULL DEFAULT '',
	pubkey     TEXT NOT NULL DEFAULT '',
	source     TEXT NOT NULL,
	first_seen INTEGER NOT NULL,
	last_ok    INTEGER NOT NULL DEFAULT 0,
	last_try   INTEGER NOT NULL DEFAULT 0,
	fails      INTEGER NOT NULL DEFAULT 0,
	state      TEXT NOT NULL DEFAULT 'new'
);`

const (
	discoverEvery  = 5 * time.Minute
	listEvery      = 6 * time.Hour
	relayListEvery = time.Hour
	probesPerRound = 4
	maxCandidates  = 300
	dropAfter      = 24 * time.Hour // an automatic relay unreachable this long is dropped
	reannounceGap  = 10 * time.Minute
)

type discovery struct {
	n    *Node
	cfg  DiscoveryConfig
	kick chan struct{}

	mu       sync.Mutex
	auto     map[string]*autoRelay // url → running automatic relay
	lastHint time.Time
}

type autoRelay struct {
	link   *relayLink
	cancel context.CancelFunc
	done   chan struct{}
}

func trusted(source string) bool { return source == "bootstrap" || source == "list" }

func (d *discovery) maxAuto() int {
	if d.cfg.MaxAuto > 0 {
		return d.cfg.MaxAuto
	}
	return 6
}

// add records a candidate. A pubkey is kept only from a trusted source or
// if the row has none yet (first contact pins it).
func (d *discovery) add(rawURL, name, region, pubkey, source string) {
	u, err := network.CheckURL(rawURL, d.cfg.AllowPrivate)
	if err != nil {
		return
	}
	db := d.n.db.db
	now := time.Now().Unix()
	db.Exec(`INSERT OR IGNORE INTO relays_known(url,name,region,pubkey,source,first_seen) VALUES(?,?,?,?,?,?)`,
		u, name, region, strings.ToLower(pubkey), source, now)
	if trusted(source) {
		// The official list wins: its name, region and key.
		db.Exec(`UPDATE relays_known SET source=?, name=?, region=?,
			pubkey=CASE WHEN ?<>'' THEN ? ELSE pubkey END,
			state=CASE WHEN state='bad' AND ?<>'' AND pubkey<>? THEN 'new' ELSE state END
			WHERE url=? AND state<>'forgotten'`, source, name, region, pubkey, strings.ToLower(pubkey), pubkey, strings.ToLower(pubkey), u)
	}
	// Bound what strangers can fill the table with.
	db.Exec(`DELETE FROM relays_known WHERE url IN (SELECT url FROM relays_known
		WHERE source NOT IN ('bootstrap','list') AND state IN ('new','bad')
		ORDER BY last_ok, first_seen LIMIT max(0, (SELECT COUNT(*) FROM relays_known) - ?))`, maxCandidates)
}

func (d *discovery) poke() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// onAnnounce takes relay hints from the mesh.
func (n *Node) onAnnounce(a announce.Announce, _ string, _ uint8) {
	if d := n.disc; d != nil && a.App.Relay != "" {
		d.add(a.App.Relay, "", "", "", "mesh")
		d.poke()
	}
}

// configured reports whether u (or a relay with this key) is a configured relay.
func (d *discovery) configured(u string) bool {
	for _, rc := range d.n.cfg.Relays {
		if c, err := network.CheckURL(rc.URL, true); err == nil && c == u {
			return true
		}
	}
	return false
}

func (d *discovery) run(ctx context.Context) {
	if d.cfg.ListURL != "none" {
		for _, r := range network.Bootstrap().Relays {
			for _, u := range r.URLs {
				d.add(u, r.Name, r.Region, r.PubKey, "bootstrap")
			}
		}
	}
	d.startKnown(ctx)
	var lastList, lastRelayLists time.Time
	t := time.NewTicker(discoverEvery)
	defer t.Stop()
	first := time.After(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			d.stopAll()
			return
		case <-first:
		case <-t.C:
		case <-d.kick:
			// Let a burst of announces settle.
			if !sleepCtx(ctx, 3*time.Second) {
				d.stopAll()
				return
			}
		}
		if d.cfg.ListURL != "none" && time.Since(lastList) > listEvery {
			if d.fetchList(ctx) {
				lastList = time.Now()
			}
		}
		if time.Since(lastRelayLists) > relayListEvery && d.fetchRelayLists(ctx) {
			lastRelayLists = time.Now()
		}
		d.probe(ctx)
		d.activate(ctx)
		d.prune()
		d.updateHint()
	}
}

func (d *discovery) fetchList(ctx context.Context) bool {
	src := d.cfg.ListURL
	if src == "" {
		src = network.SourceURL
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", src, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil || resp.StatusCode != 200 {
		return false
	}
	l, err := network.Parse(b)
	if err != nil {
		d.n.cfg.Logf("discovery: relay list: %v", err)
		return false
	}
	for _, r := range l.Relays {
		for _, u := range r.URLs {
			d.add(u, r.Name, r.Region, r.PubKey, "list")
		}
	}
	return true
}

// fetchRelayLists asks every reachable relay for its signed list.
func (d *discovery) fetchRelayLists(ctx context.Context) bool {
	got := false
	for _, l := range d.n.allRelayLinks() {
		if !l.isUp() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		list, err := l.c.Relays(cctx)
		cancel()
		if err != nil || !relayclient.VerifyRelayList(*list) {
			continue
		}
		if l.pin != "" && !strings.EqualFold(list.PubKey, l.pin) {
			continue
		}
		got = true
		for _, e := range list.Relays {
			for _, u := range e.URLs {
				// The serving relay vouches for its own key only.
				key := ""
				if strings.EqualFold(e.PubKey, list.PubKey) {
					key = e.PubKey
				}
				d.add(u, e.Name, e.Region, key, "relay")
			}
		}
	}
	return got
}

type candidate struct {
	url, name, region, pubkey, source, state string
	fails                                    int
	lastOK                                   int64
}

func (d *discovery) rows(where string, args ...any) []candidate {
	rows, err := d.n.db.db.Query(`SELECT url,name,region,pubkey,source,state,fails,last_ok FROM relays_known `+where, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.url, &c.name, &c.region, &c.pubkey, &c.source, &c.state, &c.fails, &c.lastOK) == nil {
			out = append(out, c)
		}
	}
	return out
}

// probe reaches a few candidates that are not in use and records what they say.
func (d *discovery) probe(ctx context.Context) {
	cs := d.rows(`WHERE state IN ('new','ok') ORDER BY last_try LIMIT 40`)
	n := 0
	for _, c := range cs {
		if n >= probesPerRound || ctx.Err() != nil {
			return
		}
		d.mu.Lock()
		_, running := d.auto[c.url]
		d.mu.Unlock()
		if running || d.configured(c.url) {
			continue
		}
		n++
		d.check(ctx, c)
	}
}

func (d *discovery) check(ctx context.Context, c candidate) bool {
	db := d.n.db.db
	now := time.Now().Unix()
	cl, err := relayclient.New(c.url, relayclient.Options{Origin: "node-" + d.n.id.Hash().String()[:12]})
	if err != nil {
		db.Exec(`UPDATE relays_known SET state='bad', last_try=? WHERE url=?`, now, c.url)
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	info, err := cl.Info(cctx)
	cancel()
	if err != nil {
		db.Exec(`UPDATE relays_known SET last_try=?, fails=fails+1 WHERE url=?`, now, c.url)
		return false
	}
	if c.pubkey != "" && !strings.EqualFold(info.PubKey, c.pubkey) {
		d.n.cfg.Logf("discovery: %s answers with another key: ignored", c.url)
		db.Exec(`UPDATE relays_known SET state='bad', last_try=? WHERE url=?`, now, c.url)
		return false
	}
	name, region := c.name, c.region
	if name == "" || !trusted(c.source) {
		name, region = info.Name, info.Region
	}
	db.Exec(`UPDATE relays_known SET state='ok', name=?, region=?, pubkey=?, last_ok=?, last_try=?, fails=0 WHERE url=?`,
		name, region, strings.ToLower(info.PubKey), now, now, c.url)
	return true
}

// startKnown restarts the automatic relays that worked before, at once.
func (d *discovery) startKnown(ctx context.Context) {
	for _, c := range d.rows(`WHERE state='ok' AND pubkey<>'' ORDER BY source IN ('bootstrap','list') DESC, last_ok DESC`) {
		if d.activeCount() >= d.maxAuto() {
			return
		}
		d.start(ctx, c)
	}
}

func (d *discovery) activeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.auto)
}

// activate starts verified relays, official ones first, up to MaxAuto.
func (d *discovery) activate(ctx context.Context) {
	for _, c := range d.rows(`WHERE state='ok' AND pubkey<>'' ORDER BY source IN ('bootstrap','list') DESC, last_ok DESC`) {
		if d.activeCount() >= d.maxAuto() || ctx.Err() != nil {
			return
		}
		d.start(ctx, c)
	}
}

func (d *discovery) start(ctx context.Context, c candidate) {
	if d.configured(c.url) {
		return
	}
	// One link per relay key (a relay may have several URLs).
	for _, l := range d.n.allRelayLinks() {
		if l.pin != "" && strings.EqualFold(l.pin, c.pubkey) {
			return
		}
	}
	d.mu.Lock()
	if _, ok := d.auto[c.url]; ok {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	name := c.name
	if name == "" {
		name = "relay"
	}
	d.n.mu.Lock()
	if _, taken := d.n.relays[name]; taken {
		name += "-" + c.pubkey[:6]
	}
	d.n.mu.Unlock()
	l, err := newRelayLink(d.n, RelayConfig{Name: name, URL: c.url})
	if err != nil {
		return
	}
	l.pin, l.auto = strings.ToLower(c.pubkey), c.url
	lctx, cancel := context.WithCancel(ctx)
	a := &autoRelay{link: l, cancel: cancel, done: make(chan struct{})}
	d.mu.Lock()
	d.auto[c.url] = a
	d.mu.Unlock()
	d.n.mu.Lock()
	d.n.relays[name] = l
	d.n.mu.Unlock()
	d.n.router.AddLink(l)
	d.n.cfg.Logf("discovery: using relay %s (%s, from %s)", name, c.url, c.source)
	go func() { defer close(a.done); l.run(lctx) }()
}

// stop ends an automatic relay; state is what the row becomes.
func (d *discovery) stop(u, state string) {
	d.mu.Lock()
	a, ok := d.auto[u]
	delete(d.auto, u)
	d.mu.Unlock()
	if state != "" {
		d.n.db.db.Exec(`UPDATE relays_known SET state=? WHERE url=?`, state, u)
	}
	if !ok {
		return
	}
	a.cancel()
	<-a.done
	name := strings.TrimPrefix(a.link.name, "relay:")
	d.n.router.RemoveLink(a.link.name)
	d.n.mu.Lock()
	if d.n.relays[name] == a.link {
		delete(d.n.relays, name)
	}
	delete(d.n.links, a.link.name)
	d.n.mu.Unlock()
	d.n.emit(Event{Type: "link", Info: a.link.name + ": removed"})
}

func (d *discovery) stopAll() {
	d.mu.Lock()
	urls := make([]string, 0, len(d.auto))
	for u := range d.auto {
		urls = append(urls, u)
	}
	d.mu.Unlock()
	for _, u := range urls {
		d.stop(u, "")
	}
}

// prune drops automatic relays that have been unreachable for a day.
func (d *discovery) prune() {
	d.mu.Lock()
	var dead []string
	for u, a := range d.auto {
		if a.link.downSince(dropAfter) {
			dead = append(dead, u)
		}
	}
	d.mu.Unlock()
	for _, u := range dead {
		d.n.db.db.Exec(`UPDATE relays_known SET fails=fails+1, last_try=? WHERE url=?`, time.Now().Unix(), u)
		d.stop(u, "")
	}
}

// updateHint puts a relay we reach now into our announces: a configured
// one first, else an automatic one.
func (d *discovery) updateHint() {
	hint := ""
	for _, l := range d.n.allRelayLinks() {
		if !l.isUp() {
			continue
		}
		if u, err := network.CheckURL(l.c.URL(), d.cfg.AllowPrivate); err == nil {
			hint = u
			if l.auto == "" {
				break
			}
		}
	}
	if !d.n.router.SetRelayHint(hint) || hint == "" {
		return
	}
	d.mu.Lock()
	due := time.Since(d.lastHint) > reannounceGap
	if due {
		d.lastHint = time.Now()
	}
	d.mu.Unlock()
	if due {
		d.n.router.Announce()
	}
}

// pinMismatch is called by an automatic relay link whose key changed.
func (d *discovery) pinMismatch(l *relayLink) {
	d.n.cfg.Logf("discovery: %s changed its key: dropped", l.auto)
	go d.stop(l.auto, "bad")
}

// allRelayLinks lists configured and automatic relay links, configured first.
func (n *Node) allRelayLinks() []*relayLink {
	n.mu.Lock()
	defer n.mu.Unlock()
	var cfg, auto []*relayLink
	for _, rc := range n.cfg.Relays {
		if l := n.relays[rc.Name]; l != nil {
			cfg = append(cfg, l)
		}
	}
	for _, l := range n.relays {
		if l.auto != "" {
			auto = append(auto, l)
		}
	}
	return append(cfg, auto...)
}

// KnownRelays lists every relay this node has heard of.
func (n *Node) KnownRelays() []KnownRelay {
	if n.disc == nil {
		return nil
	}
	var out []KnownRelay
	for _, c := range n.disc.rows(`ORDER BY source IN ('bootstrap','list') DESC, state='ok' DESC, last_ok DESC`) {
		k := KnownRelay{URL: c.url, Name: c.name, Region: c.region, PubKey: c.pubkey, Source: c.source, State: c.state, Config: n.disc.configured(c.url)}
		if c.lastOK > 0 {
			k.LastOK = time.Unix(c.lastOK, 0)
		}
		n.disc.mu.Lock()
		_, k.Active = n.disc.auto[c.url]
		n.disc.mu.Unlock()
		out = append(out, k)
	}
	return out
}

// ForgetRelay stops using a discovered relay (URL or name) and never picks
// it up again.
func (n *Node) ForgetRelay(ref string) error {
	if n.disc == nil {
		return errNoDiscovery
	}
	for _, k := range n.KnownRelays() {
		if k.URL == ref || strings.EqualFold(k.Name, ref) {
			n.disc.stop(k.URL, "forgotten")
			return nil
		}
	}
	return sql.ErrNoRows
}

var errNoDiscovery = errorString("relay discovery is off")

type errorString string

func (e errorString) Error() string { return string(e) }

var _ router.Link = (*relayLink)(nil)

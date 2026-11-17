package node

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/network"
)

// The official relay list (network/relays.json) is kept on disk, so a
// restart uses it at once instead of waiting for GitHub, and it survives an
// Internet blackout. Where it comes from, newest first:
//
//   - GitHub (refreshed in the background every listEvery; the time of the
//     last refresh is kept too, so restarts don't refetch);
//   - nearby devices: a signed list travels as a bulletin (router.RelayListDest)
//     over Bluetooth, LAN and direct links, store-and-forward like messages,
//     so a phone that never reached GitHub learns it from one that did;
//   - the copy compiled into the build.
//
// Lists from nearby devices must carry the project key's signature
// (network.Signed) and a higher serial than the one held.

const (
	kvListJSON    = "relaylist.json"
	kvListSig     = "relaylist.sig"
	kvListFetched = "relaylist.fetched"
	republishList = 12 * time.Hour
)

// curList is the official list this node holds.
type curList struct {
	signed network.Signed
	serial int64
}

func (d *discovery) listOn() bool { return d.cfg.ListURL != "none" }

// loadList takes the cached list if it is newer than the built-in one.
func (d *discovery) loadList() {
	var cur curList
	if !d.cfg.NoBuiltin {
		bl := network.Bootstrap()
		d.addList(bl, "bootstrap")
		cur = curList{signed: network.BootstrapSigned(), serial: bl.Serial}
	}
	if j, ok := d.n.db.get(kvListJSON); ok {
		if l, err := network.Parse([]byte(j)); err == nil && l.Serial > cur.serial {
			s := network.Signed{JSON: []byte(j)}
			if h, ok := d.n.db.get(kvListSig); ok {
				if sig, err := hex.DecodeString(h); err == nil {
					s.Sig = sig
					if !s.Verify(d.cfg.ListKey) {
						s.Sig = nil
					}
				}
			}
			cur = curList{signed: s, serial: l.Serial}
			d.addList(l, "list")
		}
	}
	if v, ok := d.n.db.get(kvListFetched); ok {
		if t, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.lastList.Store(t)
		}
	}
	d.listMu.Lock()
	d.list = cur
	d.listMu.Unlock()
}

func (d *discovery) addList(l *network.List, source string) {
	for _, r := range l.Relays {
		for _, u := range r.URLs {
			d.add(u, r.Name, r.Region, r.PubKey, source)
		}
	}
}

// take makes l the held list if it is newer. GitHub is authoritative and
// may replace a list of the same serial; others need a higher one.
func (d *discovery) take(s network.Signed, l *network.List, fromGitHub bool) bool {
	d.listMu.Lock()
	if l.Serial < d.list.serial || (!fromGitHub && l.Serial == d.list.serial) ||
		(l.Serial == d.list.serial && s.Hash() == d.list.signed.Hash()) {
		d.listMu.Unlock()
		return false
	}
	d.list = curList{signed: s, serial: l.Serial}
	d.listMu.Unlock()
	return true
}

// save persists the held list and applies its relays.
func (d *discovery) save(s network.Signed, l *network.List) {
	d.n.db.set(kvListJSON, string(s.JSON))
	if s.Sig != nil {
		d.n.db.set(kvListSig, hex.EncodeToString(s.Sig))
	} else {
		d.n.db.del(kvListSig)
	}
	d.addList(l, "list")
	d.n.cfg.Logf("discovery: relay list serial %d (%d relays)", l.Serial, len(l.Relays))
	d.poke()
}

// onBulletin checks a relay list a neighbour passed on. It runs under the
// router lock, so the slow part happens in a goroutine.
func (d *discovery) onBulletin(body []byte) bool {
	if !d.listOn() {
		return false
	}
	s, l, err := network.DecodeSigned(body, d.cfg.ListKey)
	if err != nil || !d.take(s, l, false) {
		return false
	}
	go d.save(s, l)
	return true
}

// publishList offers the held list to nearby devices, if it is signed.
func (d *discovery) publishList() {
	d.listMu.Lock()
	s := d.list.signed
	d.listMu.Unlock()
	if s.Sig == nil || !s.Verify(d.cfg.ListKey) {
		return
	}
	body := s.Encode()
	if len(body)+64 > 60<<10 {
		return
	}
	d.n.router.PublishRelayList(router.RelayListPacket(body, 0))
	d.lastPublish.Store(time.Now().Unix())
}

// refreshList fetches the list from GitHub in the background (one at a time).
func (d *discovery) refreshList(ctx context.Context) {
	if !d.fetching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer d.fetching.Store(false)
		if d.fetchList(ctx) {
			now := time.Now().Unix()
			d.lastList.Store(now)
			d.n.db.set(kvListFetched, strconv.FormatInt(now, 10))
		}
	}()
}

func (d *discovery) fetchList(ctx context.Context) bool {
	src := d.cfg.ListURL
	if src == "" {
		src = network.SourceURL
	}
	b, err := httpGet(ctx, src, 256<<10)
	if err != nil {
		return false
	}
	l, err := network.Parse(b)
	if err != nil {
		d.n.cfg.Logf("discovery: relay list: %v", err)
		return false
	}
	s := network.Signed{JSON: b}
	if h, err := httpGet(ctx, src+network.SigSuffix, 1<<10); err == nil {
		if sig, err := hex.DecodeString(string(trimSpace(h))); err == nil {
			s.Sig = sig
			if !s.Verify(d.cfg.ListKey) {
				s.Sig = nil
			}
		}
	}
	if d.take(s, l, true) {
		d.save(s, l)
		d.publishList()
	} else {
		d.addList(l, "list") // same list: refresh names and keys
	}
	return true
}

func httpGet(ctx context.Context, u string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errorString("relay list: " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

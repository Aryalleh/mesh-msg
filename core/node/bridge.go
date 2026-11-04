package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/relayclient"
)

// bridge syncs two relays that cannot reach each other (e.g. a regional
// relay over the domestic network and a global relay through a VPN or
// Starlink), within a daily data budget.
type bridge struct {
	n      *Node
	cfg    BridgeConfig
	a, b   relayclient.RemoteSide
	curPth string

	mu       sync.Mutex
	cur      relayclient.Cursors
	budget   *relayclient.Budget
	day      int
	moved    [2]int
	lastErr  string
	lastSync time.Time
}

func newBridge(n *Node, bc BridgeConfig) (*bridge, error) {
	find := func(name string) (RelayConfig, error) {
		for _, r := range n.cfg.Relays {
			if r.Name == name {
				return r, nil
			}
		}
		return RelayConfig{}, fmt.Errorf("unknown relay %q", name)
	}
	ra, err := find(bc.A)
	if err != nil {
		return nil, err
	}
	rb, err := find(bc.B)
	if err != nil {
		return nil, err
	}
	// The same label on both relays: each excludes what the bridge pushed.
	label := "bridge-" + n.id.Hash().String()[:12]
	ca, err := relayclient.New(ra.URL, relayclient.Options{Via: ra.Via, Origin: label, Token: ra.Token})
	if err != nil {
		return nil, err
	}
	cb, err := relayclient.New(rb.URL, relayclient.Options{Via: rb.Via, Origin: label, Token: rb.Token})
	if err != nil {
		return nil, err
	}
	if bc.Name == "" {
		bc.Name = bc.A + "-" + bc.B
	}
	if bc.EverySecs <= 0 {
		bc.EverySecs = 15
	}
	br := &bridge{
		n: n, cfg: bc,
		a:      relayclient.RemoteSide{C: ca, Label: ra.Name},
		b:      relayclient.RemoteSide{C: cb, Label: rb.Name},
		curPth: filepath.Join(n.cfg.DataDir, "bridge-"+bc.Name+".json"),
	}
	if b, err := os.ReadFile(br.curPth); err == nil {
		json.Unmarshal(b, &br.cur)
	}
	return br, nil
}

func (b *bridge) refreshBudget(now time.Time) {
	if b.cfg.BudgetMBPerDay <= 0 {
		b.budget = nil
		return
	}
	if d := now.YearDay(); b.budget == nil || d != b.day {
		b.day = d
		b.budget = &relayclient.Budget{Remaining: int64(b.cfg.BudgetMBPerDay) << 20}
	}
}

func (b *bridge) run(ctx context.Context) {
	name := "bridge:" + b.cfg.Name
	b.n.setLinkStatus(name, "starting")
	backoff := time.Duration(b.cfg.EverySecs) * time.Second
	for {
		b.mu.Lock()
		b.refreshBudget(time.Now())
		cur, budget := b.cur, b.budget
		b.mu.Unlock()

		ab, ba, err := relayclient.SyncOnce(ctx, b.a, b.b, &cur, budget)

		b.mu.Lock()
		b.cur = cur
		b.moved[0] += ab
		b.moved[1] += ba
		if err != nil {
			b.lastErr = err.Error()
		} else {
			b.lastErr = ""
			b.lastSync = time.Now()
		}
		data, _ := json.Marshal(b.cur)
		b.mu.Unlock()
		os.WriteFile(b.curPth, data, 0o600)

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.n.setLinkStatus(name, "error: "+err.Error())
			backoff = min(backoff*2, 10*time.Minute)
		} else {
			b.n.setLinkStatus(name, "syncing")
			backoff = time.Duration(b.cfg.EverySecs) * time.Second
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

func (b *bridge) status() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := fmt.Sprintf("%s: %s→%s %d, %s→%s %d", b.cfg.Name, b.cfg.A, b.cfg.B, b.moved[0], b.cfg.B, b.cfg.A, b.moved[1])
	if b.budget != nil {
		s += fmt.Sprintf(", budget left %d KB", max(b.budget.Remaining, 0)/1024)
	}
	if b.lastErr != "" {
		s += ", last error: " + b.lastErr
	}
	return s
}

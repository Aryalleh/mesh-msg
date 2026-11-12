package node

import (
	"context"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/core/store"
)

// relayLink is an Uplink to one relay: it uploads everything the router
// forwards to it, long-polls our own mailbox, and — in gateway mode —
// fetches packets for the local mesh.
type relayLink struct {
	n    *Node
	name string
	c    *relayclient.Client

	mu    sync.Mutex
	queue [][]byte
	kick  chan struct{}
	up    bool
}

const maxQueue = 20000

func newRelayLink(n *Node, rc RelayConfig) (*relayLink, error) {
	c, err := relayclient.New(rc.URL, relayclient.Options{Via: rc.Via, Origin: "node-" + n.id.Hash().String()[:12]})
	if err != nil {
		return nil, err
	}
	return &relayLink{n: n, name: "relay:" + rc.Name, c: c, kick: make(chan struct{}, 1)}, nil
}

func (l *relayLink) Name() string                { return l.name }
func (l *relayLink) Kind() router.LinkKind       { return router.Uplink }
func (l *relayLink) MTU() int                    { return packet.MaxSize }
func (l *relayLink) Client() *relayclient.Client { return l.c }

func (l *relayLink) Send(_ router.PeerID, raw []byte) {
	l.mu.Lock()
	if len(l.queue) < maxQueue {
		l.queue = append(l.queue, raw)
	}
	l.mu.Unlock()
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

func (l *relayLink) setUp(up bool) {
	l.mu.Lock()
	was := l.up
	l.up = up
	l.mu.Unlock()
	if up && !was {
		l.n.setLinkStatus(l.name, "connected")
		// Carry the store backlog (our announce first, then held packets).
		l.n.router.LinkUp(l.name)
	} else if !up && was {
		l.n.setLinkStatus(l.name, "unreachable")
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (l *relayLink) run(ctx context.Context) {
	l.n.setLinkStatus(l.name, "connecting")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); l.uploader(ctx) }()
	go func() { defer wg.Done(); l.downloader(ctx) }()
	wg.Wait()
}

func (l *relayLink) uploader(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.kick:
		case <-time.After(30 * time.Second):
		}
		if _, err := l.c.Info(ctx); err != nil {
			l.setUp(false)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 5*time.Minute)
			l.poke()
			continue
		}
		l.setUp(true)
		for {
			l.mu.Lock()
			batch := l.queue[:min(len(l.queue), 200)]
			l.queue = l.queue[len(batch):]
			l.mu.Unlock()
			if len(batch) == 0 {
				break
			}
			if _, err := l.c.Upload(ctx, batch); err != nil {
				l.n.cfg.Logf("%s: upload: %v", l.name, err)
				l.mu.Lock()
				l.queue = append(batch, l.queue...) // retry later
				l.mu.Unlock()
				l.setUp(false)
				if !sleepCtx(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, 5*time.Minute)
				l.poke()
				break
			}
			backoff = 2 * time.Second
		}
	}
}

func (l *relayLink) poke() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// downloader runs independent loops: our mailbox (long-poll), receipts for
// what we sent, and (if enabled) gateway fetches for the local mesh.
func (l *relayLink) downloader(ctx context.Context) {
	var wg sync.WaitGroup
	loop := func(what string, every time.Duration, once func(context.Context) error) {
		defer wg.Done()
		backoff := 2 * time.Second
		for ctx.Err() == nil {
			if err := once(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				// A 401 before our announce reached the relay is expected.
				l.n.cfg.Logf("%s: %s: %v", l.name, what, err)
				if !sleepCtx(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, 5*time.Minute)
				continue
			}
			backoff = 2 * time.Second
			if every > 0 && !sleepCtx(ctx, every) {
				return
			}
		}
	}
	wg.Add(3)
	go loop("mailbox", 0, l.mailboxOnce)
	go loop("groups", 10*time.Second, l.groupsOnce)
	go loop("receipts", 5*time.Second, l.receiptsOnce)
	if gw := l.n.cfg.Gateway; gw.Enabled {
		wg.Add(1)
		go loop("gateway", 0, func(ctx context.Context) error { return l.gatewayOnce(ctx, gw) })
	}
	wg.Wait()
}

func (l *relayLink) mailboxOnce(ctx context.Context) error {
	key := l.name + ":mailbox"
	raws, cur, err := l.c.Mailbox(ctx, l.n.id, l.n.cursor(key), 25*time.Second)
	if err != nil {
		return err
	}
	l.setUp(true)
	l.deliver(raws)
	l.n.setCursor(key, cur)
	return nil
}

// groupsOnce fetches the mailbox of every joined group (members fetch as the
// group's shared identity). No long poll: one quick fetch per group.
func (l *relayLink) groupsOnce(ctx context.Context) error {
	gs, err := l.n.Groups()
	if err != nil {
		return err
	}
	for _, g := range gs {
		if g.g == nil {
			continue // private groups travel as one-to-one messages
		}
		key := l.name + ":group:" + g.Hash
		raws, cur, err := l.c.Mailbox(ctx, g.g.Identity(), l.n.cursor(key), 0)
		if err != nil {
			// A 401 until the relay has seen the group's announce is expected;
			// keep polling the other groups at the normal pace.
			if ctx.Err() == nil {
				l.n.cfg.Logf("%s: group %s: %v", l.name, g.Title, err)
			}
			continue
		}
		l.deliver(raws)
		l.n.setCursor(key, cur)
	}
	return nil
}

// receiptsOnce asks the relay for receipts of our undelivered packets (our
// own DATA stays in the store until its receipt purges it).
func (l *relayLink) receiptsOnce(ctx context.Context) error {
	items, err := l.n.store.Since(0, 0, store.Filter{Types: []packet.Type{packet.TypeData}})
	if err != nil {
		return err
	}
	seen := map[packet.ID]bool{}
	var ids []packet.ID
	for _, it := range items {
		if it.Origin == "local" && !seen[it.Key.ID] {
			seen[it.Key.ID] = true
			ids = append(ids, it.Key.ID)
		}
	}
	raws, err := l.c.Receipts(ctx, ids)
	if err != nil {
		return err
	}
	l.deliver(raws)
	return nil
}

func (l *relayLink) gatewayOnce(ctx context.Context, gw GatewayConfig) error {
	// Serve destinations learned from local (non-relay) links.
	var serve []packet.Hash
	for _, rt := range l.n.router.Routes() {
		if len(rt.Link) < 6 || rt.Link[:6] != "relay:" {
			serve = append(serve, rt.Dest)
		}
	}
	key := l.name + ":gateway"
	quota := 0
	if gw.Mode == "carrier" {
		quota = max(gw.QuotaKBPerHour, 0) * 1024 / 144 // per ~25 s poll
	}
	raws, cur, err := l.c.Gateway(ctx, relayclient.GatewayRequest{
		Serve: serve, Carrier: gw.Mode == "carrier", QuotaBytes: quota,
		After: l.n.cursor(key), Wait: 25 * time.Second,
	})
	if err != nil {
		return err
	}
	l.deliver(raws)
	l.n.setCursor(key, cur)
	return nil
}

func (l *relayLink) deliver(raws [][]byte) {
	for _, raw := range raws {
		l.n.router.Inbound(l.name, "", raw)
	}
}

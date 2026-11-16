package lite

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
)

// ---- relay (HTTPS) ----

// receiptPoll is how often relays are asked for receipts of our messages.
var receiptPoll = 15 * time.Second

// Relay sends through a relay and long-polls our mailbox there. In the
// browser net/http is the Fetch API, so the relay must allow cross-origin
// requests (server/api does).
type Relay struct {
	c   *Client
	rc  *relayclient.Client
	url string
	q   chan [][]byte
}

// NewRelay connects c to one relay; Run must be started.
func NewRelay(c *Client, url string) (*Relay, error) {
	// A plain client: in WebAssembly its transport is the browser's fetch.
	rc, err := relayclient.New(url, relayclient.Options{Origin: "web", Timeout: 40 * time.Second, HTTPClient: &http.Client{}})
	if err != nil {
		return nil, err
	}
	return &Relay{c: c, rc: rc, url: url, q: make(chan [][]byte, 64)}, nil
}

func (r *Relay) Name() string { return "relay " + r.url }

// Send queues packets for upload (never blocks the UI).
func (r *Relay) Send(raws [][]byte) {
	select {
	case r.q <- raws:
	default:
	}
}

// Run uploads and fetches until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raws := <-r.q:
				for try := 0; try < 5; try++ {
					if _, err := r.rc.Upload(ctx, raws); err == nil {
						break
					}
					sleep(ctx, time.Duration(2<<try)*time.Second)
				}
			}
		}
	}()
	go func() { // receipts of what we sent are asked for by packet ID
		for ctx.Err() == nil {
			if ids := r.c.Pending(100); len(ids) > 0 {
				if raws, err := r.rc.Receipts(ctx, ids); err == nil {
					for _, raw := range raws {
						r.c.Ingest(raw)
					}
				}
			}
			sleep(ctx, receiptPoll)
		}
	}()
	backoff := time.Second
	for ctx.Err() == nil {
		raws, cur, err := r.rc.Mailbox(ctx, r.c.Identity(), r.c.Cursor(r.url), 25*time.Second)
		if err != nil {
			r.c.Emit(Event{Kind: "link", Detail: r.url + ": " + err.Error()})
			sleep(ctx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, raw := range raws {
			r.c.Ingest(raw)
		}
		r.c.SetCursor(r.url, cur)
		r.c.Emit(Event{Kind: "link", Detail: r.url + ": online"})
	}
}

// ---- Mava board (host link over HTTP) ----

// Board is the browser attached to a Mava node or handheld as a host (port
// 2): POST /api/link carries host-link bytes both ways, base64 in the body,
// so LoRa, the board's Bluetooth peers and its own relay link all reach the
// browser. Polled about once a second, at once when there is something to send.
type Board struct {
	c       *Client
	url     string // e.g. "" (same origin) or "http://192.168.4.1"
	session string
	http    *http.Client

	mu   sync.Mutex
	out  []byte
	kick chan struct{}
	dec  hostlink.Decoder
}

// NewBoard links c to the board serving url; Run must be started.
func NewBoard(c *Client, url, session string) *Board {
	return &Board{c: c, url: url, session: session, http: &http.Client{Timeout: 15 * time.Second}, kick: make(chan struct{}, 1)}
}

func (b *Board) Name() string { return "board" }

func (b *Board) Send(raws [][]byte) {
	b.mu.Lock()
	for _, raw := range raws {
		if f, err := hostlink.Encode(hostlink.KindPacket, raw); err == nil {
			b.out = append(b.out, f...)
		}
	}
	b.mu.Unlock()
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

// Exchange does one round trip: our pending bytes out, the board's in.
func (b *Board) Exchange(ctx context.Context) error {
	b.mu.Lock()
	out := b.out
	b.out = nil
	b.mu.Unlock()
	body := base64.StdEncoding.EncodeToString(out)
	req, err := http.NewRequestWithContext(ctx, "POST", b.url+"/api/link?s="+b.session, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := b.http.Do(req)
	if err != nil {
		b.requeue(out)
		return err
	}
	defer resp.Body.Close()
	in, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		b.requeue(out)
		if err == nil {
			err = &httpError{resp.StatusCode}
		}
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(in)))
	if err != nil {
		return err
	}
	for _, f := range b.dec.Feed(raw) {
		if f.Kind == hostlink.KindPacket {
			b.c.Ingest(f.Payload)
		}
	}
	return nil
}

func (b *Board) requeue(out []byte) {
	b.mu.Lock()
	b.out = append(out, b.out...)
	b.mu.Unlock()
}

// Run polls until ctx ends. The first exchange sends HELLO with the time,
// so the board (which has no clock) can announce itself.
func (b *Board) Run(ctx context.Context) {
	hello, _ := hostlink.Encode(hostlink.KindCommand, hostlink.Command(hostlink.OpHello, 1, unixBE(time.Now())))
	b.mu.Lock()
	b.out = append(hello, b.out...)
	b.mu.Unlock()
	up := false
	for ctx.Err() == nil {
		err := b.Exchange(ctx)
		if (err == nil) != up {
			up = err == nil
			d := "board: online"
			if !up {
				d = "board: " + err.Error()
			}
			b.c.Emit(Event{Kind: "link", Detail: d})
		}
		wait := time.Second
		if !up {
			wait = 5 * time.Second
		}
		select {
		case <-ctx.Done():
		case <-b.kick:
		case <-time.After(wait):
		}
	}
}

type httpError struct{ code int }

func (e *httpError) Error() string { return "HTTP " + http.StatusText(e.code) }

func unixBE(t time.Time) []byte {
	u := uint32(t.Unix())
	return []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// Package relayclient talks to relay servers: plain client uploads and
// mailbox fetches, gateway sessions, and federation/bridge sync. Each client
// can use its own network path (direct, SOCKS5/HTTP proxy such as a VPN
// client, or Tor), which is what lets a bridge reach one relay over the
// domestic network and another through a way out.
package relayclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/pow"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Options configure a Client.
type Options struct {
	// Via selects the network path: "" or "direct", "tor" (SOCKS5 at
	// 127.0.0.1:9050), or a proxy URL like socks5://127.0.0.1:1080.
	Via string
	// Origin labels this client to the relay (gateway/bridge/relay name).
	Origin string
	// Region is sent as informational X-Region.
	Region string
	// Token authorises federation/bridge sync (else proof-of-work is used).
	Token   string
	Timeout time.Duration // per request, excluding long-poll wait (default 30s)
	// HTTPClient overrides the transport (tests).
	HTTPClient *http.Client
}

// Client is one relay endpoint.
type Client struct {
	base *url.URL
	opt  Options
	http *http.Client

	mu   sync.Mutex
	info *wire.Info
}

// New creates a client for baseURL (e.g. https://relay.example).
func New(baseURL string, opt Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, err
	}
	if opt.Timeout == 0 {
		opt.Timeout = 30 * time.Second
	}
	hc := opt.HTTPClient
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		switch via := strings.TrimSpace(opt.Via); via {
		case "", "direct":
			tr.Proxy = nil
		case "tor":
			pu, _ := url.Parse("socks5://127.0.0.1:9050")
			tr.Proxy = http.ProxyURL(pu)
		default:
			pu, err := url.Parse(via)
			if err != nil {
				return nil, fmt.Errorf("relayclient: bad via %q: %w", via, err)
			}
			tr.Proxy = http.ProxyURL(pu)
		}
		hc = &http.Client{Transport: tr}
	}
	return &Client{base: u, opt: opt, http: hc}, nil
}

// URL returns the base URL.
func (c *Client) URL() string { return c.base.String() }

// StatusError is a non-2xx response.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("relay: HTTP %d: %s", e.Code, e.Msg) }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, hdr map[string]string, wait time.Duration) (*http.Response, []byte, error) {
	u := *c.base
	u.Path += path
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, c.opt.Timeout+wait)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, nil, err
	}
	if c.opt.Origin != "" {
		req.Header.Set(wire.HeaderOrigin, c.opt.Origin)
	}
	if c.opt.Region != "" {
		req.Header.Set(wire.HeaderRegion, c.opt.Region)
	}
	if body != nil {
		req.Header.Set("Content-Type", wire.ContentType)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode/100 != 2 {
		return resp, nil, &StatusError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	return resp, b, nil
}

// Info fetches (and caches) the relay's info.
func (c *Client) Info(ctx context.Context) (*wire.Info, error) {
	c.mu.Lock()
	if c.info != nil {
		defer c.mu.Unlock()
		return c.info, nil
	}
	c.mu.Unlock()
	_, b, err := c.do(ctx, "GET", "/v1/info", nil, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	var in wire.Info
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.info = &in
	c.mu.Unlock()
	return &in, nil
}

func (c *Client) powHeader(ctx context.Context, body []byte) (map[string]string, error) {
	in, err := c.Info(ctx)
	if err != nil {
		return nil, err
	}
	if in.PowBits <= 0 || len(body) == 0 {
		return nil, nil
	}
	return map[string]string{wire.HeaderPoW: strconv.FormatUint(pow.Solve(body, in.PowBits), 10)}, nil
}

func (c *Client) syncHeader(ctx context.Context, body []byte) (map[string]string, error) {
	if c.opt.Token != "" {
		return map[string]string{"Authorization": "Bearer " + c.opt.Token}, nil
	}
	return c.powHeader(ctx, body)
}

// Upload sends packets to the relay.
func (c *Client) Upload(ctx context.Context, raws [][]byte) (int, error) {
	if len(raws) == 0 {
		return 0, nil
	}
	body := packet.EncodeFrames(raws)
	hdr, err := c.powHeader(ctx, body)
	if err != nil {
		return 0, err
	}
	_, b, err := c.do(ctx, "POST", "/v1/packets", nil, body, hdr, 0)
	if err != nil {
		return 0, err
	}
	var a wire.Accepted
	return a.Accepted, json.Unmarshal(b, &a)
}

func cursorOf(resp *http.Response, def int64) int64 {
	n, err := strconv.ParseInt(resp.Header.Get(wire.HeaderCursor), 10, 64)
	if err != nil {
		return def
	}
	return n
}

// Mailbox fetches packets addressed to id (signed challenge), long-polling
// for up to wait.
func (c *Client) Mailbox(ctx context.Context, id *identity.Identity, after int64, wait time.Duration) ([][]byte, int64, error) {
	_, nonce, err := c.do(ctx, "GET", "/v1/challenge", nil, nil, nil, 0)
	if err != nil {
		return nil, after, err
	}
	hdr := map[string]string{
		wire.HeaderNonce:     string(nonce),
		wire.HeaderSignature: hex.EncodeToString(announce.SignFetch(id, nonce)),
	}
	q := url.Values{"after": {strconv.FormatInt(after, 10)}, "wait": {strconv.Itoa(int(wait.Seconds()))}}
	resp, b, err := c.do(ctx, "GET", "/v1/mailbox/"+id.Hash().String(), q, nil, hdr, wait)
	if err != nil {
		return nil, after, err
	}
	raws, err := packet.DecodeFrames(b)
	return raws, cursorOf(resp, after), err
}

// GatewayRequest is one gateway exchange.
type GatewayRequest struct {
	Serve      []packet.Hash
	Upload     [][]byte
	Carrier    bool
	QuotaBytes int
	After      int64
	Wait       time.Duration
}

// Gateway uploads the mesh's packets and receives packets for it.
func (c *Client) Gateway(ctx context.Context, req GatewayRequest) ([][]byte, int64, error) {
	body := wire.EncodeGateway(req.Serve, req.Upload)
	var hdr map[string]string
	if len(req.Upload) > 0 {
		var err error
		if hdr, err = c.powHeader(ctx, body); err != nil {
			return nil, req.After, err
		}
	}
	q := url.Values{
		"after": {strconv.FormatInt(req.After, 10)},
		"wait":  {strconv.Itoa(int(req.Wait.Seconds()))},
		"quota": {strconv.Itoa(req.QuotaBytes)},
	}
	if req.Carrier {
		q.Set("carrier", "1")
	}
	resp, b, err := c.do(ctx, "POST", "/v1/gateway", q, body, hdr, req.Wait)
	if err != nil {
		return nil, req.After, err
	}
	raws, err := packet.DecodeFrames(b)
	return raws, cursorOf(resp, req.After), err
}

// SyncPush sends federation/bridge items to the relay.
func (c *Client) SyncPush(ctx context.Context, items []wire.SyncItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	body := wire.EncodeSync(items)
	hdr, err := c.syncHeader(ctx, body)
	if err != nil {
		return 0, err
	}
	_, b, err := c.do(ctx, "POST", "/v1/sync/push", nil, body, hdr, 0)
	if err != nil {
		return 0, err
	}
	var a wire.Accepted
	return a.Accepted, json.Unmarshal(b, &a)
}

// SyncPull fetches items after cursor (excluding those this origin pushed).
func (c *Client) SyncPull(ctx context.Context, after int64, limit int, wait time.Duration) ([]wire.SyncItem, int64, bool, error) {
	hdr, err := c.syncHeader(ctx, nil)
	if err != nil {
		return nil, after, false, err
	}
	q := url.Values{
		"after": {strconv.FormatInt(after, 10)},
		"limit": {strconv.Itoa(limit)},
		"wait":  {strconv.Itoa(int(wait.Seconds()))},
	}
	resp, b, err := c.do(ctx, "GET", "/v1/sync/pull", q, nil, hdr, wait)
	if err != nil {
		return nil, after, false, err
	}
	items, err := wire.DecodeSync(b)
	return items, cursorOf(resp, after), resp.Header.Get(wire.HeaderMore) == "1", err
}

// Relays fetches the relay's signed relay list.
func (c *Client) Relays(ctx context.Context) (*wire.RelayList, error) {
	_, b, err := c.do(ctx, "GET", "/v1/relays", nil, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	var l wire.RelayList
	return &l, json.Unmarshal(b, &l)
}

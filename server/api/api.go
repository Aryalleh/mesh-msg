// Package api exposes a relay over HTTP (docs/server.md §6). Bodies are
// binary frames (core/wire), not JSON, to stay small on throttled links.
// Long-poll (wait=seconds) gives near-live delivery through any HTTP proxy
// or CDN, where WebSockets are often blocked.
package api

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/pow"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

const (
	Version     = "0.1.0"
	maxBody     = 4 << 20
	maxWait     = 30 * time.Second
	defaultPage = 500
)

// Config configures the HTTP layer.
type Config struct {
	Relay    *relay.Relay
	Identity *identity.Identity // relay's own key (signs relay lists)
	PowBits  int                // required on anonymous uploads
	// SyncTokens authorises peer relays and bridges for /v1/sync/*. Empty
	// means sync requires proof-of-work like any upload.
	SyncTokens []string
	Relays     []wire.RelayEntry // published relay list
	RateLimit  float64           // requests/second per IP (0 = 20)
	Media      MediaConfig       // encrypted files (off unless MaxMB > 0)
	Logf       func(format string, args ...any)
}

// Server is an http.Handler.
type Server struct {
	cfg Config
	mux *http.ServeMux
	lim *limiter

	media *mediaStore
}

// New builds the handler.
func New(cfg Config) *Server {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.RateLimit == 0 {
		cfg.RateLimit = 20
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux(), lim: newLimiter(cfg.RateLimit, 2*cfg.RateLimit)}
	s.mux.HandleFunc("GET /v1/info", s.info)
	s.mux.HandleFunc("GET /v1/relays", s.relays)
	s.mux.HandleFunc("POST /v1/packets", s.packets)
	s.mux.HandleFunc("POST /v1/receipts/query", s.receiptsQuery)
	s.mux.HandleFunc("GET /v1/challenge", s.challenge)
	s.mux.HandleFunc("GET /v1/mailbox/{dest}", s.mailbox)
	s.mux.HandleFunc("POST /v1/gateway", s.gateway)
	s.mux.HandleFunc("POST /v1/sync/push", s.syncPush)
	s.mux.HandleFunc("GET /v1/sync/pull", s.syncPull)
	if cfg.Media.MaxMB > 0 && cfg.Media.Dir != "" {
		if err := s.openMedia(); err != nil {
			cfg.Logf("media: %v (media off)", err)
		} else {
			s.mux.HandleFunc("PUT /v1/media/{id}", s.mediaPut)
			s.mux.HandleFunc("GET /v1/media/{id}", s.mediaGet)
		}
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.lim.allow(clientIP(r)) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, wire.Info{
		Name: s.cfg.Relay.Name(), Region: s.cfg.Relay.Region(), PowBits: s.cfg.PowBits,
		Version: Version, PubKey: hex.EncodeToString(s.cfg.Identity.Bytes()),
		MediaMaxMB: s.mediaMaxMB(), MediaMaxHours: s.cfg.Media.maxHours(),
		MediaAccountsOnly: s.media != nil && len(s.cfg.Media.Accounts) > 0,
	})
}

// RelayListPayload and VerifyRelayList: see relayclient (clients verify lists).
var (
	RelayListPayload = relayclient.RelayListPayload
	VerifyRelayList  = relayclient.VerifyRelayList
)

func (s *Server) relays(w http.ResponseWriter, r *http.Request) {
	issued := time.Now().Unix()
	// Our own entry carries our key, so clients can pin us.
	list := append([]wire.RelayEntry(nil), s.cfg.Relays...)
	for i := range list {
		if list[i].Name == s.cfg.Relay.Name() && list[i].PubKey == "" {
			list[i].PubKey = hex.EncodeToString(s.cfg.Identity.Bytes())
		}
	}
	sig := s.cfg.Identity.SignDomain(relayclient.DomainRelayList, RelayListPayload(list, issued))
	writeJSON(w, wire.RelayList{
		Relays: list, Issued: issued,
		PubKey: hex.EncodeToString(s.cfg.Identity.Bytes()), Signature: hex.EncodeToString(sig),
	})
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return b, true
}

func (s *Server) checkPoW(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if s.cfg.PowBits <= 0 || len(body) == 0 {
		return true
	}
	n, err := strconv.ParseUint(r.Header.Get(wire.HeaderPoW), 10, 64)
	if err != nil || !pow.Check(body, n, s.cfg.PowBits) {
		http.Error(w, "proof of work required", http.StatusPaymentRequired)
		return false
	}
	return true
}

func origin(r *http.Request, prefix string) string {
	o := r.Header.Get(wire.HeaderOrigin)
	if o == "" {
		return prefix + "anon"
	}
	if len(o) > 64 {
		o = o[:64]
	}
	return prefix + o
}

func (s *Server) packets(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok || !s.checkPoW(w, r, body) {
		return
	}
	raws, err := packet.DecodeFrames(body)
	if err != nil {
		http.Error(w, "bad frames", http.StatusBadRequest)
		return
	}
	n := s.cfg.Relay.Ingest(raws, origin(r, "client:"), s.cfg.Relay.Region())
	writeJSON(w, wire.Accepted{Accepted: n})
}

// receiptsQuery: body = concatenated 8-byte packet IDs (max 1000).
func (s *Server) receiptsQuery(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	if len(body)%8 != 0 || len(body)/8 > 1000 {
		http.Error(w, "body must be up to 1000 packet IDs", http.StatusBadRequest)
		return
	}
	ids := make([]packet.ID, len(body)/8)
	for i := range ids {
		copy(ids[i][:], body[i*8:])
	}
	writeFrames(w, packet.EncodeFrames(s.cfg.Relay.Receipts(ids)), 0, false)
}

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, s.cfg.Relay.NewChallenge())
}

func queryInt(r *http.Request, name string, def int64) int64 {
	v, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	if err != nil {
		return def
	}
	return v
}

// waitFor long-polls: it calls fetch until it returns something or the
// requested wait elapses.
func waitFor(r *http.Request, rl *relay.Relay, fetch func() bool) {
	wait := min(time.Duration(queryInt(r, "wait", 0))*time.Second, maxWait)
	deadline := time.Now().Add(wait)
	for {
		ch := rl.Changed()
		if fetch() || time.Now().After(deadline) {
			return
		}
		select {
		case <-ch:
		case <-time.After(time.Until(deadline)):
		case <-r.Context().Done():
			return
		}
	}
}

func writeFrames(w http.ResponseWriter, body []byte, cursor int64, more bool) {
	w.Header().Set("Content-Type", wire.ContentType)
	w.Header().Set(wire.HeaderCursor, strconv.FormatInt(cursor, 10))
	if more {
		w.Header().Set(wire.HeaderMore, "1")
	}
	w.Write(body)
}

func (s *Server) mailbox(w http.ResponseWriter, r *http.Request) {
	dest, err := packet.ParseHash(r.PathValue("dest"))
	if err != nil {
		http.Error(w, "bad destination", http.StatusBadRequest)
		return
	}
	sig, _ := hex.DecodeString(r.Header.Get(wire.HeaderSignature))
	if !s.cfg.Relay.AuthorizeFetch(dest, r.Header.Get(wire.HeaderNonce), sig) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	after := queryInt(r, "after", 0)
	var raws [][]byte
	cur := after
	waitFor(r, s.cfg.Relay, func() bool {
		raws, cur = s.cfg.Relay.Mailbox(dest, after, defaultPage)
		return len(raws) > 0
	})
	writeFrames(w, packet.EncodeFrames(raws), cur, false)
}

func (s *Server) gateway(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	serve, upload, err := wire.DecodeGateway(body)
	if err != nil {
		http.Error(w, "bad gateway body", http.StatusBadRequest)
		return
	}
	org := origin(r, "gw:")
	if len(upload) > 0 {
		if !s.checkPoW(w, r, body) {
			return
		}
		s.cfg.Relay.Ingest(upload, org, s.cfg.Relay.Region())
	}
	req := relay.GatewayRequest{
		Origin: org, Serve: serve, After: queryInt(r, "after", 0),
		Carrier: r.URL.Query().Get("carrier") == "1", QuotaBytes: int(queryInt(r, "quota", 0)),
		Limit: defaultPage,
	}
	var raws [][]byte
	cur := req.After
	waitFor(r, s.cfg.Relay, func() bool {
		raws, cur = s.cfg.Relay.Gateway(req)
		return len(raws) > 0
	})
	writeFrames(w, packet.EncodeFrames(raws), cur, false)
}

func (s *Server) syncAuthorized(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if tok, ok := bearer(r); ok {
		for _, t := range s.cfg.SyncTokens {
			if subtle.ConstantTimeCompare([]byte(t), []byte(tok)) == 1 {
				return true
			}
		}
		http.Error(w, "bad token", http.StatusUnauthorized)
		return false
	}
	return s.checkPoW(w, r, body)
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):], true
	}
	return "", false
}

func (s *Server) syncPush(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok || !s.syncAuthorized(w, r, body) {
		return
	}
	items, err := wire.DecodeSync(body)
	if err != nil {
		http.Error(w, "bad sync body", http.StatusBadRequest)
		return
	}
	n := s.cfg.Relay.IngestSync(items, origin(r, "peer:"))
	writeJSON(w, wire.Accepted{Accepted: n})
}

func (s *Server) syncPull(w http.ResponseWriter, r *http.Request) {
	if !s.syncAuthorized(w, r, nil) {
		return
	}
	after := queryInt(r, "after", 0)
	limit := int(min(max(queryInt(r, "limit", defaultPage), 1), 2000))
	var items []wire.SyncItem
	var cur int64
	var more bool
	waitFor(r, s.cfg.Relay, func() bool {
		items, cur, more = s.cfg.Relay.Pull(after, limit, origin(r, "peer:"))
		return len(items) > 0
	})
	writeFrames(w, wire.EncodeSync(items), cur, more)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// limiter is a per-IP token bucket.
type limiter struct {
	mu    sync.Mutex
	rate  float64
	burst float64
	b     map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rate, burst float64) *limiter {
	return &limiter{rate: rate, burst: burst, b: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.b) > 100_000 { // bound memory under address-spraying
		l.b = map[string]*bucket{}
	}
	bk := l.b[key]
	if bk == nil {
		bk = &bucket{tokens: l.burst, last: now}
		l.b[key] = bk
	}
	bk.tokens = min(l.burst, bk.tokens+now.Sub(bk.last).Seconds()*l.rate)
	bk.last = now
	if bk.tokens < 1 {
		return false
	}
	bk.tokens--
	return true
}

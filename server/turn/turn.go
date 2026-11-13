// Package turn runs a relay's UDP services for direct links
// (docs/connectivity.md, docs/server.md §6c/§6d):
//
//   - a reflector (STUN binding) on UDP 3478 and 3479 for everyone: it tells
//     a client the address and port it was seen from, nothing else;
//   - TURN (RFC 8656) only for the relay's own admins, off by default, with
//     short-lived credentials from POST /v1/turn/credentials (signed by an
//     admin account) and limits on sessions, speed and monthly traffic.
package turn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/logging"
	pturn "github.com/pion/turn/v4"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Limits are the panel's TURN settings (applied at once).
type Limits struct {
	Enabled     bool `json:"turn_enabled"`
	MaxSessions int  `json:"turn_max_sessions"` // live allocations (default 10)
	MaxKbps     int  `json:"turn_max_kbps"`     // per session, both directions (default 256)
	MonthlyGB   int  `json:"turn_monthly_gb"`   // traffic cap per calendar month (default 50)
}

// DefaultLimits: off, with sane caps for when an admin turns it on.
func DefaultLimits() Limits { return Limits{MaxSessions: 10, MaxKbps: 256, MonthlyGB: 50} }

// Check reports a setting out of range.
func (l Limits) Check() error {
	switch {
	case l.MaxSessions < 1 || l.MaxSessions > 1000:
		return errors.New("turn_max_sessions must be 1…1000")
	case l.MaxKbps < 16 || l.MaxKbps > 100000:
		return errors.New("turn_max_kbps must be 16…100000")
	case l.MonthlyGB < 1 || l.MonthlyGB > 100000:
		return errors.New("turn_monthly_gb must be 1…100000")
	}
	return nil
}

// Config configures the service.
type Config struct {
	ListenHost string // default "0.0.0.0"
	Ports      []int  // reflector/TURN ports (default 3478, 3479)
	PublicIP   net.IP // address TURN relays from; nil: TURN unavailable (reflector only)
	RelayMin   int    // TURN relayed ports (default 61000…61999)
	RelayMax   int
	DataDir    string // monthly traffic counter (turn.json); "" keeps it in memory
	Identity   *identity.Identity
	IsAdmin    func(accountHash string) bool
	Limits     Limits
	Logf       func(format string, args ...any)
	Now        func() time.Time
}

// Service is the running reflector (and TURN).
type Service struct {
	cfg    Config
	srv    *pturn.Server
	secret []byte
	limits atomic.Pointer[Limits]

	mu       sync.Mutex
	sessions map[*countingConn]bool
	month    string
	bytes    int64 // this month, all sessions
	saved    time.Time
}

const realm = "mava"

// Start opens the UDP ports.
func Start(cfg Config) (*Service, error) {
	if cfg.ListenHost == "" {
		cfg.ListenHost = "0.0.0.0"
	}
	if len(cfg.Ports) == 0 {
		cfg.Ports = []int{wire.PortReflector, wire.PortReflectorAlt}
	}
	if cfg.RelayMin == 0 {
		cfg.RelayMin, cfg.RelayMax = wire.PortTURNMin, wire.PortTURNMax
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Service{cfg: cfg, sessions: map[*countingConn]bool{}}
	s.secret = make([]byte, 32)
	rand.Read(s.secret) // credentials die with the process; they last 10 min anyway
	l := cfg.Limits
	if l.MaxSessions == 0 {
		en := l.Enabled
		l = DefaultLimits()
		l.Enabled = en
	}
	s.limits.Store(&l)
	s.load()

	var pcs []pturn.PacketConnConfig
	for _, port := range cfg.Ports {
		pc, err := net.ListenPacket("udp4", net.JoinHostPort(cfg.ListenHost, strconv.Itoa(port)))
		if err != nil {
			for _, c := range pcs {
				c.PacketConn.Close()
			}
			return nil, fmt.Errorf("turn: listen udp %d: %w", port, err)
		}
		pcs = append(pcs, pturn.PacketConnConfig{PacketConn: pc, RelayAddressGenerator: &relayGen{s: s}})
	}
	quiet := logging.NewDefaultLoggerFactory()
	quiet.DefaultLogLevel = logging.LogLevelDisabled
	srv, err := pturn.NewServer(pturn.ServerConfig{
		Realm: realm, AuthHandler: s.auth, PacketConnConfigs: pcs, LoggerFactory: quiet,
	})
	if err != nil {
		return nil, err
	}
	s.srv = srv
	return s, nil
}

// Close stops the service.
func (s *Service) Close() error {
	s.save(true)
	return s.srv.Close()
}

// SetLimits changes the TURN settings at once.
func (s *Service) SetLimits(l Limits) { s.limits.Store(&l) }

// Limits returns the settings in force.
func (s *Service) Limits() Limits { return *s.limits.Load() }

// Available tells whether TURN can run here at all (a public address).
func (s *Service) Available() bool { return s.cfg.PublicIP != nil }

// Stats is shown in the admin panel.
type Stats struct {
	Available  bool   `json:"available"`
	Enabled    bool   `json:"enabled"`
	Sessions   int    `json:"sessions"`
	MonthBytes int64  `json:"month_bytes"`
	Month      string `json:"month"`
	PublicIP   string `json:"public_ip,omitempty"`
}

// Stats reports live sessions and this month's traffic.
func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollMonth()
	st := Stats{Available: s.Available(), Enabled: s.Limits().Enabled, Sessions: len(s.sessions), MonthBytes: s.bytes, Month: s.month}
	if s.cfg.PublicIP != nil {
		st.PublicIP = s.cfg.PublicIP.String()
	}
	return st
}

// ---- credentials ----

// username = "<expiry unix>:<account hash>", password = HMAC(secret, username).
func (s *Service) password(username string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(username))
	return base64.RawStdEncoding.EncodeToString(m.Sum(nil))
}

func (s *Service) auth(username, rlm string, _ net.Addr) ([]byte, bool) {
	exp, acct, ok := strings.Cut(username, ":")
	t, err := strconv.ParseInt(exp, 10, 64)
	if !ok || err != nil || s.cfg.Now().Unix() > t || !s.Limits().Enabled || !s.Available() || !s.cfg.IsAdmin(acct) {
		return nil, false
	}
	return pturn.GenerateAuthKey(username, rlm, s.password(username)), true
}

// Credentials is the answer of POST /v1/turn/credentials.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Expires  int64  `json:"expires"`
	Port     int    `json:"port"`
}

// CredentialsHandler serves POST /v1/turn/credentials: signed by an
// account (X-Account, X-Day, X-Signature over DomainTurnCredentials | day);
// only the relay's admins get them, and only while TURN is on.
func (s *Service) CredentialsHandler(w http.ResponseWriter, r *http.Request) {
	pk, err1 := hex.DecodeString(r.Header.Get(wire.HeaderAccount))
	sig, err2 := hex.DecodeString(r.Header.Get(wire.HeaderSignature))
	d, err3 := strconv.ParseUint(r.Header.Get(wire.HeaderDay), 10, 32)
	pub, err4 := identity.PublicFromBytes(pk)
	today := wire.Day(s.cfg.Now())
	day := uint32(d)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || day+1 < today || day > today+1 ||
		!pub.Verify(wire.DomainTurnCredentials, wire.QuotaSigBody(day), sig) {
		http.Error(w, "signed request required", http.StatusUnauthorized)
		return
	}
	acct := pub.Hash().String()
	switch {
	case !s.Available() || !s.Limits().Enabled:
		http.Error(w, "TURN is off on this relay", http.StatusNotFound)
		return
	case !s.cfg.IsAdmin(acct):
		http.Error(w, "TURN is only for this relay's admins", http.StatusForbidden)
		return
	}
	exp := s.cfg.Now().Add(10 * time.Minute).Unix()
	user := strconv.FormatInt(exp, 10) + ":" + acct
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(Credentials{Username: user, Password: s.password(user), Expires: exp, Port: s.cfg.Ports[0]})
}

// ---- relayed addresses: port range, counted and limited ----

type relayGen struct{ s *Service }

func (g *relayGen) Validate() error { return nil }

func (g *relayGen) AllocateConn(string, int) (net.Conn, net.Addr, error) {
	return nil, nil, errors.New("turn: TCP allocations not offered")
}

func (g *relayGen) AllocatePacketConn(network string, _ int) (net.PacketConn, net.Addr, error) {
	s := g.s
	l := s.Limits()
	s.mu.Lock()
	s.rollMonth()
	full := len(s.sessions) >= l.MaxSessions
	over := s.bytes >= int64(l.MonthlyGB)<<30
	s.mu.Unlock()
	switch {
	case !l.Enabled || s.cfg.PublicIP == nil:
		return nil, nil, errors.New("turn: off")
	case full:
		return nil, nil, errors.New("turn: session limit reached")
	case over:
		return nil, nil, errors.New("turn: monthly traffic cap reached")
	}
	for try := 0; try < 20; try++ {
		port := s.cfg.RelayMin + randInt(s.cfg.RelayMax-s.cfg.RelayMin+1)
		pc, err := net.ListenPacket("udp4", net.JoinHostPort(s.cfg.ListenHost, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		c := &countingConn{PacketConn: pc, s: s, kbps: l.MaxKbps, last: time.Now()}
		s.mu.Lock()
		s.sessions[c] = true
		s.mu.Unlock()
		return c, &net.UDPAddr{IP: s.cfg.PublicIP, Port: port}, nil
	}
	return nil, nil, errors.New("turn: no free relay port")
}

func randInt(n int) int {
	var b [4]byte
	rand.Read(b[:])
	return int(uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3])) % n
}

// countingConn counts a session's bytes into the monthly total and holds it
// to MaxKbps (token bucket over both directions; excess datagrams dropped).
type countingConn struct {
	net.PacketConn
	s      *Service
	mu     sync.Mutex
	kbps   int
	tokens float64
	last   time.Time
	closed bool
}

func (c *countingConn) allow(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	rate := float64(c.kbps) * 1000 / 8                            // bytes per second
	c.tokens = min(c.tokens+now.Sub(c.last).Seconds()*rate, rate) // burst: one second
	c.last = now
	if c.tokens < float64(n) {
		return false
	}
	c.tokens -= float64(n)
	return true
}

func (c *countingConn) count(n int) {
	c.s.mu.Lock()
	c.s.rollMonth()
	c.s.bytes += int64(n)
	c.s.mu.Unlock()
	c.s.save(false)
}

func (c *countingConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, a, err := c.PacketConn.ReadFrom(p)
		if err != nil || c.allow(n) {
			if err == nil {
				c.count(n)
			}
			return n, a, err
		}
	}
}

func (c *countingConn) WriteTo(p []byte, a net.Addr) (int, error) {
	if !c.allow(len(p)) {
		return len(p), nil // dropped: over the session's speed
	}
	n, err := c.PacketConn.WriteTo(p, a)
	if err == nil {
		c.count(n)
	}
	return n, err
}

func (c *countingConn) Close() error {
	c.s.mu.Lock()
	delete(c.s.sessions, c)
	c.s.mu.Unlock()
	return c.PacketConn.Close()
}

// ---- monthly counter ----

type saved struct {
	Month string `json:"month"`
	Bytes int64  `json:"bytes"`
}

// rollMonth starts a new count on the 1st; s.mu held.
func (s *Service) rollMonth() {
	m := s.cfg.Now().UTC().Format("2006-01")
	if m != s.month {
		s.month, s.bytes = m, 0
	}
}

func (s *Service) path() string {
	if s.cfg.DataDir == "" {
		return ""
	}
	return s.cfg.DataDir + "/turn.json"
}

func (s *Service) load() {
	s.rollMonth()
	if p := s.path(); p != "" {
		var v saved
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &v) == nil && v.Month == s.month {
			s.bytes = v.Bytes
		}
	}
}

// save writes the counter at most once a minute (always when final).
func (s *Service) save(final bool) {
	p := s.path()
	if p == "" {
		return
	}
	s.mu.Lock()
	if !final && time.Since(s.saved) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.saved = time.Now()
	b, _ := json.Marshal(saved{s.month, s.bytes})
	s.mu.Unlock()
	os.WriteFile(p+".new", b, 0o600)
	os.Rename(p+".new", p)
}

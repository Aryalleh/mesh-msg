// Package admin is a relay's web panel at /admin.
//
// Admins log in with their Mava account: they enter their account id and
// the relay sends a one-time code to that account as an end-to-end
// encrypted Mava message (the relay knows the account's keys from its
// announces). The owner account is built in and can't be removed; more
// admins are added in the panel or with "meshmsg-relay admin add <id>".
package admin

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
	"github.com/Aryalleh/mesh-msg/server/turn"
)

// OwnerID is the network owner's account: an admin of every relay built
// from this code. It can't be removed.
const OwnerID = "ad2107c8e944bf2a99b3f0cdb2c40903"

const (
	codeTTL     = 10 * time.Minute
	codeTries   = 5
	codeEvery   = time.Minute // one code per account per minute
	sessionTTL  = 12 * time.Hour
	cookieName  = "mava_admin"
	panelHeader = "X-Mava-Panel" // required on POSTs (CSRF)
)

//go:embed panel.html
var panelHTML []byte

// Settings are what the panel (and the admin CLI) may change. They are kept
// in data_dir/panel.json and override relay.json.
type Settings struct {
	Admins       []string `json:"admins,omitempty"` // besides the owner
	PowBits      *int     `json:"pow_bits,omitempty"`
	TTLHours     *int     `json:"ttl_hours,omitempty"`
	MediaMaxMB   *int     `json:"media_max_mb,omitempty"`
	MediaStoreMB *int     `json:"media_store_mb,omitempty"`
	// Important media (api.MediaPolicy): apply at once, without a restart.
	MediaPerDay           *int    `json:"media_important_per_day,omitempty"`
	MediaImportantHours   *int    `json:"media_important_hours,omitempty"`
	MediaNewAccountDays   *int    `json:"media_new_account_days,omitempty"`
	MediaNewAccountPerDay *int    `json:"media_new_account_per_day,omitempty"`
	MediaPowBaseBits      *int    `json:"media_pow_base_bits,omitempty"`
	MediaPowMaxBits       *int    `json:"media_pow_max_bits,omitempty"`
	MediaPowAgeTiers      *string `json:"media_pow_age_tiers,omitempty"` // "3:3, 30:2, 180:1" (days:bits)
	// TURN for this relay's admins (turn.Limits): apply at once.
	TurnEnabled     *bool    `json:"turn_enabled,omitempty"`
	TurnMaxSessions *int     `json:"turn_max_sessions,omitempty"`
	TurnMaxKbps     *int     `json:"turn_max_kbps,omitempty"`
	TurnMonthlyGB   *int     `json:"turn_monthly_gb,omitempty"`
	MediaAccounts   []string `json:"media_accounts,omitempty"`
	Log             *bool    `json:"log,omitempty"`
}

// Policy lays the saved important-media settings over base (relay.json's)
// and checks the result.
func (s Settings) Policy(base api.MediaPolicy) (api.MediaPolicy, error) {
	p := base
	for _, f := range []struct {
		dst *int
		v   *int
	}{
		{&p.PerDay, s.MediaPerDay}, {&p.Hours, s.MediaImportantHours},
		{&p.NewAccountDays, s.MediaNewAccountDays}, {&p.NewAccountPerDay, s.MediaNewAccountPerDay},
		{&p.PowBaseBits, s.MediaPowBaseBits}, {&p.PowMaxBits, s.MediaPowMaxBits},
	} {
		if f.v != nil {
			*f.dst = *f.v
		}
	}
	if s.MediaPowAgeTiers != nil {
		ts, err := api.ParseTiers(*s.MediaPowAgeTiers)
		if err != nil {
			return base, err
		}
		p.AgeTiers = ts
	}
	return p, p.Check()
}

// TurnLimits lays the saved TURN settings over base (relay.json's).
func (s Settings) TurnLimits(base turn.Limits) (turn.Limits, error) {
	l := base
	if s.TurnEnabled != nil {
		l.Enabled = *s.TurnEnabled
	}
	for _, f := range []struct {
		dst *int
		v   *int
	}{{&l.MaxSessions, s.TurnMaxSessions}, {&l.MaxKbps, s.TurnMaxKbps}, {&l.MonthlyGB, s.TurnMonthlyGB}} {
		if f.v != nil {
			*f.dst = *f.v
		}
	}
	return l, l.Check()
}

// SettingsPath is where the overrides live.
func SettingsPath(dataDir string) string { return filepath.Join(dataDir, "panel.json") }

// LoadSettings reads the overrides (none: zero Settings).
func LoadSettings(dataDir string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(SettingsPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// SaveSettings writes the overrides atomically.
func SaveSettings(dataDir string, s Settings) error {
	s.Admins = CleanAdmins(s.Admins)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := SettingsPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, SettingsPath(dataDir))
}

// ParseID checks an account id (32 hex characters, as "mesh id" or the
// app's account screen shows it).
func ParseID(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return "", fmt.Errorf("%q is not an account id (32 hex characters)", s)
	}
	return s, nil
}

// CleanAdmins drops the owner (always admin anyway), duplicates and junk.
func CleanAdmins(ids []string) []string {
	var out []string
	for _, id := range ids {
		id, err := ParseID(id)
		if err != nil || id == OwnerID || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// Info is the relay's fixed description, shown on the panel.
type Info struct {
	Name, Region, Version, PubKey, Listen string
	Peers                                 []string
	Started                               time.Time
}

// Config wires the panel to the relay.
type Config struct {
	Info     Info
	DataDir  string
	Identity *identity.Identity
	Relay    *relay.Relay
	API      *api.Server
	// BasePolicy: the important-media rules from relay.json (panel
	// settings are laid over it).
	BasePolicy api.MediaPolicy
	// TURN: the relay's reflector/TURN service (nil: not running) and the
	// TURN settings from relay.json.
	TURN     *turn.Service
	BaseTURN turn.Limits
	// Effective returns the settings in use (relay.json + panel.json).
	Effective func() map[string]any
	// Restart asks the process to exit so its supervisor restarts it with
	// the saved settings.
	Restart func()
	Now     func() time.Time
}

type pending struct {
	code  string
	until time.Time
	tries int
}

// Panel is the http.Handler for /admin.
type Panel struct {
	cfg Config

	mu       sync.Mutex
	codes    map[string]*pending  // account → code
	lastCode map[string]time.Time // account → last code sent
	sessions map[string]session   // token → session
	sent     []time.Time          // codes sent in the last hour (global cap)
}

type session struct {
	admin string
	until time.Time
}

// New builds the panel.
func New(cfg Config) *Panel {
	if cfg.BasePolicy.PerDay == 0 {
		cfg.BasePolicy = api.DefaultMediaPolicy()
	}
	if cfg.BaseTURN.MaxSessions == 0 {
		cfg.BaseTURN = turn.DefaultLimits()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Panel{cfg: cfg, codes: map[string]*pending{}, lastCode: map[string]time.Time{}, sessions: map[string]session{}}
}

// Admins is the current admin list, owner first.
func (p *Panel) Admins() []string {
	s, _ := LoadSettings(p.cfg.DataDir)
	return append([]string{OwnerID}, CleanAdmins(s.Admins)...)
}

func (p *Panel) isAdmin(id string) bool { return slices.Contains(p.Admins(), id) }

// Routes registers the panel under /admin.
func (p *Panel) Routes(mount func(pattern string, h http.Handler)) {
	h := func(f http.HandlerFunc) http.Handler { return f }
	mount("GET /admin", h(p.page))
	mount("GET /admin/", h(p.page))
	mount("POST /admin/api/code", h(p.post(false, p.sendCode)))
	mount("POST /admin/api/login", h(p.post(false, p.login)))
	mount("POST /admin/api/logout", h(p.post(true, p.logout)))
	mount("GET /admin/api/state", h(p.auth(p.state)))
	mount("POST /admin/api/settings", h(p.post(true, p.saveSettings)))
	mount("POST /admin/api/admins", h(p.post(true, p.changeAdmins)))
	mount("POST /admin/api/media/delete", h(p.post(true, p.deleteMedia)))
	mount("POST /admin/api/restart", h(p.post(true, p.restart)))
}

func (p *Panel) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write(panelHTML)
}

func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	jsonOut(w, code, map[string]string{"error": msg})
}

// sessionOf returns the logged-in admin, if any (and still an admin).
func (p *Panel) sessionOf(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	p.mu.Lock()
	s, ok := p.sessions[c.Value]
	if ok && p.cfg.Now().After(s.until) {
		delete(p.sessions, c.Value)
		ok = false
	}
	p.mu.Unlock()
	if !ok || !p.isAdmin(s.admin) {
		return "", false
	}
	return s.admin, true
}

func (p *Panel) auth(f func(w http.ResponseWriter, r *http.Request, admin string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, ok := p.sessionOf(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "log in")
			return
		}
		f(w, r, admin)
	}
}

// post: JSON body, CSRF header, optionally a session.
func (p *Panel) post(needAuth bool, f func(w http.ResponseWriter, r *http.Request, admin string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(panelHeader) != "1" {
			fail(w, http.StatusForbidden, "missing "+panelHeader+" header")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if !needAuth {
			f(w, r, "")
			return
		}
		p.auth(f)(w, r)
	}
}

func randomDigits(n int) string {
	var b strings.Builder
	for range n {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String()
}

func (p *Panel) sendCode(w http.ResponseWriter, r *http.Request, _ string) {
	var req struct{ ID string }
	json.NewDecoder(r.Body).Decode(&req)
	id, err := ParseID(req.ID)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// Same answer for non-admins: the panel doesn't tell who is an admin.
	const sentMsg = "If this account is an admin of this relay, a code is on its way to it in Mava. It can take a minute."
	if !p.isAdmin(id) {
		jsonOut(w, http.StatusOK, map[string]string{"info": sentMsg})
		return
	}
	now := p.cfg.Now()
	p.mu.Lock()
	recent := p.sent[:0]
	for _, t := range p.sent {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	p.sent = recent
	if now.Sub(p.lastCode[id]) < codeEvery || len(p.sent) >= 30 {
		p.mu.Unlock()
		fail(w, http.StatusTooManyRequests, "A code was just sent. Wait a minute before asking again.")
		return
	}
	p.mu.Unlock()

	var dest packet.Hash
	b, _ := hex.DecodeString(id)
	copy(dest[:], b)
	pub, ok := p.cfg.Relay.PublicFor(dest)
	if !ok {
		fail(w, http.StatusConflict, "This relay doesn't know this account's keys yet. Open Mava with this relay in its relay list (it announces itself), then try again.")
		return
	}
	code := randomDigits(8)
	text := fmt.Sprintf("Admin panel code for relay %s: %s\nValid for 10 minutes. If you didn't ask for it, ignore this message.", p.cfg.Info.Name, code)
	pkts, err := message.BuildText(p.cfg.Identity, pub, text, message.Options{Priority: true})
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	var raws [][]byte
	for _, pk := range pkts {
		raw, err := pk.Encode()
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		raws = append(raws, raw)
	}
	p.cfg.Relay.Ingest(raws, p.cfg.Info.Name, p.cfg.Info.Region)

	p.mu.Lock()
	p.codes[id] = &pending{code: code, until: now.Add(codeTTL)}
	p.lastCode[id] = now
	p.sent = append(p.sent, now)
	p.mu.Unlock()
	jsonOut(w, http.StatusOK, map[string]string{"info": sentMsg})
}

func (p *Panel) login(w http.ResponseWriter, r *http.Request, _ string) {
	var req struct{ ID, Code string }
	json.NewDecoder(r.Body).Decode(&req)
	id, err := ParseID(req.ID)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r >= '۰' && r <= '۹' { // Persian digits
			return '0' + (r - '۰')
		}
		return -1
	}, req.Code)
	now := p.cfg.Now()
	p.mu.Lock()
	pc := p.codes[id]
	ok := false
	if pc != nil && now.Before(pc.until) && p.isAdmin(id) {
		pc.tries++
		ok = subtle.ConstantTimeCompare([]byte(pc.code), []byte(code)) == 1
		if ok || pc.tries >= codeTries {
			delete(p.codes, id)
		}
	}
	var token string
	if ok {
		var t [32]byte
		rand.Read(t[:])
		token = hex.EncodeToString(t[:])
		for k, s := range p.sessions { // drop expired
			if now.After(s.until) {
				delete(p.sessions, k)
			}
		}
		p.sessions[token] = session{admin: id, until: now.Add(sessionTTL)}
	}
	p.mu.Unlock()
	if !ok {
		fail(w, http.StatusUnauthorized, "Wrong or expired code. After 5 wrong tries, ask for a new one.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/admin", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	jsonOut(w, http.StatusOK, map[string]string{"admin": id})
}

func (p *Panel) logout(w http.ResponseWriter, r *http.Request, _ string) {
	if c, err := r.Cookie(cookieName); err == nil {
		p.mu.Lock()
		delete(p.sessions, c.Value)
		p.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) state(w http.ResponseWriter, r *http.Request, admin string) {
	files, used, capB, on := p.cfg.API.MediaFiles()
	saved, _ := LoadSettings(p.cfg.DataDir)
	st := p.cfg.Relay.Stats()
	jsonOut(w, http.StatusOK, map[string]any{
		"you":     admin,
		"owner":   OwnerID,
		"admins":  p.Admins(),
		"info":    p.cfg.Info,
		"uptime":  int(p.cfg.Now().Sub(p.cfg.Info.Started).Seconds()),
		"stats":   st,
		"current": p.cfg.Effective(),
		"saved":   saved,
		"secure":  r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		"media": map[string]any{
			"on": on, "used": used, "cap": capB, "count": len(files), "files": files[:min(len(files), 200)],
		},
		"ledger": p.cfg.API.Ledger().Stats(),
		"turn":   p.turnStats(),
	})
}

func (p *Panel) turnStats() any {
	if p.cfg.TURN == nil {
		return nil
	}
	return p.cfg.TURN.Stats()
}

func (p *Panel) saveSettings(w http.ResponseWriter, r *http.Request, _ string) {
	var in Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	check := func(name string, v *int, lo, hi int) error {
		if v != nil && (*v < lo || *v > hi) {
			return fmt.Errorf("%s must be %d…%d", name, lo, hi)
		}
		return nil
	}
	for _, err := range []error{
		check("pow_bits", in.PowBits, 0, 28),
		check("ttl_hours", in.TTLHours, 1, 24*30),
		check("media_max_mb", in.MediaMaxMB, 0, 64),
		check("media_store_mb", in.MediaStoreMB, 16, 1<<20),
	} {
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var accounts []string
	for _, a := range in.MediaAccounts {
		if strings.TrimSpace(a) == "" {
			continue
		}
		id, err := ParseID(a)
		if err != nil {
			fail(w, http.StatusBadRequest, "media accounts: "+err.Error())
			return
		}
		if !slices.Contains(accounts, id) {
			accounts = append(accounts, id)
		}
	}
	policy, err := in.Policy(p.cfg.BasePolicy)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	limits, err := in.TurnLimits(p.cfg.BaseTURN)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, _ := LoadSettings(p.cfg.DataDir)
	in.Admins = cur.Admins // changed only through /admins
	in.MediaAccounts = accounts
	if err := SaveSettings(p.cfg.DataDir, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	p.cfg.API.SetMediaPolicy(policy)
	if p.cfg.TURN != nil {
		p.cfg.TURN.SetLimits(limits)
	}
	jsonOut(w, http.StatusOK, map[string]string{"info": "Saved. Important-file rules apply now; restart the relay for the rest."})
}

func (p *Panel) changeAdmins(w http.ResponseWriter, r *http.Request, _ string) {
	var req struct{ Add, Remove string }
	json.NewDecoder(r.Body).Decode(&req)
	s, err := LoadSettings(p.cfg.DataDir)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch {
	case req.Add != "":
		id, err := ParseID(req.Add)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		s.Admins = append(s.Admins, id)
	case req.Remove != "":
		id, err := ParseID(req.Remove)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if id == OwnerID {
			fail(w, http.StatusForbidden, "The owner can't be removed.")
			return
		}
		s.Admins = slices.DeleteFunc(s.Admins, func(a string) bool { return strings.EqualFold(a, id) })
	}
	if err := SaveSettings(p.cfg.DataDir, s); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"admins": p.Admins()})
}

func (p *Panel) deleteMedia(w http.ResponseWriter, r *http.Request, _ string) {
	var req struct{ ID string }
	json.NewDecoder(r.Body).Decode(&req)
	if !p.cfg.API.DeleteMedia(strings.ToLower(req.ID)) {
		fail(w, http.StatusNotFound, "no such file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) restart(w http.ResponseWriter, r *http.Request, _ string) {
	if p.cfg.Restart == nil {
		fail(w, http.StatusNotImplemented, "restart not available")
		return
	}
	jsonOut(w, http.StatusOK, map[string]string{"info": "Restarting… reload this page in a few seconds."})
	go func() { time.Sleep(500 * time.Millisecond); p.cfg.Restart() }()
}

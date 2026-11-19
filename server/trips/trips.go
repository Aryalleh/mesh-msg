// Package trips keeps the trip a relay runs (docs/trip.md): one signed trip
// card, served to members at GET /v1/trip, set and ended by the relay's
// admins who lead the trip (PUT / DELETE /v1/trip, signed by their account).
// The relay never holds the trip's key: it stores and passes on the
// members' encrypted reports like any other packets, and its admin panel's
// map opens them in the leader's browser.
package trips

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/trip"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Config configures the service.
type Config struct {
	DataDir string
	IsAdmin func(accountHash string) bool
	Now     func() time.Time
}

// Service holds the relay's trip.
type Service struct {
	cfg  Config
	mu   sync.Mutex
	cur  *trip.Signed
	card trip.Card
}

func (s *Service) path() string { return filepath.Join(s.cfg.DataDir, "trip.json") }

// New loads the saved trip, if any.
func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Service{cfg: cfg}
	if b, err := os.ReadFile(s.path()); err == nil {
		if sg, err := trip.ParseSigned(string(b)); err == nil {
			if c, err := sg.Open(); err == nil {
				s.cur, s.card = &sg, c
			}
		}
	}
	return s
}

// Current returns the running trip (false if none or it has ended).
func (s *Service) Current() (trip.Signed, trip.Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.card.Ended(s.cfg.Now()) {
		return trip.Signed{}, trip.Card{}, false
	}
	return *s.cur, s.card, true
}

// Routes registers GET, PUT and DELETE /v1/trip.
func (s *Service) Routes(mount func(pattern string, h http.Handler)) {
	mount("GET /v1/trip", http.HandlerFunc(s.get))
	mount("PUT /v1/trip", http.HandlerFunc(s.put))
	mount("DELETE /v1/trip", http.HandlerFunc(s.del))
}

func (s *Service) get(w http.ResponseWriter, _ *http.Request) {
	sg, _, ok := s.Current()
	if !ok {
		http.Error(w, "no trip on this relay", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(sg.JSON())
}

// signer checks the account signature of a trip request and returns the
// account's hash.
func (s *Service) signer(r *http.Request, domain string, what []byte) (string, error) {
	pk, err1 := hex.DecodeString(r.Header.Get(wire.HeaderAccount))
	sig, err2 := hex.DecodeString(r.Header.Get(wire.HeaderSignature))
	d, err3 := strconv.ParseUint(r.Header.Get(wire.HeaderDay), 10, 32)
	pub, err4 := identity.PublicFromBytes(pk)
	today := wire.Day(s.cfg.Now())
	day := uint32(d)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || day+1 < today || day > today+1 ||
		!pub.Verify(domain, wire.TripSigBody(day, what), sig) {
		return "", errors.New("signed request required")
	}
	return pub.Hash().String(), nil
}

func (s *Service) put(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	sg, err := trip.ParseSigned(string(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d := sg.Digest()
	acct, err := s.signer(r, wire.DomainTripPut, d[:])
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !s.cfg.IsAdmin(acct) {
		http.Error(w, "only this relay's admins can run a trip on it", http.StatusForbidden)
		return
	}
	c, err := sg.Open()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := s.cfg.Now()
	switch {
	case c.Ended(now):
		http.Error(w, "this trip has already ended", http.StatusBadRequest)
		return
	case c.Made > now.Add(24*time.Hour).Unix():
		http.Error(w, "the card is dated in the future", http.StatusBadRequest)
		return
	case !c.IsLeader(acct):
		http.Error(w, "you are not one of this trip's leaders", http.StatusForbidden)
		return
	}
	for _, l := range c.Leaders {
		if !s.cfg.IsAdmin(strings.ToLower(l.ID)) {
			name := l.Name
			if name == "" {
				name = l.ID
			}
			http.Error(w, "every leader must be an admin of this relay; "+name+" is not (add them in the relay's admin panel)", http.StatusForbidden)
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil && s.card.Key == c.Key && c.Made <= s.card.Made {
		http.Error(w, "the relay has a newer card of this trip", http.StatusConflict)
		return
	}
	if err := writeFile(s.path(), sg.JSON()); err != nil {
		http.Error(w, "cannot save the trip", http.StatusInternalServerError)
		return
	}
	s.cur, s.card = &sg, c
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) del(w http.ResponseWriter, r *http.Request) {
	h, err := packet.ParseHash(r.URL.Query().Get("trip"))
	if err != nil {
		http.Error(w, "trip= required", http.StatusBadRequest)
		return
	}
	acct, err := s.signer(r, wire.DomainTripDelete, h[:])
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !s.cfg.IsAdmin(acct) {
		http.Error(w, "only this relay's admins can end its trip", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.card.Hash() != h.String() {
		http.Error(w, "this trip is not on the relay", http.StatusNotFound)
		return
	}
	if err := os.Remove(s.path()); err != nil && !os.IsNotExist(err) {
		http.Error(w, "cannot remove the trip", http.StatusInternalServerError)
		return
	}
	s.cur, s.card = nil, trip.Card{}
	w.WriteHeader(http.StatusNoContent)
}

func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// State is what the admin panel shows about the trip.
type State struct {
	Card    *trip.Card      `json:"card,omitempty"`
	Hash    string          `json:"hash,omitempty"`
	Signed  json.RawMessage `json:"signed,omitempty"`
	Link    string          `json:"link,omitempty"`
	Leaders []LeaderState   `json:"leaders,omitempty"`
}

// LeaderState is one leader and whether they are still an admin.
type LeaderState struct {
	trip.Leader
	Admin bool `json:"admin"`
}

// PanelState returns the panel's view of the trip.
func (s *Service) PanelState() State {
	sg, c, ok := s.Current()
	if !ok {
		return State{}
	}
	st := State{Card: &c, Hash: c.Hash(), Signed: sg.JSON(), Link: sg.URI()}
	for _, l := range c.Leaders {
		st.Leaders = append(st.Leaders, LeaderState{Leader: l, Admin: s.cfg.IsAdmin(strings.ToLower(l.ID))})
	}
	return st
}

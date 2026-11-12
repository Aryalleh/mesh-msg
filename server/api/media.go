package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/pow"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/ledger"
)

// Media: a relay may hold important encrypted files (core/media,
// protocol.md §6b; normal files never touch a relay).
//
//   - PUT /v1/media/{id}: body = the blob, X-PoW over wire.MediaPoWBody(id),
//     X-Readers = how many downloads to expect (one per recipient), signed
//     by the account (X-Account, X-Day, X-Signature).
//   - GET /v1/media/{id}: the blob; "Range: bytes=N-" resumes.
//   - GET /v1/media/quota: an account's allowance today (signed).
//
// Each account may upload a few files per UTC day, counted on every relay
// through the ledger (one file to a group counts once), with proof-of-work
// that grows for young accounts and with each upload (MediaPolicy). A file is deleted as soon as it was downloaded to the end
// X-Readers times, or after the policy's keep time (default 7 days). An accepted
// file is never evicted early: when the store is full, uploads get 507.
// Media is not copied between relays.
type MediaConfig struct {
	Dir     string // where blobs live
	MaxMB   int    // largest file; 0 = media off
	StoreMB int    // total cap (default 2048)
	// Policy: allowance, keep time, proof-of-work (nil: DefaultMediaPolicy).
	Policy *MediaPolicy
	// Accounts, if set, are the only accounts (identity hashes, hex) that
	// may upload. Anyone may download (files are encrypted and fetched by
	// their unguessable id).
	Accounts []string
}

func (c MediaConfig) allowed(h string) bool {
	if len(c.Accounts) == 0 {
		return true
	}
	for _, a := range c.Accounts {
		if strings.EqualFold(strings.TrimSpace(a), h) {
			return true
		}
	}
	return false
}

func (c MediaConfig) storeBytes() int64 {
	if c.StoreMB <= 0 {
		return 2048 << 20
	}
	return int64(c.StoreMB) << 20
}

// SetMediaPolicy changes the important-media rules at once (admin panel).
// Files already stored keep their time.
func (s *Server) SetMediaPolicy(p MediaPolicy) { s.policy.Store(&p) }

// MediaPolicy returns the rules in force.
func (s *Server) MediaPolicy() MediaPolicy { return *s.policy.Load() }

type mediaMeta struct {
	Size    int64     `json:"size"`
	Readers int       `json:"readers"`
	Served  int       `json:"served"`
	Expires time.Time `json:"expires"`
}

type mediaStore struct {
	dir  string
	mu   sync.Mutex
	used int64
	meta map[string]*mediaMeta
	// uploads in progress per account (counted against the allowance)
	pending map[string]map[string]bool
}

func (s *Server) mediaMaxMB() int {
	if s.media == nil {
		return 0
	}
	return s.cfg.Media.MaxMB
}

func (s *Server) openMedia() error {
	dir := s.cfg.Media.Dir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	m := &mediaStore{dir: dir, meta: map[string]*mediaMeta{}, pending: map[string]map[string]bool{}}
	os.Remove(filepath.Join(dir, "usage.json")) // step-1 per-relay counts; the ledger counts now
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			if strings.HasSuffix(e.Name(), ".tmp") {
				os.Remove(filepath.Join(dir, e.Name()))
			}
			continue
		}
		var mm mediaMeta
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || json.Unmarshal(b, &mm) != nil {
			continue
		}
		m.meta[id] = &mm
		m.used += mm.Size
	}
	s.media = m
	go func() {
		for range time.Tick(time.Minute) {
			m.expire(time.Now())
		}
	}()
	return nil
}

func (m *mediaStore) blob(id string) string { return filepath.Join(m.dir, id) }

func (m *mediaStore) save(id string, mm *mediaMeta) {
	b, _ := json.Marshal(mm)
	os.WriteFile(filepath.Join(m.dir, id+".json"), b, 0o600)
}

// remove deletes a file; m.mu held.
func (m *mediaStore) remove(id string) {
	if mm, ok := m.meta[id]; ok {
		m.used -= mm.Size
		delete(m.meta, id)
	}
	os.Remove(m.blob(id))
	os.Remove(filepath.Join(m.dir, id+".json"))
}

func (m *mediaStore) expire(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mm := range m.meta {
		if now.After(mm.Expires) {
			m.remove(id)
		}
	}
}

func mediaID(r *http.Request) (string, bool) {
	id := strings.ToLower(r.PathValue("id"))
	b, err := hex.DecodeString(id)
	return id, err == nil && len(b) == 16
}

func (s *Server) mediaPut(w http.ResponseWriter, r *http.Request) {
	m := s.media
	id, ok := mediaID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	// Signed for the relay's day exactly (the client takes it from the quota).
	up, ok := s.mediaSigned(w, r, wire.DomainMediaUpload, func(day uint32) []byte { return wire.MediaSigBody(id, day) }, true)
	if !ok {
		return
	}
	if !s.cfg.Media.allowed(up.hash) {
		http.Error(w, "this account may not upload media to this relay", http.StatusForbidden)
		return
	}
	readers, _ := strconv.Atoi(r.Header.Get(wire.HeaderReaders))
	readers = min(max(readers, 1), 64)
	limit := int64(s.cfg.Media.MaxMB)<<20 + int64(s.cfg.Media.MaxMB)*16*16 + 16 // + per-chunk tags
	if r.ContentLength > limit {
		http.Error(w, "file too large for this relay", http.StatusRequestEntityTooLarge)
		return
	}
	p := s.MediaPolicy()
	var bid [16]byte
	hex.Decode(bid[:], []byte(id))
	m.mu.Lock()
	if _, ok := m.meta[id]; ok {
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	capB := s.cfg.Media.storeBytes()
	if r.ContentLength > 0 && m.used+r.ContentLength > capB {
		m.mu.Unlock()
		http.Error(w, "relay media store is full", http.StatusInsufficientStorage)
		return
	}
	// The daily allowance, counted network-wide from the ledger; a file the
	// account already uploaded (to any relay) counts once. The slot is taken
	// now so parallel uploads can't pass it, and given back on failure.
	counted := s.ledger.Has(up.pub.Bytes(), bid)
	st := s.accountState(up.hash, up.day)
	if !counted {
		if st.used >= p.Allowance(st.age) {
			m.mu.Unlock()
			reset := time.Unix(int64(up.day+1)*86400, 0)
			w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(reset).Seconds())+1))
			http.Error(w, fmt.Sprintf("daily allowance of %d important files used; resets %s", p.Allowance(st.age), reset.UTC().Format(time.RFC3339)), http.StatusTooManyRequests)
			return
		}
	}
	// Proof of work: more for young accounts and for each file today.
	need := p.Bits(st.age, st.used, s.loadBits())
	nonce, err := strconv.ParseUint(r.Header.Get(wire.HeaderPoW), 10, 64)
	if need > 0 && (err != nil || !pow.Check(wire.MediaPoWBody(id, up.hash, up.day), nonce, need)) {
		m.mu.Unlock()
		w.Header().Set(wire.HeaderPoWBits, strconv.Itoa(need))
		http.Error(w, fmt.Sprintf("proof of work of %d bits required", need), http.StatusPaymentRequired)
		return
	}
	if !counted {
		m.reserve(up.hash, id, true)
		defer func() {
			m.mu.Lock()
			m.reserve(up.hash, id, false)
			m.mu.Unlock()
		}()
	}
	m.mu.Unlock()

	tmp, err := os.CreateTemp(m.dir, "*.tmp")
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), http.MaxBytesReader(w, r.Body, limit))
	tmp.Close()
	if err != nil || n == 0 {
		http.Error(w, "upload failed or too large", http.StatusBadRequest)
		return
	}
	if hex.EncodeToString(h.Sum(nil)[:16]) != id {
		http.Error(w, "content does not match id", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used+n > capB {
		http.Error(w, "relay media store is full", http.StatusInsufficientStorage)
		return
	}
	if err := os.Rename(tmp.Name(), m.blob(id)); err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	mm := &mediaMeta{Size: n, Readers: readers, Expires: time.Now().Add(time.Duration(p.Hours) * time.Hour)}
	m.meta[id] = mm
	m.used += n
	m.save(id, mm)
	// Into the ledger, signed by this relay; it spreads to the others.
	if e, err := ledger.New(s.cfg.Identity, up.day, id, up.pub, up.sig); err == nil {
		s.ledger.Add([]ledger.Entry{e}, "")
	}
	w.WriteHeader(http.StatusCreated)
}

// reserve marks an upload in progress (counted against the allowance until
// it is in the ledger); m.mu held.
func (m *mediaStore) reserve(acct, id string, on bool) {
	if m.pending[acct] == nil {
		m.pending[acct] = map[string]bool{}
	}
	if on {
		m.pending[acct][id] = true
	} else if delete(m.pending[acct], id); len(m.pending[acct]) == 0 {
		delete(m.pending, acct)
	}
}

type acctState struct {
	age  time.Duration // since the network first saw the account
	used int           // files today: ledger + uploads in progress here
}

// accountState: s.media.mu held.
func (s *Server) accountState(acct string, day uint32) acctState {
	var st acctState
	if t, ok := s.ledger.FirstSeen(acct); ok {
		st.age = max(time.Since(t), 0)
	}
	st.used = s.ledger.Used(acct, day) + len(s.media.pending[acct])
	return st
}

// loadBits: 0–2 more bits of work when the store fills up.
func (s *Server) loadBits() int {
	capB := s.cfg.Media.storeBytes()
	switch used := s.media.used; {
	case used*5 >= capB*4:
		return 2
	case used*2 >= capB:
		return 1
	}
	return 0
}

type signedReq struct {
	pub  identity.Public
	hash string
	day  uint32
	sig  []byte
}

// mediaSigned checks an account signature (X-Account, X-Day, X-Signature
// over body(day)). The day must be the relay's today, or today ± 1 (clock
// skew) unless strict.
func (s *Server) mediaSigned(w http.ResponseWriter, r *http.Request, domain string, body func(uint32) []byte, strict bool) (signedReq, bool) {
	today := wire.Day(time.Now())
	pk, err1 := hex.DecodeString(r.Header.Get(wire.HeaderAccount))
	sig, err2 := hex.DecodeString(r.Header.Get(wire.HeaderSignature))
	d, err3 := strconv.ParseUint(r.Header.Get(wire.HeaderDay), 10, 32)
	pub, err4 := identity.PublicFromBytes(pk)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		http.Error(w, "signed request required (X-Account, X-Day, X-Signature)", http.StatusUnauthorized)
		return signedReq{}, false
	}
	day := uint32(d)
	skew := uint32(1)
	if strict {
		skew = 0
	}
	if day+skew < today || day > today+skew || !pub.Verify(domain, body(day), sig) {
		http.Error(w, fmt.Sprintf("bad signature or day (relay's day is %d)", today), http.StatusUnauthorized)
		return signedReq{}, false
	}
	return signedReq{pub, pub.Hash().String(), day, sig}, true
}

// mediaQuota tells an account its allowance today and the work the next
// upload needs (signed, so only the account itself asks).
func (s *Server) mediaQuota(w http.ResponseWriter, r *http.Request) {
	up, ok := s.mediaSigned(w, r, wire.DomainMediaQuota, wire.QuotaSigBody, false)
	if !ok {
		return
	}
	day := wire.Day(time.Now())
	p := s.MediaPolicy()
	s.media.mu.Lock()
	st := s.accountState(up.hash, day)
	load := s.loadBits()
	s.media.mu.Unlock()
	q := wire.MediaQuota{
		PerDay: p.Allowance(st.age), Used: st.used, Resets: int64(day+1) * 86400, Allowed: s.cfg.Media.allowed(up.hash),
		PowBits: p.Bits(st.age, st.used, load), AgeDays: int(st.age.Hours() / 24),
	}
	if q.Allowed {
		q.Left = max(q.PerDay-q.Used, 0)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(q)
}

func (s *Server) mediaGet(w http.ResponseWriter, r *http.Request) {
	m := s.media
	id, ok := mediaID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	mm, ok := m.meta[id]
	var size int64
	if ok {
		size = mm.Size
	}
	m.mu.Unlock()
	if !ok {
		http.Error(w, "no such file (fetched by everyone, expired, or not uploaded yet)", http.StatusNotFound)
		return
	}
	f, err := os.Open(m.blob(id))
	if err != nil {
		http.Error(w, "gone", http.StatusNotFound)
		return
	}
	defer f.Close()
	var from int64
	if rg := r.Header.Get("Range"); rg != "" {
		v, ok := strings.CutPrefix(rg, "bytes=")
		v, _ = strings.CutSuffix(v, "-")
		if from, err = strconv.ParseInt(v, 10, 64); !ok || err != nil || from < 0 || from >= size {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			http.Error(w, "bad range (only bytes=N- is supported)", http.StatusRequestedRangeNotSatisfiable)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size-from, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	if from > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, size-1, size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead {
		return
	}
	f.Seek(from, io.SeekStart)
	n, _ := io.Copy(w, f)
	if from+n != size {
		return // cut off: the reader resumes later
	}
	// Downloaded to the end: once every reader has it, it is gone.
	m.mu.Lock()
	defer m.mu.Unlock()
	if mm, ok := m.meta[id]; ok {
		mm.Served++
		if mm.Served >= mm.Readers {
			m.remove(id)
		} else {
			m.save(id, mm)
		}
	}
}

// MediaFile describes a stored file, for the admin panel.
type MediaFile struct {
	ID      string    `json:"id"`
	Size    int64     `json:"size"`
	Readers int       `json:"readers"`
	Served  int       `json:"served"`
	Expires time.Time `json:"expires"`
}

// MediaFiles lists stored files, oldest expiry first; on reports media is on.
func (s *Server) MediaFiles() (files []MediaFile, used, capBytes int64, on bool) {
	m := s.media
	if m == nil {
		return nil, 0, 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mm := range m.meta {
		files = append(files, MediaFile{id, mm.Size, mm.Readers, mm.Served, mm.Expires})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Expires.Before(files[j].Expires) })
	return files, m.used, s.cfg.Media.storeBytes(), true
}

// DeleteMedia removes a stored file now.
func (s *Server) DeleteMedia(id string) bool {
	m := s.media
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.meta[id]; !ok {
		return false
	}
	m.remove(id)
	return true
}

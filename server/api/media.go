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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/pow"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Media: a relay may hold encrypted files (core/media) for a short time.
//
//   - PUT /v1/media/{id}: body = the blob, X-PoW over wire.MediaPoWBody(id),
//     X-Readers = how many downloads to expect (one per recipient).
//   - GET /v1/media/{id}: the blob; "Range: bytes=N-" resumes.
//
// A file is deleted as soon as it was downloaded to the end X-Readers times,
// or when it expires: 8 hours (MaxHours) for files up to 1 MB, less for
// bigger ones (never under MinHours). The store has a total cap. Media is
// not copied between relays; packet sync between relays is unchanged.
type MediaConfig struct {
	Dir      string // where blobs live
	MaxMB    int    // largest file; 0 = media off
	StoreMB  int    // total cap (default 2048)
	MaxHours int    // keep time for files up to 1 MB (default 8)
	MinHours int    // shortest keep time, for big files (default 1)
	// Accounts, if set, are the only accounts (identity hashes, hex) that
	// may upload; uploads are then signed. Anyone may download (files are
	// encrypted and fetched by their unguessable id).
	Accounts []string
}

func (c MediaConfig) allowed(h string) bool {
	for _, a := range c.Accounts {
		if strings.EqualFold(strings.TrimSpace(a), h) {
			return true
		}
	}
	return false
}

func (c MediaConfig) maxHours() int {
	if c.MaxMB <= 0 {
		return 0
	}
	if c.MaxHours <= 0 {
		return 8
	}
	return c.MaxHours
}

// TTL is how long a file of size bytes waits for its readers.
func (c MediaConfig) TTL(size int64) time.Duration {
	maxT := time.Duration(c.maxHours()) * time.Hour
	minT := time.Duration(max(c.MinHours, 1)) * time.Hour
	if minT > maxT {
		minT = maxT
	}
	t := maxT
	if size > 1<<20 {
		t = time.Duration(float64(maxT) * float64(1<<20) / float64(size))
	}
	return max(t, minT)
}

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
	m := &mediaStore{dir: dir, meta: map[string]*mediaMeta{}}
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
	if s.cfg.PowBits > 0 {
		n, err := strconv.ParseUint(r.Header.Get(wire.HeaderPoW), 10, 64)
		if err != nil || !pow.Check(wire.MediaPoWBody(id), n, s.cfg.PowBits) {
			http.Error(w, "proof of work required", http.StatusPaymentRequired)
			return
		}
	}
	if len(s.cfg.Media.Accounts) > 0 {
		pk, err1 := hex.DecodeString(r.Header.Get(wire.HeaderAccount))
		sig, err2 := hex.DecodeString(r.Header.Get(wire.HeaderSignature))
		pub, err3 := identity.PublicFromBytes(pk)
		if err1 != nil || err2 != nil || err3 != nil || !pub.Verify(wire.DomainMediaUpload, []byte(id), sig) {
			http.Error(w, "this relay takes media only from its accounts: signed upload required", http.StatusUnauthorized)
			return
		}
		if !s.cfg.Media.allowed(pub.Hash().String()) {
			http.Error(w, "this account may not upload media to this relay", http.StatusForbidden)
			return
		}
	}
	readers, _ := strconv.Atoi(r.Header.Get(wire.HeaderReaders))
	readers = min(max(readers, 1), 64)
	limit := int64(s.cfg.Media.MaxMB)<<20 + int64(s.cfg.Media.MaxMB)*16*16 + 16 // + per-chunk tags
	if r.ContentLength > limit {
		http.Error(w, "file too large for this relay", http.StatusRequestEntityTooLarge)
		return
	}
	m.mu.Lock()
	if _, ok := m.meta[id]; ok {
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	capB := int64(s.cfg.Media.StoreMB) << 20
	if capB <= 0 {
		capB = 2048 << 20
	}
	if r.ContentLength > 0 && m.used+r.ContentLength > capB {
		m.mu.Unlock()
		http.Error(w, "relay media store is full", http.StatusInsufficientStorage)
		return
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
	mm := &mediaMeta{Size: n, Readers: readers, Expires: time.Now().Add(s.cfg.Media.TTL(n))}
	m.meta[id] = mm
	m.used += n
	m.save(id, mm)
	w.WriteHeader(http.StatusCreated)
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

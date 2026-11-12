package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/ledger"
)

// Usage ledger (docs/server.md §6a): anyone may read it, any relay may push
// entries. Every entry is checked on arrival (account and relay signatures),
// so pushing needs no trust.
//
//	POST /v1/usage          body = entries (ledger.EntrySize each); answer = new count
//	GET  /v1/usage?since=N  entries after cursor N; X-Cursor, X-More

const usagePage = 1000

func (s *Server) usagePush(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, usagePage*ledger.EntrySize))
	if err != nil {
		http.Error(w, "too many entries (1000 per request)", http.StatusRequestEntityTooLarge)
		return
	}
	es, err := ledger.DecodeAll(b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	source := r.Header.Get(wire.HeaderOrigin)
	if source == "" {
		source = clientIP(r)
	}
	n, err := s.ledger.Add(es, source)
	if err != nil {
		http.Error(w, "ledger", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(strconv.Itoa(n)))
}

func (s *Server) usagePull(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	es, next, more, err := s.ledger.Since(since, usagePage)
	if err != nil {
		http.Error(w, "ledger", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", wire.ContentType)
	w.Header().Set(wire.HeaderCursor, strconv.FormatInt(next, 10))
	if more {
		w.Header().Set(wire.HeaderMore, "1")
	}
	for _, e := range es {
		w.Write(e.Encode())
	}
}

// Ledger returns the relay's ledger (admin panel, sync loop).
func (s *Server) Ledger() *ledger.Ledger { return s.ledger }

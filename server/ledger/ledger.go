// Package ledger is the relays' shared record of important-media uploads
// (docs/server.md §6a "Usage ledger"), and the first time each account was
// seen (its age).
//
// Every entry carries the uploading account's own signature over its upload
// and the accepting relay's signature, so any relay can check every entry and
// nobody can invent uploads for someone else. Entries only add up: copies
// that met each other converge (a grow-only set), deduplicated per (account,
// file). A day's entries are dropped after two days.
package ledger

import (
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// DomainUsage is what the accepting relay signs an entry with.
const DomainUsage = "MESHMSG-USAGE"

const (
	pubLen = 64
	sigLen = 64
	// EntrySize is the encoded size of an entry:
	// day(4) | media_id(16) | account_pubkey(64) | account_sig(64) | relay_pubkey(64) | relay_sig(64)
	EntrySize = 4 + 16 + pubLen + sigLen + pubLen + sigLen
	signedLen = 4 + 16 + pubLen + sigLen // what the relay signs
)

// Entry is one important upload.
type Entry struct {
	Day        uint32
	Media      [16]byte
	Account    []byte // public key, 64 bytes
	AccountSig []byte // the upload's X-Signature
	Relay      []byte // public key of the relay that accepted it
	RelaySig   []byte
}

// Encode returns the entry's wire form (EntrySize bytes).
func (e Entry) Encode() []byte {
	b := make([]byte, 0, EntrySize)
	b = binary.BigEndian.AppendUint32(b, e.Day)
	b = append(b, e.Media[:]...)
	b = append(b, e.Account...)
	b = append(b, e.AccountSig...)
	b = append(b, e.Relay...)
	return append(b, e.RelaySig...)
}

// Decode parses one entry.
func Decode(b []byte) (Entry, error) {
	if len(b) != EntrySize {
		return Entry{}, errors.New("ledger: bad entry size")
	}
	var e Entry
	e.Day = binary.BigEndian.Uint32(b)
	copy(e.Media[:], b[4:20])
	o := 20
	e.Account = append([]byte(nil), b[o:o+pubLen]...)
	o += pubLen
	e.AccountSig = append([]byte(nil), b[o:o+sigLen]...)
	o += sigLen
	e.Relay = append([]byte(nil), b[o:o+pubLen]...)
	o += pubLen
	e.RelaySig = append([]byte(nil), b[o:o+sigLen]...)
	return e, nil
}

// DecodeAll splits a body of concatenated entries.
func DecodeAll(b []byte) ([]Entry, error) {
	if len(b)%EntrySize != 0 {
		return nil, errors.New("ledger: body is not a whole number of entries")
	}
	out := make([]Entry, 0, len(b)/EntrySize)
	for i := 0; i < len(b); i += EntrySize {
		e, _ := Decode(b[i : i+EntrySize])
		out = append(out, e)
	}
	return out, nil
}

// MediaHex is the file id as the upload signature covers it.
func (e Entry) MediaHex() string { return hex.EncodeToString(e.Media[:]) }

// New makes the entry for an upload a relay accepted, signed by the relay.
func New(relay *identity.Identity, day uint32, mediaHex string, account identity.Public, accountSig []byte) (Entry, error) {
	id, err := hex.DecodeString(mediaHex)
	if err != nil || len(id) != 16 || len(accountSig) != sigLen {
		return Entry{}, errors.New("ledger: bad upload")
	}
	e := Entry{Day: day, Account: account.Bytes(), AccountSig: accountSig, Relay: relay.Public.Bytes()}
	copy(e.Media[:], id)
	e.RelaySig = relay.SignDomain(DomainUsage, e.Encode()[:signedLen])
	return e, nil
}

// Errors from Verify: which signature failed tells who misbehaved.
var (
	ErrRelaySig   = errors.New("ledger: bad relay signature")
	ErrAccountSig = errors.New("ledger: bad account signature") // the relay signed a forged upload
)

// Verify checks both signatures and returns the account's public key.
func (e Entry) Verify() (identity.Public, error) {
	rp, err := identity.PublicFromBytes(e.Relay)
	if err != nil || !rp.Verify(DomainUsage, e.Encode()[:signedLen], e.RelaySig) {
		return identity.Public{}, ErrRelaySig
	}
	ap, err := identity.PublicFromBytes(e.Account)
	if err != nil || !ap.Verify(wire.DomainMediaUpload, wire.MediaSigBody(e.MediaHex(), e.Day), e.AccountSig) {
		return identity.Public{}, ErrAccountSig
	}
	return ap, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	seq       INTEGER PRIMARY KEY AUTOINCREMENT,
	day       INTEGER NOT NULL,
	media     BLOB NOT NULL,
	account   BLOB NOT NULL,
	acct_hash TEXT NOT NULL,
	raw       BLOB NOT NULL,
	UNIQUE(account, media)
);
CREATE INDEX IF NOT EXISTS entries_acct ON entries(acct_hash, day);
CREATE TABLE IF NOT EXISTS accounts (
	hash       TEXT PRIMARY KEY,
	first_seen INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sources (
	key     TEXT PRIMARY KEY,   -- "relay:<pubkey hex>" or "peer:<label>"
	valid   INTEGER NOT NULL DEFAULT 0,
	invalid INTEGER NOT NULL DEFAULT 0,
	last    INTEGER NOT NULL DEFAULT 0,
	blocked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cursors (
	peer TEXT PRIMARY KEY,
	push INTEGER NOT NULL DEFAULT 0,
	pull INTEGER NOT NULL DEFAULT 0
);`

// Ledger is a relay's copy.
type Ledger struct {
	db  *sql.DB
	Now func() time.Time
}

// Open opens (or creates) the ledger at path (":memory:" for tests).
func Open(path string) (*Ledger, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		dsn = "file::memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: schema: %w", err)
	}
	return &Ledger{db: db, Now: time.Now}, nil
}

func (l *Ledger) Close() error { return l.db.Close() }

func (l *Ledger) today() uint32 { return wire.Day(l.Now()) }

// Add stores verified entries that came from source (a peer label, or ""
// for this relay's own uploads) and returns how many were new. Entries
// signed by a relay that once signed a forged upload are ignored; an entry
// with a bad relay signature counts against the source.
func (l *Ledger) Add(entries []Entry, source string) (added int, err error) {
	today := l.today()
	for _, e := range entries {
		if e.Day+2 < today || e.Day > today+1 {
			continue // too old to matter, or from the future
		}
		relayKey := "relay:" + hex.EncodeToString(e.Relay)
		if l.blocked(relayKey) {
			continue
		}
		ap, verr := e.Verify()
		switch {
		case errors.Is(verr, ErrRelaySig):
			l.note("peer:"+source, false)
			continue
		case errors.Is(verr, ErrAccountSig):
			l.note(relayKey, false)
			l.block(relayKey) // signed an upload the account never made
			continue
		}
		res, err := l.db.Exec(`INSERT OR IGNORE INTO entries(day,media,account,acct_hash,raw) VALUES(?,?,?,?,?)`,
			e.Day, e.Media[:], e.Account, ap.Hash().String(), e.Encode())
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
		l.note(relayKey, true)
		if source != "" {
			l.note("peer:"+source, true)
		}
	}
	return added, nil
}

func (l *Ledger) note(key string, ok bool) {
	col := "valid"
	if !ok {
		col = "invalid"
	}
	l.db.Exec(`INSERT INTO sources(key,`+col+`,last) VALUES(?,1,?) ON CONFLICT(key) DO UPDATE SET `+col+`=`+col+`+1, last=excluded.last`,
		key, l.Now().Unix())
}

func (l *Ledger) block(key string) { l.db.Exec(`UPDATE sources SET blocked=1 WHERE key=?`, key) }

func (l *Ledger) blocked(key string) bool {
	var b int
	l.db.QueryRow(`SELECT blocked FROM sources WHERE key=?`, key).Scan(&b)
	return b == 1
}

// Has tells whether an upload of this file by this account is recorded.
func (l *Ledger) Has(account []byte, media [16]byte) bool {
	var n int
	l.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE account=? AND media=?`, account, media[:]).Scan(&n)
	return n > 0
}

// Used is how many important files an account uploaded on day, on every
// relay this ledger heard of.
func (l *Ledger) Used(acctHash string, day uint32) int {
	var n int
	l.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE acct_hash=? AND day=?`, acctHash, day).Scan(&n)
	return n
}

// Since returns entries after cursor (at most limit), the new cursor and
// whether more are waiting.
func (l *Ledger) Since(cursor int64, limit int) (out []Entry, next int64, more bool, err error) {
	rows, err := l.db.Query(`SELECT seq, raw FROM entries WHERE seq > ? ORDER BY seq LIMIT ?`, cursor, limit+1)
	if err != nil {
		return nil, cursor, false, err
	}
	defer rows.Close()
	next = cursor
	for rows.Next() {
		var seq int64
		var raw []byte
		if err := rows.Scan(&seq, &raw); err != nil {
			return nil, cursor, false, err
		}
		if len(out) == limit {
			more = true
			break
		}
		e, err := Decode(raw)
		if err != nil {
			continue
		}
		out = append(out, e)
		next = seq
	}
	return out, next, more, rows.Err()
}

// Expire drops days older than yesterday.
func (l *Ledger) Expire() {
	l.db.Exec(`DELETE FROM entries WHERE day + 1 < ?`, l.today())
}

// Seen records that an account was seen at t (its announce); only the
// first time is kept.
func (l *Ledger) Seen(acctHash string, t time.Time) {
	l.db.Exec(`INSERT INTO accounts(hash, first_seen) VALUES(?,?) ON CONFLICT(hash) DO UPDATE SET first_seen=MIN(first_seen, excluded.first_seen)`,
		acctHash, t.Unix())
}

// FirstSeen is when the account was first seen (ok false: never).
func (l *Ledger) FirstSeen(acctHash string) (time.Time, bool) {
	var ts int64
	if err := l.db.QueryRow(`SELECT first_seen FROM accounts WHERE hash=?`, acctHash).Scan(&ts); err != nil {
		return time.Time{}, false
	}
	return time.Unix(ts, 0), true
}

// Cursors returns the push and pull cursors for a peer.
func (l *Ledger) Cursors(peer string) (push, pull int64) {
	l.db.QueryRow(`SELECT push, pull FROM cursors WHERE peer=?`, peer).Scan(&push, &pull)
	return
}

// SetCursors stores a peer's cursors.
func (l *Ledger) SetCursors(peer string, push, pull int64) {
	l.db.Exec(`INSERT INTO cursors(peer,push,pull) VALUES(?,?,?) ON CONFLICT(peer) DO UPDATE SET push=excluded.push, pull=excluded.pull`,
		peer, push, pull)
}

// Source is a relay or peer that sent entries, for the admin panel.
type Source struct {
	Key     string    `json:"key"`
	Valid   int       `json:"valid"`
	Invalid int       `json:"invalid"`
	Last    time.Time `json:"last"`
	Blocked bool      `json:"blocked"`
}

// Stats summarises the ledger for the admin panel.
type Stats struct {
	Entries   int      `json:"entries"`
	Today     int      `json:"today"`
	Accounts  int      `json:"accounts_today"`
	Known     int      `json:"accounts_known"` // accounts with a first-seen time
	Sources   []Source `json:"sources"`
	BadSource int      `json:"bad_sources"`
}

// Stats reports the ledger's state.
func (l *Ledger) Stats() Stats {
	var s Stats
	today := l.today()
	l.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&s.Entries)
	l.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT acct_hash) FROM entries WHERE day=?`, today).Scan(&s.Today, &s.Accounts)
	l.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&s.Known)
	rows, err := l.db.Query(`SELECT key, valid, invalid, last, blocked FROM sources ORDER BY invalid DESC, last DESC LIMIT 50`)
	if err != nil {
		return s
	}
	defer rows.Close()
	for rows.Next() {
		var src Source
		var last int64
		rows.Scan(&src.Key, &src.Valid, &src.Invalid, &last, &src.Blocked)
		src.Last = time.Unix(last, 0)
		if src.Invalid > 0 {
			s.BadSource++
		}
		s.Sources = append(s.Sources, src)
	}
	return s
}

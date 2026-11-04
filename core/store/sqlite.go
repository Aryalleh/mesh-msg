package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go, no cgo: cross-compiles to every target

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// SQLite is a persistent Store.
type SQLite struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS items (
	seq      INTEGER PRIMARY KEY AUTOINCREMENT,
	pid      BLOB    NOT NULL,
	frag     INTEGER NOT NULL,
	type     INTEGER NOT NULL,
	dest     BLOB    NOT NULL,
	raw      BLOB    NOT NULL,
	origin   TEXT    NOT NULL DEFAULT '',
	region   TEXT    NOT NULL DEFAULT '',
	received INTEGER NOT NULL,
	expires  INTEGER NOT NULL,
	UNIQUE(pid, frag)
);
CREATE INDEX IF NOT EXISTS items_dest ON items(dest, type, seq);
CREATE INDEX IF NOT EXISTS items_expires ON items(expires);
`

// OpenSQLite opens (or creates) a store at path. Use ":memory:" for tests.
func OpenSQLite(path string) (*SQLite, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		dsn = "file::memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialise writers; plenty for our volumes
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromUnix(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (s *SQLite) Put(it Item) (int64, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var seq int64
	err = tx.QueryRow(`SELECT seq FROM items WHERE pid=? AND frag=?`, it.Key.ID[:], it.Key.Frag).Scan(&seq)
	if err == nil {
		return seq, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if it.Type == packet.TypeAnnounce {
		if _, err := tx.Exec(`DELETE FROM items WHERE type=? AND dest=?`, packet.TypeAnnounce, it.Dest[:]); err != nil {
			return 0, false, err
		}
	}
	res, err := tx.Exec(`INSERT INTO items(pid,frag,type,dest,raw,origin,region,received,expires) VALUES(?,?,?,?,?,?,?,?,?)`,
		it.Key.ID[:], it.Key.Frag, it.Type, it.Dest[:], it.Raw, it.Origin, it.Region, unix(it.Received), unix(it.Expires))
	if err != nil {
		return 0, false, err
	}
	if seq, err = res.LastInsertId(); err != nil {
		return 0, false, err
	}
	return seq, true, tx.Commit()
}

func (s *SQLite) Has(k packet.Key) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM items WHERE pid=? AND frag=?`, k.ID[:], k.Frag).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

const cols = `seq,pid,frag,type,dest,raw,origin,region,received,expires`

func scanItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var pid, dest []byte
		var recv, exp int64
		if err := rows.Scan(&it.Seq, &pid, &it.Key.Frag, &it.Type, &dest, &it.Raw, &it.Origin, &it.Region, &recv, &exp); err != nil {
			return nil, err
		}
		copy(it.Key.ID[:], pid)
		copy(it.Dest[:], dest)
		it.Received, it.Expires = fromUnix(recv), fromUnix(exp)
		out = append(out, it)
	}
	return out, rows.Err()
}

func lim(limit int) int {
	if limit <= 0 {
		return -1
	}
	return limit
}

func (s *SQLite) Since(after int64, limit int, f Filter) ([]Item, error) {
	q := `SELECT ` + cols + ` FROM items WHERE seq > ?`
	args := []any{after}
	if f.ExcludeOrigin != "" {
		q += ` AND origin != ?`
		args = append(args, f.ExcludeOrigin)
	}
	if len(f.Types) > 0 {
		q += ` AND type IN (?` + strings.Repeat(",?", len(f.Types)-1) + `)`
		for _, t := range f.Types {
			args = append(args, t)
		}
	}
	q += ` ORDER BY seq LIMIT ?`
	rows, err := s.db.Query(q, append(args, lim(limit))...)
	if err != nil {
		return nil, err
	}
	return scanItems(rows)
}

func (s *SQLite) ByDest(dest packet.Hash, after int64, limit int) ([]Item, error) {
	rows, err := s.db.Query(`SELECT `+cols+` FROM items WHERE dest=? AND type=? AND seq > ? ORDER BY seq LIMIT ?`,
		dest[:], packet.TypeData, after, lim(limit))
	if err != nil {
		return nil, err
	}
	return scanItems(rows)
}

func (s *SQLite) LatestAnnounce(dest packet.Hash) (Item, bool, error) {
	rows, err := s.db.Query(`SELECT `+cols+` FROM items WHERE dest=? AND type=? ORDER BY seq DESC LIMIT 1`, dest[:], packet.TypeAnnounce)
	if err != nil {
		return Item{}, false, err
	}
	items, err := scanItems(rows)
	if err != nil || len(items) == 0 {
		return Item{}, false, err
	}
	return items[0], true, nil
}

func (s *SQLite) DeleteData(id packet.ID) (int, error) {
	res, err := s.db.Exec(`DELETE FROM items WHERE pid=? AND type=?`, id[:], packet.TypeData)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *SQLite) Expire(now time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM items WHERE expires != 0 AND expires < ?`, unix(now))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *SQLite) MaxSeq() (int64, error) {
	// sqlite_sequence keeps the high-water mark even after deletes.
	var seq sql.NullInt64
	err := s.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='items'`).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq.Int64, err
}

func (s *SQLite) Count() (int, error) {
	var n int
	return n, s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n)
}

func (s *SQLite) Close() error { return s.db.Close() }

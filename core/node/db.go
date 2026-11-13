package node

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// appDB holds user-facing state: contacts, message history, small settings.
type appDB struct{ db *sql.DB }

const appSchema = `
CREATE TABLE IF NOT EXISTS contacts (
	hash  BLOB PRIMARY KEY,
	pub   BLOB NOT NULL,
	name  TEXT NOT NULL DEFAULT '',
	added INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id        TEXT PRIMARY KEY,   -- packet id (hex)
	peer      BLOB NOT NULL,      -- contact hash
	outgoing  INTEGER NOT NULL,
	body      TEXT NOT NULL,
	sent      INTEGER NOT NULL,
	received  INTEGER NOT NULL,
	delivered INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_peer ON messages(peer, received);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

func openAppDB(path string) (*appDB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(appSchema); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(relaysSchema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateGroups(db); err != nil {
		db.Close()
		return nil, err
	}
	// v2: group chats. Older databases get the new message columns.
	if _, err := db.Exec(mediaSchema); err != nil {
		db.Close()
		return nil, err
	}
	// v3: important media (protocol.md §6b).
	var imp int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('media') WHERE name='important'`).Scan(&imp)
	if imp == 0 {
		if _, err := db.Exec(`ALTER TABLE media ADD COLUMN important INTEGER NOT NULL DEFAULT 0`); err != nil {
			db.Close()
			return nil, err
		}
	}
	for _, col := range []string{"sender BLOB", "sender_name TEXT NOT NULL DEFAULT ''", "media TEXT NOT NULL DEFAULT ''", "seen INTEGER NOT NULL DEFAULT 0"} {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name=?`, strings.Fields(col)[0]).Scan(&n)
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN ` + col); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	return &appDB{db}, nil
}

// Contact is a saved contact.
type Contact struct {
	Hash  string    `json:"hash"`
	Name  string    `json:"name"`
	URI   string    `json:"uri"`
	Added time.Time `json:"added"`
	pub   identity.Public
}

func (d *appDB) putContact(pub identity.Public, name string, now time.Time) error {
	h := pub.Hash()
	_, err := d.db.Exec(`INSERT INTO contacts(hash,pub,name,added) VALUES(?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=CASE WHEN excluded.name != '' THEN excluded.name ELSE contacts.name END`,
		h[:], pub.Bytes(), name, now.Unix())
	return err
}

func (d *appDB) contacts() ([]Contact, error) {
	rows, err := d.db.Query(`SELECT pub,name,added FROM contacts ORDER BY name, added`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var pb []byte
		var c Contact
		var added int64
		if err := rows.Scan(&pb, &c.Name, &added); err != nil {
			return nil, err
		}
		if c.pub, err = identity.PublicFromBytes(pb); err != nil {
			continue
		}
		c.Hash = c.pub.Hash().String()
		c.URI = identity.Contact{Public: c.pub, Name: c.Name}.URI()
		c.Added = time.Unix(added, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *appDB) deleteContact(h packet.Hash) error {
	_, err := d.db.Exec(`DELETE FROM contacts WHERE hash=?`, h[:])
	return err
}

// Message is one chat message.
type Message struct {
	ID        string    `json:"id"`
	Peer      string    `json:"peer"`
	Outgoing  bool      `json:"outgoing"`
	Text      string    `json:"text"`
	Sent      time.Time `json:"sent"`
	Received  time.Time `json:"received"`
	Delivered bool      `json:"delivered"`
	// Read: the contact has seen it (read receipt, over relays only).
	Read bool `json:"read,omitempty"`
	// Group messages: Peer is the group's hash, From the sender's hash and
	// FromName the name the sender gave (a contact's saved name wins).
	Group    bool   `json:"group,omitempty"`
	From     string `json:"from,omitempty"`
	FromName string `json:"from_name,omitempty"`
	// Outgoing private-group messages: how many members it went to and how
	// many confirmed it (Delivered = all of them).
	Recipients  int `json:"recipients,omitempty"`
	DeliveredTo int `json:"delivered_to,omitempty"`
	// A photo, file, voice or video message (Text is its caption).
	Media *MediaInfo `json:"media,omitempty"`
}

func (d *appDB) putMessage(m Message) error {
	peer, err := packet.ParseHash(m.Peer)
	if err != nil {
		return err
	}
	var sender []byte
	if m.From != "" {
		h, err := packet.ParseHash(m.From)
		if err != nil {
			return err
		}
		sender = h[:]
	}
	media := ""
	if m.Media != nil {
		media = m.Media.ID
	}
	_, err = d.db.Exec(`INSERT OR IGNORE INTO messages(id,peer,outgoing,body,sent,received,delivered,sender,sender_name,media) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, peer[:], m.Outgoing, m.Text, m.Sent.Unix(), m.Received.UnixMilli(), m.Delivered, sender, m.FromName, media)
	return err
}

func (d *appDB) markDelivered(id string) (bool, error) {
	res, err := d.db.Exec(`UPDATE messages SET delivered=1 WHERE id=? AND outgoing=1 AND delivered=0`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (d *appDB) messages(peer *packet.Hash, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id,peer,outgoing,body,sent,received,delivered,sender,sender_name,media,seen FROM messages`
	var args []any
	if peer != nil {
		q += ` WHERE peer=?`
		args = append(args, peer[:])
	}
	q += ` ORDER BY received DESC LIMIT ?`
	rows, err := d.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var pb []byte
		var sent, recv int64
		var sender []byte
		var media string
		if err := rows.Scan(&m.ID, &pb, &m.Outgoing, &m.Text, &sent, &recv, &m.Delivered, &sender, &m.FromName, &media, &m.Read); err != nil {
			return nil, err
		}
		if media != "" {
			m.Media = &MediaInfo{ID: media}
		}
		if len(sender) == len(packet.Hash{}) {
			m.Group, m.From = true, hex.EncodeToString(sender)
		}
		var h packet.Hash
		copy(h[:], pb)
		m.Peer = h.String()
		m.Sent, m.Received = time.Unix(sent, 0), time.UnixMilli(recv)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // before more queries on a single-connection pool
	for i := range out {
		if out[i].Media != nil {
			if mi, ok := d.mediaInfo(out[i].Media.ID); ok {
				out[i].Media = &mi
			}
		}
	}
	// oldest first for display
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func (d *appDB) get(k string) (string, bool) {
	var v string
	err := d.db.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return "", false
	}
	return v, true
}

func (d *appDB) set(k, v string) error {
	_, err := d.db.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func (d *appDB) del(k string) error {
	_, err := d.db.Exec(`DELETE FROM kv WHERE k=?`, k)
	return err
}

func (d *appDB) close() error { return d.db.Close() }

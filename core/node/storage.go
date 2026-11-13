package node

import (
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"
)

// Storage: what files take space on this device, per chat, and deleting
// them by chat, time range and kind (or any mix). A deleted file's message
// stays in the chat ("deleted"); the file is not downloaded or served again.

// StoredFile is one file kept on this device.
type StoredFile struct {
	ID       string    `json:"id"`
	Chat     string    `json:"chat"` // contact or group hash
	Kind     string    `json:"kind"` // image | audio | video | round | file
	Name     string    `json:"name"`
	Mime     string    `json:"mime"`
	Bytes    int64     `json:"bytes"` // on disk
	Created  time.Time `json:"created"`
	Outgoing bool      `json:"outgoing"`
}

// ChatStorage is one chat's total.
type ChatStorage struct {
	Chat   string           `json:"chat"`
	Files  int              `json:"files"`
	Bytes  int64            `json:"bytes"`
	ByKind map[string]int64 `json:"by_kind"`
}

// StorageFilter picks files. Empty fields match everything; times are
// inclusive (zero: open).
type StorageFilter struct {
	Chats []string  `json:"chats,omitempty"`
	Kinds []string  `json:"kinds,omitempty"`
	From  time.Time `json:"from,omitempty"`
	To    time.Time `json:"to,omitempty"`
	IDs   []string  `json:"ids,omitempty"`
}

// StorageUsage is the total and the per-chat use, biggest first.
type StorageUsage struct {
	Bytes int64         `json:"bytes"`
	Files int           `json:"files"`
	Chats []ChatStorage `json:"chats"`
}

// StoredFiles lists the files on this device that match f, newest first.
func (n *Node) StoredFiles(f StorageFilter) ([]StoredFile, error) {
	rows, err := n.db.db.Query(`SELECT m.id, m.mime, m.name, m.created, COALESCE(g.peer, x''), COALESCE(g.outgoing, 0)
		FROM media m LEFT JOIN (SELECT media, peer, outgoing FROM messages WHERE media<>'' GROUP BY media) g ON g.media = m.id
		WHERE m.state<>'deleted' ORDER BY m.created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := set(f.Chats)
	kinds := set(f.Kinds)
	ids := set(f.IDs)
	var out []StoredFile
	for rows.Next() {
		var s StoredFile
		var created int64
		var peer []byte
		if err := rows.Scan(&s.ID, &s.Mime, &s.Name, &created, &peer, &s.Outgoing); err != nil {
			return nil, err
		}
		s.Created = time.Unix(created, 0)
		s.Chat = hex.EncodeToString(peer)
		s.Kind = MediaInfo{Mime: s.Mime, Name: s.Name}.Kind()
		s.Bytes = fileSize(n.blobPath(s.ID)) + fileSize(n.blobPath(s.ID)+".part")
		if s.Bytes == 0 {
			continue // nothing on disk (not downloaded)
		}
		if (chats != nil && !chats[s.Chat]) || (kinds != nil && !kinds[s.Kind]) || (ids != nil && !ids[s.ID]) {
			continue
		}
		if (!f.From.IsZero() && s.Created.Before(f.From)) || (!f.To.IsZero() && s.Created.After(f.To)) {
			continue
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Storage sums the files on this device per chat.
func (n *Node) Storage() (StorageUsage, error) {
	fs, err := n.StoredFiles(StorageFilter{})
	if err != nil {
		return StorageUsage{}, err
	}
	u := StorageUsage{Chats: []ChatStorage{}}
	idx := map[string]int{}
	for _, f := range fs {
		u.Bytes += f.Bytes
		u.Files++
		i, ok := idx[f.Chat]
		if !ok {
			i = len(u.Chats)
			idx[f.Chat] = i
			u.Chats = append(u.Chats, ChatStorage{Chat: f.Chat, ByKind: map[string]int64{}})
		}
		c := &u.Chats[i]
		c.Files++
		c.Bytes += f.Bytes
		c.ByKind[f.Kind] += f.Bytes
	}
	for i := 1; i < len(u.Chats); i++ {
		for j := i; j > 0 && u.Chats[j].Bytes > u.Chats[j-1].Bytes; j-- {
			u.Chats[j], u.Chats[j-1] = u.Chats[j-1], u.Chats[j]
		}
	}
	return u, nil
}

// DeleteFiles deletes the files that match f from this device. It returns
// how many and the bytes freed. An empty filter is refused (use explicit
// limits to delete everything, e.g. To = now).
func (n *Node) DeleteFiles(f StorageFilter) (int, int64, error) {
	if len(f.Chats) == 0 && len(f.Kinds) == 0 && len(f.IDs) == 0 && f.From.IsZero() && f.To.IsZero() {
		return 0, 0, errors.New("storage: no filter")
	}
	fs, err := n.StoredFiles(f)
	if err != nil {
		return 0, 0, err
	}
	var freed int64
	count := 0
	for _, s := range fs {
		n.mw.mu.Lock()
		busy := n.mw.busy[s.ID]
		n.mw.mu.Unlock()
		if busy {
			continue // downloading right now: next time
		}
		os.Remove(n.blobPath(s.ID))
		os.Remove(n.blobPath(s.ID) + ".part")
		n.db.db.Exec(`UPDATE media SET state='deleted', wanted=0, pending='' WHERE id=?`, s.ID)
		freed += s.Bytes
		count++
		n.emit(Event{Type: "media", Info: s.ID})
	}
	return count, freed, nil
}

func set(xs []string) map[string]bool {
	if len(xs) == 0 {
		return nil
	}
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[strings.ToLower(x)] = true
	}
	return m
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

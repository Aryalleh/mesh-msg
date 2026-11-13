package node

import (
	"errors"
	"strconv"
)

// MergeContact moves the chat with old (a contact who lost that account)
// into the chat with new (their new account): every message, the read
// mark and the folder entries go to new, and old is removed as a contact.
// It returns how many messages moved. Private groups keep old as a member
// until a group owner changes it.
func (n *Node) MergeContact(oldRef, newRef string) (int, error) {
	o, err := n.findContact(oldRef)
	if err != nil {
		return 0, err
	}
	nw, err := n.findContact(newRef)
	if err != nil {
		return 0, err
	}
	if o.Hash == nw.Hash {
		return 0, errors.New("merge: the same contact")
	}
	oh, nh := o.pub.Hash(), nw.pub.Hash()
	// Read before the transaction: the app DB has one connection.
	om, nm := n.readMark(o.Hash), n.readMark(nw.Hash)
	tx, err := n.db.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE messages SET peer=? WHERE peer=? AND sender IS NULL`, nh[:], oh[:])
	if err != nil {
		return 0, err
	}
	moved, _ := res.RowsAffected()
	// Read mark: the later of the two.
	if om > nm {
		if _, err := tx.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, "read:"+nw.Hash, strconv.FormatInt(om, 10)); err != nil {
			return 0, err
		}
	}
	for _, k := range []string{"read:", "rr_sent:", "rr_pending:"} {
		tx.Exec(`DELETE FROM kv WHERE k=?`, k+o.Hash)
	}
	if _, err := tx.Exec(`DELETE FROM contacts WHERE hash=?`, oh[:]); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// Folders: old's place goes to new.
	if fs := n.Folders(); len(fs) > 0 {
		changed := false
		for i := range fs {
			var out []string
			has := false
			for _, c := range fs[i].Chats {
				if c == nw.Hash {
					has = true
				}
			}
			for _, c := range fs[i].Chats {
				switch {
				case c == o.Hash && !has:
					out = append(out, nw.Hash)
					has, changed = true, true
				case c == o.Hash:
					changed = true
				default:
					out = append(out, c)
				}
			}
			fs[i].Chats = out
		}
		if changed {
			n.SetFolders(fs)
		}
	}
	n.emit(Event{Type: "contact", Info: nh.String()})
	return int(moved), nil
}

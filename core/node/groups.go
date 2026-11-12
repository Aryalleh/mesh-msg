package node

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/group"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Group is a joined group chat, as UIs see it: a private group (member list,
// every message sent to each member separately) or a public chat (#global,
// a regional chat).
type Group struct {
	Hash    string        `json:"hash"`
	Name    string        `json:"name"`
	Title   string        `json:"title"` // "#tehran" for public chats
	Kind    group.Kind    `json:"kind"`  // private | regional | global
	URI     string        `json:"uri,omitempty"`
	Joined  time.Time     `json:"joined"`
	Owner   string        `json:"owner,omitempty"`    // private: the owner's hash
	IsOwner bool          `json:"is_owner,omitempty"` // private: we manage the members
	Removed bool          `json:"removed,omitempty"`  // private: the owner removed us
	Members []GroupMember `json:"members,omitempty"`  // private, the owner included

	id      packet.Hash
	g       *group.Group // public chats
	version uint32
}

// GroupMember is one member of a private group.
type GroupMember struct {
	Hash  string `json:"hash"`
	Name  string `json:"name"`
	Owner bool   `json:"owner,omitempty"`
	You   bool   `json:"you,omitempty"`
	pub   identity.Public
}

// Regions is a starter list of regional chats offered by UIs; any other name
// works too (everyone who types the same name meets in one chat).
var Regions = []string{
	"iran", "tehran", "mashhad", "isfahan", "karaj", "shiraz", "tabriz", "qom", "ahvaz",
	"kermanshah", "urmia", "rasht", "zahedan", "kerman", "hamadan", "yazd", "ardabil",
	"bandar-abbas", "arak", "sanandaj", "qazvin", "zanjan", "khorramabad", "gorgan", "sari",
	"bushehr", "birjand", "ilam", "shahrekord", "yasuj", "semnan", "bojnurd",
	"afghanistan", "kabul", "herat", "tajikistan", "iraq", "turkey", "europe", "north-america",
}

const groupSchema = `
CREATE TABLE IF NOT EXISTS groups (
	hash    BLOB PRIMARY KEY,
	name    TEXT NOT NULL,
	kind    TEXT NOT NULL,           -- private | regional | global
	secret  BLOB NOT NULL,           -- public chats; empty for private groups
	joined  INTEGER NOT NULL,
	owner   BLOB,                    -- private: owner's hash (NULL = not known yet)
	version INTEGER NOT NULL DEFAULT 0,
	removed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS group_members (
	grp  BLOB NOT NULL,
	pub  BLOB NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (grp, pub)
);
-- One row per member a private-group message was sent to.
CREATE TABLE IF NOT EXISTS group_sends (
	packet    TEXT PRIMARY KEY,      -- packet id (hex) of the copy for this member
	msg       TEXT NOT NULL,         -- local message id
	member    BLOB NOT NULL,
	delivered INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS group_sends_msg ON group_sends(msg);
`

// migrateGroups adds columns that older development databases lack.
func migrateGroups(db *sql.DB) error {
	if _, err := db.Exec(groupSchema); err != nil {
		return err
	}
	for _, col := range []string{"owner BLOB", "version INTEGER NOT NULL DEFAULT 0", "removed INTEGER NOT NULL DEFAULT 0"} {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('groups') WHERE name=?`, strings.Fields(col)[0]).Scan(&n)
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE groups ADD COLUMN ` + col); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *appDB) putChannel(g *group.Group, now time.Time) error {
	h := g.Hash()
	_, err := d.db.Exec(`INSERT INTO groups(hash,name,kind,secret,joined) VALUES(?,?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name`, h[:], g.Name, string(g.Kind), g.Secret[:], now.Unix())
	return err
}

// putPrivate stores a private group's roster (replacing its members).
func (d *appDB) putPrivate(id packet.Hash, r group.Roster, owner *packet.Hash, removed bool, now time.Time) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ownerB []byte
	if owner != nil {
		ownerB = owner[:]
	}
	if _, err := tx.Exec(`INSERT INTO groups(hash,name,kind,secret,joined,owner,version,removed) VALUES(?,?,?,x'',?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name, owner=excluded.owner, version=excluded.version, removed=excluded.removed`,
		id[:], r.Name, string(group.Private), now.Unix(), ownerB, r.Version, removed); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM group_members WHERE grp=?`, id[:]); err != nil {
		return err
	}
	for _, m := range r.Members {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO group_members(grp,pub,name) VALUES(?,?,?)`, id[:], m.Public.Bytes(), m.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *appDB) groups(me packet.Hash) ([]Group, error) {
	rows, err := d.db.Query(`SELECT hash,name,kind,secret,joined,owner,version,removed FROM groups ORDER BY joined`)
	if err != nil {
		return nil, err
	}
	var out []Group
	for rows.Next() {
		var hash, sec, owner []byte
		var name, kind string
		var joined int64
		var version uint32
		var removed bool
		if err := rows.Scan(&hash, &name, &kind, &sec, &joined, &owner, &version, &removed); err != nil {
			rows.Close()
			return nil, err
		}
		g := Group{Name: name, Kind: group.Kind(kind), Joined: time.Unix(joined, 0), version: version, Removed: removed}
		copy(g.id[:], hash)
		g.Hash = g.id.String()
		if g.Kind == group.Private {
			g.Title = name
			if len(owner) == len(packet.Hash{}) {
				var o packet.Hash
				copy(o[:], owner)
				g.Owner, g.IsOwner = o.String(), o == me
			}
		} else {
			if len(sec) != group.SecretSize {
				continue
			}
			var s [group.SecretSize]byte
			copy(s[:], sec)
			ch, err := group.FromSecret(name, g.Kind, s)
			if err != nil {
				continue
			}
			g.g, g.Title, g.URI = ch, ch.Title(), ch.URI()
		}
		out = append(out, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Kind == group.Private {
			if out[i].Members, err = d.members(out[i].id, me, out[i].Owner); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (d *appDB) members(id packet.Hash, me packet.Hash, owner string) ([]GroupMember, error) {
	rows, err := d.db.Query(`SELECT pub,name FROM group_members WHERE grp=? ORDER BY name`, id[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var pb []byte
		var m GroupMember
		if err := rows.Scan(&pb, &m.Name); err != nil {
			return nil, err
		}
		if m.pub, err = identity.PublicFromBytes(pb); err != nil {
			continue
		}
		h := m.pub.Hash()
		m.Hash, m.You, m.Owner = h.String(), h == me, h.String() == owner
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *appDB) deleteGroup(h packet.Hash) error {
	_, err := d.db.Exec(`DELETE FROM groups WHERE hash=?`, h[:])
	if err == nil {
		_, err = d.db.Exec(`DELETE FROM group_members WHERE grp=?`, h[:])
	}
	return err
}

// ---- public chats ----

// loadGroups joins the stored public chats at startup.
func (n *Node) loadGroups() {
	gs, err := n.db.groups(n.id.Hash())
	if err != nil {
		n.cfg.Logf("node: groups: %v", err)
	}
	for _, g := range gs {
		if g.g != nil {
			n.router.AddGroup(g.g)
		}
		// Members' keys, so their receipts verify (they need not be contacts).
		for _, m := range g.Members {
			n.router.AddKnown(m.pub)
		}
	}
}

// JoinGroup joins a public chat: "global", a region name ("tehran",
// "#tehran") or its link (meshgroup:#tehran).
func (n *Node) JoinGroup(link string) (Group, error) {
	ch, err := group.ParseURI(strings.TrimSpace(link))
	if err != nil && !strings.HasPrefix(strings.TrimSpace(link), "meshgroup:") {
		ch, err = group.Channel(link)
	}
	if err != nil {
		return Group{}, err
	}
	if err := n.db.putChannel(ch, time.Now()); err != nil {
		return Group{}, err
	}
	n.router.AddGroup(ch)
	n.router.Announce() // so relays accept our fetches of the chat's mailbox
	n.emit(Event{Type: "group", Info: ch.Hash().String()})
	return n.findGroup(ch.Hash().String())
}

// ---- private groups ----

// CreateGroup makes a private group owned by us with the given contacts
// (names, hashes or prefixes) and sends them the member list.
func (n *Node) CreateGroup(name string, members []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("a group needs a name")
	}
	id, err := group.NewID(rand.Reader)
	if err != nil {
		return Group{}, err
	}
	me := n.id.Hash()
	r := group.Roster{Version: 1, Name: name, Members: []group.Member{{Public: n.id.Public, Name: n.cfg.Name}}}
	for _, ref := range members {
		c, err := n.findContact(ref)
		if err != nil {
			return Group{}, err
		}
		if !r.Has(c.pub.Hash()) {
			r.Members = append(r.Members, group.Member{Public: c.pub, Name: c.Name})
		}
	}
	if len(r.Members) > group.MaxMembers {
		return Group{}, fmt.Errorf("a group can have at most %d members", group.MaxMembers)
	}
	if err := n.db.putPrivate(id, r, &me, false, time.Now()); err != nil {
		return Group{}, err
	}
	n.sendRoster(id, r, nil)
	n.emit(Event{Type: "group", Info: id.String()})
	return n.findGroup(id.String())
}

// roster rebuilds the stored roster of a private group.
func (g *Group) roster() group.Roster {
	r := group.Roster{Version: g.version, Name: g.Name}
	for _, m := range g.Members {
		r.Members = append(r.Members, group.Member{Public: m.pub, Name: m.Name})
	}
	return r
}

func (n *Node) ownedGroup(ref string) (Group, error) {
	g, err := n.findGroup(ref)
	if err != nil {
		return g, err
	}
	if g.Kind != group.Private {
		return g, errors.New("public chats have no member list")
	}
	if !g.IsOwner {
		return g, errors.New("only the group's owner can change its members")
	}
	return g, nil
}

// AddMembers adds contacts to a private group we own.
func (n *Node) AddMembers(ref string, contacts []string) (Group, error) {
	g, err := n.ownedGroup(ref)
	if err != nil {
		return g, err
	}
	r := g.roster()
	for _, cref := range contacts {
		c, err := n.findContact(cref)
		if err != nil {
			return g, err
		}
		if !r.Has(c.pub.Hash()) {
			r.Members = append(r.Members, group.Member{Public: c.pub, Name: c.Name})
		}
	}
	if len(r.Members) > group.MaxMembers {
		return g, fmt.Errorf("a group can have at most %d members", group.MaxMembers)
	}
	r.Version++
	me := n.id.Hash()
	if err := n.db.putPrivate(g.id, r, &me, false, time.Now()); err != nil {
		return g, err
	}
	n.sendRoster(g.id, r, nil)
	n.emit(Event{Type: "group", Info: g.Hash})
	return n.findGroup(g.Hash)
}

// RemoveMember removes a member (hash, prefix or name) from a group we own.
func (n *Node) RemoveMember(ref, member string) (Group, error) {
	g, err := n.ownedGroup(ref)
	if err != nil {
		return g, err
	}
	var gone *GroupMember
	for i, m := range g.Members {
		if m.Hash == member || strings.EqualFold(m.Name, member) || (len(member) >= 6 && strings.HasPrefix(m.Hash, strings.ToLower(member))) {
			gone = &g.Members[i]
			break
		}
	}
	if gone == nil {
		if c, err := n.findContact(member); err == nil {
			for i, m := range g.Members {
				if m.Hash == c.Hash {
					gone = &g.Members[i]
				}
			}
		}
	}
	if gone == nil {
		return g, fmt.Errorf("%q is not a member", member)
	}
	if gone.You {
		return g, errors.New("you own this group: leave it instead")
	}
	r := g.roster()
	kept := r.Members[:0]
	for _, m := range r.Members {
		if m.Public.Hash().String() != gone.Hash {
			kept = append(kept, m)
		}
	}
	r.Members, r.Version = kept, r.Version+1
	me := n.id.Hash()
	if err := n.db.putPrivate(g.id, r, &me, false, time.Now()); err != nil {
		return g, err
	}
	n.sendRoster(g.id, r, nil)
	n.sendOp(g.id, gone.pub, group.OpRemoved)
	n.emit(Event{Type: "group", Info: g.Hash})
	return n.findGroup(g.Hash)
}

// sendRoster sends the member list to every member but us (or only to).
func (n *Node) sendRoster(id packet.Hash, r group.Roster, only *identity.Public) {
	b, err := group.EncodeRoster(id, r)
	if err != nil {
		n.cfg.Logf("node: group roster: %v", err)
		return
	}
	me := n.id.Hash()
	for _, m := range r.Members {
		if m.Public.Hash() == me || (only != nil && m.Public != *only) {
			continue
		}
		if _, err := n.router.SendContent(n.id, m.Public, e2e.ContentGroup, b, false, false); err != nil {
			n.cfg.Logf("node: group roster to %s: %v", m.Public.Hash(), err)
		}
	}
}

func (n *Node) sendOp(id packet.Hash, to identity.Public, op group.Op) {
	if _, err := n.router.SendContent(n.id, to, e2e.ContentGroup, group.EncodeOp(id, op), false, false); err != nil {
		n.cfg.Logf("node: group op to %s: %v", to.Hash(), err)
	}
}

// LeaveGroup leaves a group. For a private group the other members are told.
// The history is kept.
func (n *Node) LeaveGroup(ref string) error {
	g, err := n.findGroup(ref)
	if err != nil {
		return err
	}
	if g.g != nil {
		n.router.RemoveGroup(g.id)
	} else if !g.Removed {
		for _, m := range g.Members {
			if !m.You {
				n.sendOp(g.id, m.pub, group.OpLeave)
			}
		}
	}
	if err := n.db.deleteGroup(g.id); err != nil {
		return err
	}
	n.emit(Event{Type: "group", Info: g.Hash})
	return nil
}

// Groups lists joined groups.
func (n *Node) Groups() ([]Group, error) { return n.db.groups(n.id.Hash()) }

// findGroup resolves a hash, hash prefix (6+), title ("#global") or name.
func (n *Node) findGroup(ref string) (Group, error) {
	gs, err := n.Groups()
	if err != nil {
		return Group{}, err
	}
	var matches []Group
	for _, g := range gs {
		if g.Hash == ref || strings.EqualFold(g.Title, ref) || strings.EqualFold(g.Name, ref) {
			return g, nil
		}
		if len(ref) >= 6 && strings.HasPrefix(g.Hash, strings.ToLower(ref)) {
			matches = append(matches, g)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Group{}, fmt.Errorf("no group %q", ref)
	}
	return Group{}, fmt.Errorf("%q matches %d groups", ref, len(matches))
}

// SendGroup posts text to a group. In a private group every other member
// gets their own copy, each with its own delivery receipt.
func (n *Node) SendGroup(ref, text string) (Message, error) {
	g, err := n.findGroup(ref)
	if err != nil {
		return Message{}, err
	}
	now := time.Now()
	m := Message{Peer: g.Hash, Outgoing: true, Text: text, Sent: now, Received: now,
		Group: true, From: n.id.Hash().String(), FromName: n.cfg.Name}
	if g.g != nil {
		id, err := n.router.SendGroupText(n.id, n.cfg.Name, g.id, text, false)
		if err != nil {
			return Message{}, err
		}
		m.ID = id.String()
		return m, n.db.putMessage(m)
	}
	if g.Removed {
		return Message{}, errors.New("you were removed from this group")
	}
	if message.TextBytes(text) > message.MaxTextBytes-group.IDSize-2 {
		return Message{}, errors.New("message: text too long")
	}
	m.ID = packet.NewID().String()
	content := group.EncodeText(g.id, text)
	for _, mem := range g.Members {
		if mem.You {
			continue
		}
		pid, err := n.router.SendContent(n.id, mem.pub, e2e.ContentGroup, content, false, false)
		if err != nil {
			return Message{}, err
		}
		n.db.db.Exec(`INSERT INTO group_sends(packet,msg,member) VALUES(?,?,?)`, pid.String(), m.ID, hashBytes(mem.pub.Hash()))
		m.Recipients++
	}
	return m, n.db.putMessage(m)
}

func hashBytes(h packet.Hash) []byte { return h[:] }

// onGroupReceipt records a member's receipt for a private-group copy. It
// reports the local message id and whether that changed anything.
func (n *Node) onGroupReceipt(acked packet.ID) (string, bool) {
	var msg string
	if err := n.db.db.QueryRow(`SELECT msg FROM group_sends WHERE packet=? AND delivered=0`, acked.String()).Scan(&msg); err != nil {
		return "", false
	}
	n.db.db.Exec(`UPDATE group_sends SET delivered=1 WHERE packet=?`, acked.String())
	var left int
	n.db.db.QueryRow(`SELECT COUNT(*) FROM group_sends WHERE msg=? AND delivered=0`, msg).Scan(&left)
	if left == 0 {
		n.db.db.Exec(`UPDATE messages SET delivered=1 WHERE id=?`, msg)
	}
	return msg, true
}

// fillDelivery sets Recipients / DeliveredTo of outgoing private-group messages.
func (n *Node) fillDelivery(m *Message) {
	if !m.Group || !m.Outgoing {
		return
	}
	n.db.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(delivered),0) FROM group_sends WHERE msg=?`, m.ID).Scan(&m.Recipients, &m.DeliveredTo)
}

// onPrivateGroup handles private-group traffic from a one-to-one message.
func (n *Node) onPrivateGroup(rm *message.Received) {
	env, err := group.Decode(rm.Content)
	if err != nil {
		n.cfg.Logf("node: group message: %v", err)
		return
	}
	from := rm.From.Hash()
	me := n.id.Hash()
	gs, _ := n.Groups()
	var g *Group
	for i := range gs {
		if gs[i].id == env.ID {
			g = &gs[i]
		}
	}
	if g != nil && g.Kind != group.Private {
		return
	}
	isMember := func() (string, bool) {
		for _, m := range g.Members {
			if m.Hash == from.String() {
				return m.Name, true
			}
		}
		return "", false
	}

	switch env.Op {
	case group.OpRoster:
		r := env.Roster
		if !r.Has(from) {
			return // a roster comes from a member: the owner
		}
		if g != nil && g.Owner != "" && (g.Owner != from.String() || r.Version <= g.version) {
			return // only the owner's newer lists count
		}
		if g == nil && !r.Has(me) {
			return
		}
		removed := !r.Has(me)
		if err := n.db.putPrivate(env.ID, *r, &from, removed, time.Now()); err != nil {
			n.cfg.Logf("node: group roster: %v", err)
			return
		}
		n.emit(Event{Type: "group", Info: env.ID.String()})

	case group.OpText, group.OpMedia:
		if g == nil {
			// The text overtook the member list: keep it in a group we learn
			// the name and members of when the list arrives.
			r := group.Roster{Name: "group", Members: []group.Member{{Public: rm.From}, {Public: n.id.Public}}}
			if err := n.db.putPrivate(env.ID, r, nil, false, time.Now()); err != nil {
				return
			}
			gs, _ := n.Groups()
			for i := range gs {
				if gs[i].id == env.ID {
					g = &gs[i]
				}
			}
			n.emit(Event{Type: "group", Info: env.ID.String()})
		}
		if g == nil || g.Removed {
			return
		}
		name, _ := isMember()
		m := Message{
			ID: rm.PacketID.String(), Peer: env.ID.String(), Text: env.Text, Sent: rm.Sent, Received: time.Now(),
			Group: true, From: from.String(), FromName: name,
		}
		if env.Op == group.OpMedia {
			n.onMedia(&m, env.Media)
			return
		}
		if err := n.db.putMessage(m); err != nil {
			n.cfg.Logf("node: store group message: %v", err)
		}
		n.nameGroupSender(&m)
		n.emit(Event{Type: "message", Message: &m})

	case group.OpLeave:
		if g == nil {
			return
		}
		if _, ok := isMember(); !ok {
			return
		}
		r := g.roster()
		kept := r.Members[:0]
		for _, m := range r.Members {
			if m.Public.Hash() != from {
				kept = append(kept, m)
			}
		}
		r.Members = kept
		var owner *packet.Hash
		if g.Owner != "" {
			o, _ := packet.ParseHash(g.Owner)
			owner = &o
		}
		if g.IsOwner {
			r.Version++ // the owner tells everyone else
		}
		if err := n.db.putPrivate(env.ID, r, owner, g.Removed, time.Now()); err == nil {
			if g.IsOwner {
				n.sendRoster(env.ID, r, nil)
			}
			n.emit(Event{Type: "group", Info: env.ID.String()})
		}

	case group.OpRemoved:
		if g == nil || g.Owner != from.String() {
			return
		}
		r := g.roster()
		o := from
		if err := n.db.putPrivate(env.ID, r, &o, true, time.Now()); err == nil {
			n.emit(Event{Type: "group", Info: env.ID.String()})
		}
	}
}

func (n *Node) onGroupMessage(gm *group.Message) {
	from := gm.From.Hash()
	m := Message{
		ID: gm.PacketID.String(), Peer: gm.Group.String(), Outgoing: from == n.id.Hash(),
		Text: gm.Text, Sent: gm.Sent, Received: time.Now(),
		Group: true, From: from.String(), FromName: gm.FromName,
	}
	if err := n.db.putMessage(m); err != nil {
		n.cfg.Logf("node: store group message: %v", err)
	}
	n.nameGroupSender(&m)
	n.emit(Event{Type: "message", Message: &m})
}

// nameGroupSender prefers the name we saved for a contact over the name the
// sender (public chats) or the owner (private groups) gave.
func (n *Node) nameGroupSender(m *Message) {
	if !m.Group || m.From == "" {
		return
	}
	n.fillDelivery(m)
	if m.From == n.id.Hash().String() {
		m.FromName = "you"
		return
	}
	if c, err := n.findContact(m.From); err == nil && c.Name != "" {
		m.FromName = c.Name
	}
}

// resolveChat finds a contact or a group by reference; exactly one is set.
func (n *Node) resolveChat(ref string) (*Contact, *Group, error) {
	if c, err := n.findContact(ref); err == nil {
		return &c, nil, nil
	}
	if g, err := n.findGroup(ref); err == nil {
		return nil, &g, nil
	}
	return nil, nil, fmt.Errorf("no contact or group %q", ref)
}

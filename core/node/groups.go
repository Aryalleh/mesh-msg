package node

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/group"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Group is a joined group chat or channel, as UIs see it.
type Group struct {
	Hash   string     `json:"hash"`
	Name   string     `json:"name"`
	Title  string     `json:"title"` // "#tehran" for channels
	Kind   group.Kind `json:"kind"`  // private | regional | global
	URI    string     `json:"uri"`   // invite link (meshgroup:…)
	Joined time.Time  `json:"joined"`
	g      *group.Group
}

func groupView(g *group.Group, joined time.Time) Group {
	return Group{Hash: g.Hash().String(), Name: g.Name, Title: g.Title(), Kind: g.Kind, URI: g.URI(), Joined: joined, g: g}
}

// Regions is a starter list of regional channels offered by UIs; any other
// name works too (everyone who types the same name meets in one channel).
var Regions = []string{
	"iran", "tehran", "mashhad", "isfahan", "karaj", "shiraz", "tabriz", "qom", "ahvaz",
	"kermanshah", "urmia", "rasht", "zahedan", "kerman", "hamadan", "yazd", "ardabil",
	"bandar-abbas", "arak", "sanandaj", "qazvin", "zanjan", "khorramabad", "gorgan", "sari",
	"bushehr", "birjand", "ilam", "shahrekord", "yasuj", "semnan", "bojnurd",
	"afghanistan", "kabul", "herat", "tajikistan", "iraq", "turkey", "europe", "north-america",
}

func (d *appDB) putGroup(g *group.Group, now time.Time) error {
	h := g.Hash()
	_, err := d.db.Exec(`INSERT INTO groups(hash,name,kind,secret,joined) VALUES(?,?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name`, h[:], g.Name, string(g.Kind), g.Secret[:], now.Unix())
	return err
}

func (d *appDB) groups() ([]Group, error) {
	rows, err := d.db.Query(`SELECT name,kind,secret,joined FROM groups ORDER BY joined`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var name, kind string
		var sec []byte
		var joined int64
		if err := rows.Scan(&name, &kind, &sec, &joined); err != nil {
			return nil, err
		}
		if len(sec) != group.SecretSize {
			continue
		}
		var s [group.SecretSize]byte
		copy(s[:], sec)
		g, err := group.FromSecret(name, group.Kind(kind), s)
		if err != nil {
			continue
		}
		out = append(out, groupView(g, time.Unix(joined, 0)))
	}
	return out, rows.Err()
}

func (d *appDB) deleteGroup(h packet.Hash) error {
	_, err := d.db.Exec(`DELETE FROM groups WHERE hash=?`, h[:])
	return err
}

// loadGroups joins the stored groups at startup.
func (n *Node) loadGroups() {
	gs, err := n.db.groups()
	if err != nil {
		n.cfg.Logf("node: groups: %v", err)
	}
	for _, g := range gs {
		n.router.AddGroup(g.g)
	}
}

func (n *Node) joinGroup(g *group.Group) (Group, error) {
	if err := n.db.putGroup(g, time.Now()); err != nil {
		return Group{}, err
	}
	n.router.AddGroup(g)
	n.router.Announce() // so relays accept our fetches of the group's mailbox
	n.emit(Event{Type: "group", Info: g.Hash().String()})
	return n.findGroup(g.Hash().String())
}

// CreateGroup makes a new private group; share its URI to invite people.
func (n *Node) CreateGroup(name string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("a group needs a name")
	}
	g, err := group.New(name, rand.Reader)
	if err != nil {
		return Group{}, err
	}
	return n.joinGroup(g)
}

// JoinGroup joins a group from an invite link (meshgroup:…) or a channel
// name ("#tehran", "global").
func (n *Node) JoinGroup(link string) (Group, error) {
	link = strings.TrimSpace(link)
	var g *group.Group
	var err error
	if strings.HasPrefix(link, "meshgroup:") || strings.HasPrefix(link, "#") {
		g, err = group.ParseURI(link)
	} else {
		g, err = group.Channel(link)
	}
	if err != nil {
		return Group{}, err
	}
	return n.joinGroup(g)
}

// LeaveGroup leaves a group (its history is kept until deleted with it).
func (n *Node) LeaveGroup(ref string) error {
	g, err := n.findGroup(ref)
	if err != nil {
		return err
	}
	h := g.g.Hash()
	n.router.RemoveGroup(h)
	if err := n.db.deleteGroup(h); err != nil {
		return err
	}
	n.emit(Event{Type: "group", Info: g.Hash})
	return nil
}

// Groups lists joined groups.
func (n *Node) Groups() ([]Group, error) { return n.db.groups() }

// findGroup resolves a hash, hash prefix (6+), title ("#global") or name.
func (n *Node) findGroup(ref string) (Group, error) {
	gs, err := n.db.groups()
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

// SendGroup posts text to a joined group.
func (n *Node) SendGroup(ref, text string) (Message, error) {
	g, err := n.findGroup(ref)
	if err != nil {
		return Message{}, err
	}
	id, err := n.router.SendGroupText(n.id, n.cfg.Name, g.g.Hash(), text, false)
	if err != nil {
		return Message{}, err
	}
	now := time.Now()
	m := Message{
		ID: id.String(), Peer: g.Hash, Outgoing: true, Text: text, Sent: now, Received: now,
		Group: true, From: n.id.Hash().String(), FromName: n.cfg.Name,
	}
	return m, n.db.putMessage(m)
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
// sender chose.
func (n *Node) nameGroupSender(m *Message) {
	if !m.Group || m.From == "" {
		return
	}
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

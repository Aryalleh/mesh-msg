package node

import (
	"strconv"
	"time"

	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Chat is one row of a chat list: a contact, its newest message and how many
// incoming messages arrived since the chat was last marked read.
type Chat struct {
	Contact Contact  `json:"contact"`         // empty for a group chat
	Group   *Group   `json:"group,omitempty"` // set for a group chat
	Last    *Message `json:"last,omitempty"`
	Unread  int      `json:"unread"`
}

// Chats lists every contact with its newest message, most recent first
// (contacts without messages last, by name).
func (n *Node) Chats() ([]Chat, error) {
	cs, err := n.db.contacts()
	if err != nil {
		return nil, err
	}
	out := make([]Chat, 0, len(cs))
	for _, c := range cs {
		h := c.pub.Hash()
		ch := Chat{Contact: c}
		if ms, err := n.db.messages(&h, 1); err == nil && len(ms) == 1 {
			ch.Last = &ms[0]
		}
		ch.Unread, _ = n.db.unread(h, n.readMark(c.Hash))
		out = append(out, ch)
	}
	gs, err := n.Groups()
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		h := g.id
		ch := Chat{Group: &g}
		if ms, err := n.db.messages(&h, 1); err == nil && len(ms) == 1 {
			n.nameGroupSender(&ms[0])
			ch.Last = &ms[0]
		}
		ch.Unread, _ = n.db.unread(h, n.readMark(g.Hash))
		out = append(out, ch)
	}
	sortChats(out)
	return out, nil
}

func sortChats(cs []Chat) {
	key := func(c Chat) int64 {
		if c.Last == nil {
			return 0
		}
		return c.Last.Received.UnixMilli()
	}
	for i := 1; i < len(cs); i++ { // insertion sort: stable, lists are short
		for j := i; j > 0 && key(cs[j]) > key(cs[j-1]); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// MarkRead records that the user has seen every message of a chat.
func (n *Node) MarkRead(ref string) error {
	c, g, err := n.resolveChat(ref)
	if err != nil {
		return err
	}
	hash := ""
	if c != nil {
		hash = c.Hash
	} else {
		hash = g.Hash
	}
	if err := n.db.set("read:"+hash, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return err
	}
	if c != nil {
		n.sendReadReceipt(c)
	}
	return nil
}

func (n *Node) readMark(hash string) int64 {
	v, ok := n.db.get("read:" + hash)
	if !ok {
		return 0
	}
	ms, _ := strconv.ParseInt(v, 10, 64)
	return ms
}

func (d *appDB) unread(peer packet.Hash, sinceMs int64) (int, error) {
	var c int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE peer=? AND outgoing=0 AND received>?`, peer[:], sinceMs).Scan(&c)
	return c, err
}

// SetLinkStatus publishes the status of a link a platform added itself
// (Android BLE mesh); subscribers get a "link" event when it changes.
func (n *Node) SetLinkStatus(name, status string) { n.setLinkStatus(name, status) }

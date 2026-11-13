package node

import (
	"encoding/hex"
	"strings"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

// Read receipts (protocol.md §3a): when the user opens a chat, the contact
// gets the packet id of the newest message seen; their app marks its
// messages up to that one as read (the second tick turns blue).
//
// They travel over relays only — never LoRa or Bluetooth, where airtime is
// scarce: the packet carries NOLORA and is handed straight to the relay
// links, outside the router's store (whose backlog would also go to
// Bluetooth neighbours). Without a relay link the receipt waits (one per
// chat, the newest wins) for one to come up. Only 1:1 chats.

const kvReadReceipts = "read_receipts" // "0": off

// ReadReceipts reports whether this node sends read receipts.
func (n *Node) ReadReceipts() bool {
	v, _ := n.db.get(kvReadReceipts)
	return v != "0"
}

// SetReadReceipts turns sending read receipts on or off.
func (n *Node) SetReadReceipts(on bool) error {
	v := "1"
	if !on {
		v = "0"
	}
	return n.db.set(kvReadReceipts, v)
}

// sendReadReceipt tells c which of their messages the user has seen.
func (n *Node) sendReadReceipt(c *Contact) {
	if !n.ReadReceipts() {
		return
	}
	h, err := packet.ParseHash(c.Hash)
	if err != nil {
		return
	}
	var id string
	if n.db.db.QueryRow(`SELECT id FROM messages WHERE peer=? AND outgoing=0 AND sender IS NULL ORDER BY received DESC LIMIT 1`, h[:]).Scan(&id) != nil {
		return
	}
	if v, _ := n.db.get("rr_sent:" + c.Hash); v == id {
		return
	}
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != len(packet.ID{}) {
		return
	}
	links := n.allRelayLinks()
	if len(links) == 0 {
		n.db.set("rr_pending:"+c.Hash, "1")
		return
	}
	pkts, err := message.BuildContent(n.id, c.pub, e2e.ContentRead, raw, message.Options{KnownContact: true})
	if err != nil {
		return
	}
	for _, p := range pkts {
		p.Ctx |= packet.CtxNoLoRa
		b := p.MustEncode()
		for _, l := range links {
			l.Send("", b) // queued while the relay is unreachable
		}
	}
	n.db.set("rr_sent:"+c.Hash, id)
	n.db.del("rr_pending:" + c.Hash)
}

// flushReadReceipts sends receipts that waited for a relay link.
func (n *Node) flushReadReceipts() {
	rows, err := n.db.db.Query(`SELECT k FROM kv WHERE k LIKE 'rr_pending:%'`)
	if err != nil {
		return
	}
	var hashes []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		hashes = append(hashes, strings.TrimPrefix(k, "rr_pending:"))
	}
	rows.Close()
	for _, h := range hashes {
		if c, err := n.findContact(h); err == nil {
			n.sendReadReceipt(&c)
		} else {
			n.db.del("rr_pending:" + h)
		}
	}
}

// onReadReceipt marks our messages to from, up to the one named, as read.
func (n *Node) onReadReceipt(from identity.Public, content []byte) {
	if len(content) != len(packet.ID{}) {
		return
	}
	peer := from.Hash()
	if _, err := n.findContact(peer.String()); err != nil {
		return
	}
	var upTo int64
	if n.db.db.QueryRow(`SELECT received FROM messages WHERE id=? AND peer=? AND outgoing=1`, hex.EncodeToString(content), peer[:]).Scan(&upTo) != nil {
		return
	}
	res, err := n.db.db.Exec(`UPDATE messages SET seen=1, delivered=1 WHERE peer=? AND outgoing=1 AND seen=0 AND received<=?`, peer[:], upTo)
	if err != nil {
		return
	}
	if k, _ := res.RowsAffected(); k > 0 {
		n.emit(Event{Type: "read", Info: peer.String()})
	}
}

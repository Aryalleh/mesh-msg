package node

import (
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
)

// SetLoRaRetry shortens the port reopen delay in tests.
func SetLoRaRetry(d time.Duration) { loraRetry = d }

// UseLoopbackDirect lets direct links use 127.0.0.1 (tests run on one host).
func (n *Node) UseLoopbackDirect() {
	if n.direct != nil {
		n.direct.net = direct.OSNet{Loopback: true}
	}
}

// DirectConnected tells whether a direct link to the contact is up.
func (n *Node) DirectConnected(peer string) bool { return n.direct != nil && n.direct.connected(peer) }

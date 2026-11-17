package blemesh

import (
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// NodeHost adapts a node.Node to Host.
type NodeHost struct{ N *node.Node }

func (h NodeHost) Inbound(link string, p router.PeerID, raw []byte) { h.N.Inbound(link, p, raw) }
func (h NodeHost) PeerUp(p router.PeerID)                           { h.N.Router().PeerUp(LinkName, p) }
func (h NodeHost) PeerDown(p router.PeerID)                         { h.N.Router().PeerDown(LinkName, p) }
func (h NodeHost) Status(s string)                                  { h.N.SetLinkStatus(LinkName, s) }

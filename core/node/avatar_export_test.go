package node

import (
	"context"
	"time"
)

// ProbeAvatarOver asks every neighbour on link for hash's picture: served
// tells whether one answered with a picture, answered whether any answered
// at all (tests only).
func (n *Node) ProbeAvatarOver(link, hash string) (served, answered bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, nb := range n.router.Neighbours() {
		if nb.Link != link {
			continue
		}
		size, err := n.probeP2P(ctx, nb, avatarID(hash))
		if err == nil {
			answered = true
			served = served || size > 0
		}
	}
	return served, answered
}

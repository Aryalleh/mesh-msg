package relay

import (
	"context"

	"github.com/Aryalleh/mesh-msg/core/wire"
)

// LocalSide exposes this relay as one end of a federation sync with Peer
// (see relayclient.SyncOnce). Labels match the HTTP API ("peer:<name>").
type LocalSide struct {
	R    *Relay
	Peer string
}

func (s LocalSide) Name() string { return s.R.Name() }

func (s LocalSide) Pull(_ context.Context, after int64, limit int) ([]wire.SyncItem, int64, bool, error) {
	items, cur, more := s.R.Pull(after, limit, "peer:"+s.Peer)
	return items, cur, more, nil
}

func (s LocalSide) Push(_ context.Context, items []wire.SyncItem) error {
	s.R.IngestSync(items, "peer:"+s.Peer)
	return nil
}

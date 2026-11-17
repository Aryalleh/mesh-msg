//go:build !linux || android

package blemesh

import (
	"context"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// Run is Linux only (BlueZ); elsewhere meshd has no Bluetooth.
func Run(ctx context.Context, n *node.Node, logf func(string, ...any)) {
	logf("bluetooth: only on Linux for now")
	<-ctx.Done()
}

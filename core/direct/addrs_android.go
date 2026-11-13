//go:build android

package direct

import (
	"net"

	"github.com/wlynxg/anet"
)

// Android 11+ forbids the netlink call net.InterfaceAddrs uses; anet reads
// the addresses another way.
func interfaceAddrs() ([]net.Addr, error) { return anet.InterfaceAddrs() }

//go:build !android

package direct

import "net"

func interfaceAddrs() ([]net.Addr, error) { return net.InterfaceAddrs() }

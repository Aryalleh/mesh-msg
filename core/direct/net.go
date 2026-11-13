package direct

import (
	"net"
	"net/netip"
)

// Net is what the agent needs from the network, so tests can run whole NAT
// topologies in memory (simnet_test.go).
type Net interface {
	// ListenUDP opens a UDP socket on every interface (port 0: any).
	ListenUDP(port int) (net.PacketConn, error)
	// Addrs lists the addresses of the up interfaces.
	Addrs() ([]netip.Addr, error)
}

// OSNet is the real network.
type OSNet struct {
	// Loopback: also offer 127.0.0.1 / ::1 (two apps on one machine, tests).
	Loopback bool
}

func (o OSNet) ListenUDP(port int) (net.PacketConn, error) {
	return net.ListenUDP("udp", &net.UDPAddr{Port: port})
}

func (o OSNet) Addrs() ([]netip.Addr, error) {
	as, err := interfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, a := range as {
		pfx, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(pfx.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		switch {
		case ip.IsLoopback() && !o.Loopback,
			ip.Is6() && ip.IsLinkLocalUnicast(), // needs a zone; IPv4 link-local stays
			ip.IsMulticast(), ip.IsUnspecified():
			continue
		}
		out = append(out, ip)
	}
	return out, nil
}

func addrPort(a net.Addr) netip.AddrPort {
	if u, ok := a.(*net.UDPAddr); ok {
		ap := u.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	ap, _ := netip.ParseAddrPort(a.String())
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

func udpAddr(a netip.AddrPort) *net.UDPAddr { return net.UDPAddrFromAddrPort(a) }

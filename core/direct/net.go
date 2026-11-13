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

// addrNet is a Net that can also open a socket on one local address. The
// agent then checks every path from the socket of the interface it leaves by.
type addrNet interface {
	ListenUDPAddr(ip netip.Addr) (net.PacketConn, error)
}

// OSNet is the real network.
type OSNet struct {
	// Loopback: also offer 127.0.0.1 / ::1 (two apps on one machine, tests).
	Loopback bool
	// Bind is called for every socket opened on one local address, with its
	// file descriptor. Android needs it: there the routing table is chosen
	// per network, not by destination, so a socket that is not bound to the
	// Wi-Fi network sends through the default network (mobile data when the
	// Wi-Fi has no internet) and never reaches a phone on the same Wi-Fi.
	Bind func(fd int, ip netip.Addr)
}

// ListenUDPAddr opens a UDP socket on one local address, any port.
func (o OSNet) ListenUDPAddr(ip netip.Addr) (net.PacketConn, error) {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip.AsSlice(), Zone: ip.Zone()})
	if err != nil {
		return nil, err
	}
	if o.Bind != nil {
		if rc, err := c.SyscallConn(); err == nil {
			rc.Control(func(fd uintptr) { o.Bind(int(fd), ip) })
		}
	}
	return c, nil
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

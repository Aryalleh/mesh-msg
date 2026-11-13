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
	// MoreAddrs lists local addresses the platform knows (Android:
	// java.net.NetworkInterface); they are added to what the OS call gives,
	// so host candidates exist even when that call is not allowed.
	MoreAddrs func() []netip.Addr
}

// ListenUDPAddr opens a UDP socket on one local address, any port.
func (o OSNet) ListenUDPAddr(ip netip.Addr) (net.PacketConn, error) {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip.AsSlice(), Zone: ip.Zone()})
	if err != nil {
		return nil, err
	}
	bigBuffers(c)
	if o.Bind != nil {
		if rc, err := c.SyscallConn(); err == nil {
			rc.Control(func(fd uintptr) { o.Bind(int(fd), ip) })
		}
	}
	return c, nil
}

func (o OSNet) ListenUDP(port int) (net.PacketConn, error) {
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err == nil {
		bigBuffers(c)
	}
	return c, err
}

// bigBuffers lets a burst of datagrams (a video keyframe is up to a few
// hundred) wait in the socket instead of being dropped; the OS caps it.
func bigBuffers(c *net.UDPConn) {
	c.SetReadBuffer(2 << 20)
	c.SetWriteBuffer(1 << 20)
}

func (o OSNet) Addrs() ([]netip.Addr, error) {
	as, err := interfaceAddrs()
	var all []netip.Addr
	for _, a := range as {
		var raw net.IP
		switch v := a.(type) {
		case *net.IPNet:
			raw = v.IP
		case *net.IPAddr:
			raw = v.IP
		}
		if ip, ok := netip.AddrFromSlice(raw); ok {
			all = append(all, ip)
		}
	}
	if o.MoreAddrs != nil {
		all = append(all, o.MoreAddrs()...)
	}
	if len(all) == 0 && err != nil {
		return nil, err
	}
	seen := map[netip.Addr]bool{}
	var out []netip.Addr
	for _, ip := range all {
		ip = ip.Unmap().WithZone("")
		switch {
		case !ip.IsValid(), seen[ip],
			ip.IsLoopback() && !o.Loopback,
			ip.Is6() && ip.IsLinkLocalUnicast(), // needs a zone; IPv4 link-local stays
			ip.IsMulticast(), ip.IsUnspecified():
			continue
		}
		seen[ip] = true
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

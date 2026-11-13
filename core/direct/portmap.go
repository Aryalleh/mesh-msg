package direct

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

// Router port mapping (connectivity.md §3a): ask the gateway to forward an
// outside port to the session socket, by PCP, then NAT-PMP, then UPnP IGD.
// The lease is short and renewed while the session lives; release removes
// it. This works on home routers, not on carrier NAT.

const (
	pmpPort  = 5351
	lease    = 10 * time.Minute
	renewGap = 5 * time.Minute
)

// portMapper holds what tests replace.
type portMapper struct {
	gateways []netip.Addr
	pmpPort  int
	ssdp     func(ctx context.Context) []string // UPnP device description URLs
}

func mapPort(ctx context.Context, n Net, port uint16) (Candidate, func(), error) {
	addrs, _ := n.Addrs()
	pm := portMapper{gateways: gateways(addrs), pmpPort: pmpPort, ssdp: ssdpSearch}
	return pm.mapPort(ctx, port)
}

func (pm portMapper) mapPort(ctx context.Context, port uint16) (Candidate, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, gw := range pm.gateways {
		for _, proto := range []func(context.Context, netip.Addr, uint16, time.Duration) (netip.AddrPort, error){pm.pcp, pm.natpmp} {
			ext, err := proto(ctx, gw, port, lease)
			if err != nil {
				continue
			}
			stop := make(chan struct{})
			go renew(stop, func() { proto(context.Background(), gw, port, lease) })
			release := func() {
				close(stop)
				c, cl := context.WithTimeout(context.Background(), time.Second)
				defer cl()
				proto(c, gw, port, 0)
			}
			return Candidate{Kind: Mapped, Addr: ext, Priority: priority(Mapped, ext.Addr())}, release, nil
		}
	}
	for _, loc := range pm.ssdp(ctx) {
		ig, err := upnpDevice(ctx, loc)
		if err != nil {
			continue
		}
		ext, err := ig.add(ctx, port, lease)
		if err != nil {
			continue
		}
		stop := make(chan struct{})
		go renew(stop, func() { ig.add(context.Background(), port, lease) })
		release := func() {
			close(stop)
			c, cl := context.WithTimeout(context.Background(), 2*time.Second)
			defer cl()
			ig.remove(c, port)
		}
		return Candidate{Kind: Mapped, Addr: ext, Priority: priority(Mapped, ext.Addr())}, release, nil
	}
	return Candidate{}, nil, errors.New("direct: no router granted a port mapping")
}

func renew(stop chan struct{}, f func()) {
	t := time.NewTicker(renewGap)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			f()
		}
	}
}

// gateways: the default gateway from the routing table where readable,
// then x.y.z.1 of every private IPv4 address (the usual router).
func gateways(addrs []netip.Addr) []netip.Addr {
	var out []netip.Addr
	if b, err := os.ReadFile("/proc/net/route"); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) > 2 && f[1] == "00000000" && f[2] != "00000000" {
				if g, err := hex.DecodeString(f[2]); err == nil && len(g) == 4 {
					out = append(out, netip.AddrFrom4([4]byte{g[3], g[2], g[1], g[0]}))
				}
			}
		}
	}
	for _, a := range addrs {
		if a.Is4() && a.IsPrivate() {
			v := a.As4()
			v[3] = 1
			if g := netip.AddrFrom4(v); g != a && !contains(out, g) {
				out = append(out, g)
			}
		}
	}
	return out
}

func contains(as []netip.Addr, a netip.Addr) bool {
	for _, x := range as {
		if x == a {
			return true
		}
	}
	return false
}

// udpAsk sends req to gw and returns the first answer.
func udpAsk(ctx context.Context, gw netip.Addr, port int, req []byte) ([]byte, error) {
	c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(gw, uint16(port))))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	buf := make([]byte, 1100)
	for try, wait := 0, 250*time.Millisecond; try < 3; try, wait = try+1, wait*2 {
		c.Write(req)
		dl := time.Now().Add(wait)
		if d, ok := ctx.Deadline(); ok && d.Before(dl) {
			dl = d
		}
		c.SetReadDeadline(dl)
		n, err := c.Read(buf)
		if err == nil {
			return buf[:n], nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("direct: gateway did not answer")
}

// natpmp maps UDP port (RFC 6886); lifetime 0 removes the mapping.
func (pm portMapper) natpmp(ctx context.Context, gw netip.Addr, port uint16, life time.Duration) (netip.AddrPort, error) {
	resp, err := udpAsk(ctx, gw, pm.pmpPort, []byte{0, 0}) // external address
	if err != nil || len(resp) < 12 || resp[1] != 128 || binary.BigEndian.Uint16(resp[2:]) != 0 {
		return netip.AddrPort{}, errors.New("direct: NAT-PMP: no external address")
	}
	ext := netip.AddrFrom4([4]byte(resp[8:12]))
	req := []byte{0, 1, 0, 0}
	req = binary.BigEndian.AppendUint16(req, port)
	req = binary.BigEndian.AppendUint16(req, port)
	req = binary.BigEndian.AppendUint32(req, uint32(life.Seconds()))
	resp, err = udpAsk(ctx, gw, pm.pmpPort, req)
	if err != nil || len(resp) < 16 || resp[1] != 129 || binary.BigEndian.Uint16(resp[2:]) != 0 {
		return netip.AddrPort{}, errors.New("direct: NAT-PMP: mapping refused")
	}
	return netip.AddrPortFrom(ext, binary.BigEndian.Uint16(resp[10:])), nil
}

// pcp maps UDP port with a PCP MAP request (RFC 6887).
func (pm portMapper) pcp(ctx context.Context, gw netip.Addr, port uint16, life time.Duration) (netip.AddrPort, error) {
	local, err := localAddrTo(gw)
	if err != nil {
		return netip.AddrPort{}, err
	}
	req := []byte{2, 1, 0, 0}
	req = binary.BigEndian.AppendUint32(req, uint32(life.Seconds()))
	req = append(req, mapped16(local)...)
	nonce := make([]byte, 12)
	rand.Read(nonce)
	req = append(req, nonce...)
	req = append(req, 17, 0, 0, 0) // UDP
	req = binary.BigEndian.AppendUint16(req, port)
	req = binary.BigEndian.AppendUint16(req, port)
	req = append(req, make([]byte, 16)...) // no suggested external address
	resp, err := udpAsk(ctx, gw, pm.pmpPort, req)
	if err != nil || len(resp) < 60 || resp[0] != 2 || resp[1] != 0x81 || resp[3] != 0 || !bytes.Equal(resp[24:36], nonce) {
		return netip.AddrPort{}, errors.New("direct: PCP: mapping refused")
	}
	ext := netip.AddrFrom16([16]byte(resp[44:60])).Unmap()
	return netip.AddrPortFrom(ext, binary.BigEndian.Uint16(resp[42:])), nil
}

// mapped16: the address as 16 bytes (IPv4-mapped for IPv4).
func mapped16(a netip.Addr) []byte {
	b := a.As16() // As16 maps IPv4 to ::ffff:a.b.c.d
	return b[:]
}

func localAddrTo(gw netip.Addr) (netip.Addr, error) {
	c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(gw, 9)))
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(), nil
}

// ---- UPnP IGD ----

func ssdpSearch(ctx context.Context) []string {
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil
	}
	defer c.Close()
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	for _, st := range []string{"urn:schemas-upnp-org:service:WANIPConnection:1", "urn:schemas-upnp-org:service:WANPPPConnection:1", "urn:schemas-upnp-org:service:WANIPConnection:2"} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: " + st + "\r\n\r\n"
		c.WriteTo([]byte(msg), dst)
	}
	var out []string
	dl := time.Now().Add(1500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(dl) {
		dl = d
	}
	c.SetReadDeadline(dl)
	buf := make([]byte, 2048)
	for {
		n, _, err := c.ReadFrom(buf)
		if err != nil {
			return out
		}
		for _, line := range strings.Split(string(buf[:n]), "\r\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "location") {
				if v = strings.TrimSpace(v); !containsS(out, v) {
					out = append(out, v)
				}
			}
		}
	}
}

func containsS(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

type igd struct {
	control, service string
	local            netip.Addr
}

// upnpDevice reads a device description and finds its WAN connection service.
func upnpDevice(ctx context.Context, location string) (*igd, error) {
	u, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", location, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var desc struct {
		Services []struct {
			Type    string `xml:"serviceType"`
			Control string `xml:"controlURL"`
		} `xml:"device>deviceList>device>deviceList>device>serviceList>service"`
		Services2 []struct {
			Type    string `xml:"serviceType"`
			Control string `xml:"controlURL"`
		} `xml:"device>serviceList>service"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&desc); err != nil {
		return nil, err
	}
	for _, s := range append(desc.Services, desc.Services2...) {
		if strings.Contains(s.Type, "WANIPConnection") || strings.Contains(s.Type, "WANPPPConnection") {
			ctl, err := u.Parse(s.Control)
			if err != nil {
				continue
			}
			host, _, _ := net.SplitHostPort(u.Host)
			gw, _ := netip.ParseAddr(host)
			local, _ := localAddrTo(gw)
			return &igd{control: ctl.String(), service: s.Type, local: local}, nil
		}
	}
	return nil, errors.New("direct: UPnP device without a WAN connection service")
}

func (g *igd) soap(ctx context.Context, action, args string) ([]byte, error) {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` +
		`<u:` + action + ` xmlns:u="` + g.service + `">` + args + `</u:` + action + `></s:Body></s:Envelope>`
	req, _ := http.NewRequestWithContext(ctx, "POST", g.control, strings.NewReader(body))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+g.service+"#"+action+`"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("direct: UPnP %s: %s", action, resp.Status)
	}
	return b, nil
}

func (g *igd) add(ctx context.Context, port uint16, life time.Duration) (netip.AddrPort, error) {
	b, err := g.soap(ctx, "GetExternalIPAddress", "")
	if err != nil {
		return netip.AddrPort{}, err
	}
	var ext struct {
		IP string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
	}
	xml.Unmarshal(b, &ext)
	ip, err := netip.ParseAddr(strings.TrimSpace(ext.IP))
	if err != nil {
		return netip.AddrPort{}, errors.New("direct: UPnP: no external address")
	}
	p := fmt.Sprint(port)
	_, err = g.soap(ctx, "AddPortMapping", "<NewRemoteHost></NewRemoteHost><NewExternalPort>"+p+"</NewExternalPort><NewProtocol>UDP</NewProtocol>"+
		"<NewInternalPort>"+p+"</NewInternalPort><NewInternalClient>"+g.local.String()+"</NewInternalClient><NewEnabled>1</NewEnabled>"+
		"<NewPortMappingDescription>Mava</NewPortMappingDescription><NewLeaseDuration>"+fmt.Sprint(int(life.Seconds()))+"</NewLeaseDuration>")
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(ip, port), nil
}

func (g *igd) remove(ctx context.Context, port uint16) {
	g.soap(ctx, "DeletePortMapping", "<NewRemoteHost></NewRemoteHost><NewExternalPort>"+fmt.Sprint(port)+"</NewExternalPort><NewProtocol>UDP</NewProtocol>")
}

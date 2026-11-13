package direct

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

// fakeGateway answers NAT-PMP (version 0) and, if pcp, PCP (version 2).
func fakeGateway(t *testing.T, pcp bool) (port int, mapped func() []uint32) {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	var mu sync.Mutex
	var lifetimes []uint32
	go func() {
		buf := make([]byte, 1100)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			b := buf[:n]
			switch {
			case b[0] == 0 && b[1] == 0: // NAT-PMP external address
				pc.WriteTo([]byte{0, 128, 0, 0, 0, 0, 0, 1, 198, 51, 100, 77}, from)
			case b[0] == 0 && b[1] == 1 && n >= 12:
				mu.Lock()
				lifetimes = append(lifetimes, binary.BigEndian.Uint32(b[8:]))
				mu.Unlock()
				r := []byte{0, 129, 0, 0, 0, 0, 0, 1}
				r = append(r, b[4:6]...)                    // internal port
				r = binary.BigEndian.AppendUint16(r, 31000) // external port
				r = append(r, b[8:12]...)                   // lifetime
				pc.WriteTo(r, from)
			case b[0] == 2 && pcp && n >= 60:
				mu.Lock()
				lifetimes = append(lifetimes, binary.BigEndian.Uint32(b[4:]))
				mu.Unlock()
				r := make([]byte, 60)
				r[0], r[1] = 2, 0x81
				copy(r[4:8], b[4:8])
				copy(r[24:36], b[24:36]) // nonce
				r[36] = 17
				copy(r[40:42], b[40:42])
				binary.BigEndian.PutUint16(r[42:], 32000)
				ext := netip.MustParseAddr("198.51.100.78").As16()
				copy(r[44:], ext[:])
				pc.WriteTo(r, from)
			case b[0] == 2: // a NAT-PMP-only router: unsupported version
				pc.WriteTo([]byte{0, 0x81, 0, 1}, from)
			}
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port, func() []uint32 { mu.Lock(); defer mu.Unlock(); return append([]uint32(nil), lifetimes...) }
}

func TestPortMappingPCPAndNATPMP(t *testing.T) {
	lo := netip.MustParseAddr("127.0.0.1")
	none := func(context.Context) []string { return nil }
	// PCP first.
	port, lt := fakeGateway(t, true)
	pm := portMapper{gateways: []netip.Addr{lo}, pmpPort: port, ssdp: none}
	c, release, err := pm.mapPort(context.Background(), 4500)
	if err != nil || c.Kind != Mapped || c.Addr.String() != "198.51.100.78:32000" {
		t.Fatalf("PCP: %v %v", c, err)
	}
	release()
	if l := lt(); len(l) != 2 || l[0] == 0 || l[1] != 0 {
		t.Fatalf("PCP lifetimes %v: want a lease, then 0 to remove it", l)
	}
	// A NAT-PMP-only router.
	port, lt = fakeGateway(t, false)
	pm.pmpPort = port
	c, release, err = pm.mapPort(context.Background(), 4500)
	if err != nil || c.Addr.String() != "198.51.100.77:31000" {
		t.Fatalf("NAT-PMP: %v %v", c, err)
	}
	release()
	if l := lt(); len(l) != 2 || l[1] != 0 {
		t.Fatalf("NAT-PMP lifetimes %v", l)
	}
}

func TestPortMappingUPnP(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><root><device><deviceList><device><deviceList><device><serviceList><service>
<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl</controlURL>
</service></serviceList></device></deviceList></device></deviceList></device></root>`))
	})
	mux.HandleFunc("/ctl", func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("SOAPAction")
		mu.Lock()
		actions = append(actions, a[strings.Index(a, "#")+1:len(a)-1])
		mu.Unlock()
		if strings.Contains(a, "GetExternalIPAddress") {
			w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetExternalIPAddressResponse><NewExternalIPAddress>198.51.100.79</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`))
		}
	})
	pm := portMapper{pmpPort: 1, ssdp: func(context.Context) []string { return []string{srv.URL + "/desc.xml"} }}
	c, release, err := pm.mapPort(context.Background(), 4600)
	if err != nil || c.Addr.String() != "198.51.100.79:4600" {
		t.Fatalf("UPnP: %v %v", c, err)
	}
	release()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(actions, ",") != "GetExternalIPAddress,AddPortMapping,DeletePortMapping" {
		t.Fatalf("actions %v", actions)
	}
}

package direct

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// Every local address gets a socket of its own, the bind hook sees each of
// them, their ports are offered as host candidates, and the path that wins
// between two agents on one machine uses a bound socket.
func TestBoundSocketPerAddress(t *testing.T) {
	var mu sync.Mutex
	bound := map[netip.Addr]int{}
	osn := OSNet{Loopback: true, Bind: func(fd int, ip netip.Addr) {
		mu.Lock()
		bound[ip]++
		mu.Unlock()
		if fd <= 0 {
			t.Errorf("bad descriptor %d", fd)
		}
	}}
	var sid [16]byte
	var key [32]byte
	sid[0], key[0] = 7, 9
	a, err := NewAgent(Config{Net: osn}, sid, key, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewAgent(Config{Net: osn}, sid, key, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	oa, ob := a.Gather(ctx), b.Gather(ctx)

	addrs, _ := osn.Addrs()
	if len(addrs) == 0 {
		t.Skip("no addresses")
	}
	mu.Lock()
	for _, ip := range addrs[:min(len(addrs), maxBoundSocks)] {
		if bound[ip] != 2 { // once per agent
			t.Errorf("address %s bound %d times", ip, bound[ip])
		}
	}
	mu.Unlock()
	ports := map[uint16]bool{}
	for _, c := range oa.Candidates {
		if c.Kind == Host {
			ports[c.Addr.Port()] = true
		}
	}
	if len(ports) < 2 {
		t.Fatalf("host candidates share one port: %v", oa.Candidates)
	}

	type res struct {
		l   *Link
		err error
	}
	ch := make(chan res, 2)
	go func() { l, err := a.Connect(ctx, ob); ch <- res{l, err} }()
	go func() { l, err := b.Connect(ctx, oa); ch <- res{l, err} }()
	for i := 0; i < 2; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatal(r.err)
		}
		if !r.l.s.bound {
			t.Errorf("chosen path %s is not on a bound socket", r.l.path)
		}
	}
}

// Addresses the platform reports are offered even when the OS call gives
// nothing useful, without duplicates and without unusable ones.
func TestMoreAddrs(t *testing.T) {
	extra := netip.MustParseAddr("192.0.2.77")
	o := OSNet{MoreAddrs: func() []netip.Addr {
		return []netip.Addr{extra, extra, netip.MustParseAddr("fe80::1"), netip.MustParseAddr("127.0.0.1"), {}}
	}}
	as, err := o.Addrs()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range as {
		if a == extra {
			n++
		}
		if a.IsLoopback() || a.IsLinkLocalUnicast() || !a.IsValid() {
			t.Errorf("unusable address %s offered", a)
		}
	}
	if n != 1 {
		t.Fatalf("platform address offered %d times: %v", n, as)
	}
}

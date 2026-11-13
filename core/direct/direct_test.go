package direct

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

// pairUp gathers on both sides, swaps the offers (the signaling) and
// connects both at once, like two apps would.
func pairUp(t *testing.T, a, b Config) (*Link, *Link, error) {
	t.Helper()
	var sid [16]byte
	var key [32]byte
	rand.Read(sid[:])
	rand.Read(key[:])
	if a.CheckTimeout == 0 {
		a.CheckTimeout = 4 * time.Second
	}
	if b.CheckTimeout == 0 {
		b.CheckTimeout = 4 * time.Second
	}
	ag, err := NewAgent(a, sid, key, true)
	if err != nil {
		t.Fatal(err)
	}
	bg, err := NewAgent(b, sid, key, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	oa, ob := ag.Gather(ctx), bg.Gather(ctx)
	// The offers travel as bytes.
	da, _, err := DecodeOffer(oa.Encode())
	if err != nil {
		t.Fatal(err)
	}
	db, _, err := DecodeOffer(ob.Encode())
	if err != nil {
		t.Fatal(err)
	}
	type res struct {
		l   *Link
		err error
	}
	cb := make(chan res, 1)
	go func() {
		l, err := bg.Connect(ctx, da)
		cb <- res{l, err}
	}()
	la, errA := ag.Connect(ctx, db)
	rb := <-cb
	t.Cleanup(func() { ag.Close(); bg.Close() })
	if errA != nil {
		return nil, nil, errA
	}
	if rb.err != nil {
		return nil, nil, rb.err
	}
	return la, rb.l, nil
}

// talk sends both ways and checks the data arrives intact.
func talk(t *testing.T, a, b *Link) {
	t.Helper()
	for i, dir := range []struct{ from, to *Link }{{a, b}, {b, a}} {
		msg := bytes.Repeat([]byte{byte(i + 1)}, 1000)
		deadline := time.After(5 * time.Second)
		for got := false; !got; {
			if err := dir.from.Send(msg); err != nil {
				t.Fatal(err)
			}
			select {
			case m := <-dir.to.Recv():
				if !bytes.Equal(m, msg) {
					t.Fatal("data differs")
				}
				got = true
			case <-time.After(200 * time.Millisecond):
			case <-deadline:
				t.Fatalf("direction %d: no data", i)
			}
		}
	}
}

func TestSameLANStaysLocal(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	home := w.lan("home", inet, natIndep, "203.0.113.5")
	r1, r2 := w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")
	a := w.host(home, "192.168.1.10")
	b := w.host(home, "192.168.1.11")
	la, lb, err := pairUp(t, Config{Net: a, Reflectors: []string{r1, r2}}, Config{Net: b, Reflectors: []string{r1, r2}})
	if err != nil {
		t.Fatal(err)
	}
	if p := la.Path(); p.Class != 1 || p.Remote.Kind != Host {
		t.Fatalf("path %s, want host ↔ host on the LAN", p)
	}
	before := inet.packets.Load()
	talk(t, la, lb)
	if n := inet.packets.Load() - before; n != 0 {
		t.Fatalf("%d session packets went over the internet", n)
	}
}

// Phone A shares mobile data (behind carrier NAT) as a hotspot; B and C on
// the hotspot talk inside it, and A reaches B on its hotspot interface.
func TestHotspotBehindCGNAT(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	carrier := w.lan("carrier", inet, natIndep, "203.0.113.9")
	hot := w.lan("hotspot", carrier, natIndep, "100.64.7.2")
	r1, r2 := w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")
	refl := []string{r1, r2}
	phoneA := w.host(carrier, "100.64.7.2", hot, "192.168.43.1")
	b := w.host(hot, "192.168.43.20")
	c := w.host(hot, "192.168.43.21")

	lb, lc, err := pairUp(t, Config{Net: b, Reflectors: refl}, Config{Net: c, Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	if lb.Path().Class != 1 {
		t.Fatalf("B ↔ C: %s", lb.Path())
	}
	before := carrier.packets.Load()
	talk(t, lb, lc)
	if carrier.packets.Load() != before {
		t.Fatal("B ↔ C traffic left the hotspot")
	}

	la, lb2, err := pairUp(t, Config{Net: phoneA, Reflectors: refl}, Config{Net: b, Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	if p := la.Path(); p.Class != 1 || !strings.HasPrefix(p.Remote.Addr.Addr().String(), "192.168.43.") {
		t.Fatalf("A ↔ B: %s, want the hotspot interface", p)
	}
	talk(t, la, lb2)
}

// Double NAT on one side, a home router on the other: hole punching over
// the reflexive addresses.
func TestDoubleNATHolePunch(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	isp := w.lan("isp", inet, natIndep, "203.0.113.20")
	inner := w.lan("home2", isp, natIndep, "10.0.0.2")
	other := w.lan("other", inet, natIndep, "198.18.0.7")
	r1, r2 := w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")
	refl := []string{r1, r2}
	a := w.host(inner, "192.168.0.5")
	b := w.host(other, "192.168.1.5")
	la, lb, err := pairUp(t, Config{Net: a, Reflectors: refl}, Config{Net: b, Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	if p := la.Path(); p.Class != 3 && p.Class != 4 {
		t.Fatalf("path %s, want through both NATs", p)
	}
	talk(t, la, lb)
}

// Two unrelated networks with the same private subnet and the same host
// address and port: no false match, the path crosses the internet.
func TestSameSubnetDifferentNetworks(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	n1 := w.lan("n1", inet, natIndep, "203.0.113.1")
	n2 := w.lan("n2", inet, natIndep, "203.0.113.2")
	r1, r2 := w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")
	refl := []string{r1, r2}
	a := w.host(n1, "192.168.1.10")
	b := w.host(n2, "192.168.1.10") // same address, and the same first port
	la, lb, err := pairUp(t, Config{Net: a, Reflectors: refl}, Config{Net: b, Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	if p := la.Path(); p.Class == 1 {
		t.Fatalf("false local match: %s", p)
	}
	talk(t, la, lb)
}

func TestNATTypeDetection(t *testing.T) {
	for _, tc := range []struct {
		kind natKind
		want NATType
	}{{natIndep, NATIndep}, {natSymSeq, NATSymSeq}, {natSymRandom, NATSymRandom}, {natBlockUDP, NATBlocked}} {
		w := newWorld()
		inet := w.lan("internet", nil, 0, "")
		n := w.lan("n", inet, tc.kind, "203.0.113.1")
		refl := []string{w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2"), w.reflector(inet, "198.51.100.3")}
		ag, _ := NewAgent(Config{Net: w.host(n, "10.0.0.2"), Reflectors: refl}, [16]byte{}, [32]byte{}, true)
		o := ag.Gather(context.Background())
		ag.Close()
		if o.NAT.Type != tc.want {
			t.Errorf("NAT %d: detected %s, want %s", tc.kind, o.NAT.Type, tc.want)
		}
		if tc.want == NATSymSeq && o.NAT.Step != 1 {
			t.Errorf("step %d", o.NAT.Step)
		}
	}
	// No NAT at all.
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	refl := []string{w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")}
	ag, _ := NewAgent(Config{Net: w.host(inet, "198.18.5.5"), Reflectors: refl}, [16]byte{}, [32]byte{}, true)
	defer ag.Close()
	if o := ag.Gather(context.Background()); o.NAT.Type != NATOpen {
		t.Errorf("public address: %s", o.NAT.Type)
	}
}

func TestSymmetricSequentialPrediction(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	sym := w.lan("carrier", inet, natSymSeq, "203.0.113.30")
	home := w.lan("home", inet, natIndep, "203.0.113.31")
	refl := []string{w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")}
	la, lb, err := pairUp(t, Config{Net: w.host(sym, "10.1.0.2"), Reflectors: refl}, Config{Net: w.host(home, "192.168.1.2"), Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	talk(t, la, lb)
}

func TestSymmetricRandomBirthday(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	sym := w.lan("carrier", inet, natSymRandom, "203.0.113.40")
	home := w.lan("home", inet, natIndep, "203.0.113.41")
	refl := []string{w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")}
	la, lb, err := pairUp(t, Config{Net: w.host(sym, "10.1.0.2"), Reflectors: refl}, Config{Net: w.host(home, "192.168.1.2"), Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	talk(t, la, lb)
}

func TestBothSymmetricFailsWithReason(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	s1 := w.lan("c1", inet, natSymRandom, "203.0.113.50")
	s2 := w.lan("c2", inet, natSymRandom, "203.0.113.51")
	refl := []string{w.reflector(inet, "198.51.100.1"), w.reflector(inet, "198.51.100.2")}
	_, _, err := pairUp(t, Config{Net: w.host(s1, "10.1.0.2"), Reflectors: refl, CheckTimeout: 2 * time.Second, SymmetricTimeout: 4 * time.Second},
		Config{Net: w.host(s2, "10.2.0.2"), Reflectors: refl, CheckTimeout: 2 * time.Second, SymmetricTimeout: 4 * time.Second})
	if err == nil {
		t.Skip("connected anyway (a lucky birthday hit)")
	}
	if !errors.Is(err, ErrNoPath) || !strings.Contains(err.Error(), "symmetric") {
		t.Fatalf("error %v", err)
	}
}

// The peer's NAT gives it a new outside port mid-session (CGNAT rebinding):
// the link follows its authenticated data to the new address. (When both
// sides sit behind port-restricted NATs, a new signaling round is needed:
// core/node reconnects.)
func TestLinkFollowsPeerAfterRebinding(t *testing.T) {
	w := newWorld()
	inet := w.lan("internet", nil, 0, "")
	n2 := w.lan("n2", inet, natIndep, "203.0.113.2")
	refl := []string{w.reflector(inet, "198.51.100.1")}
	la, lb, err := pairUp(t, Config{Net: w.host(inet, "198.18.0.1"), Reflectors: refl}, Config{Net: w.host(n2, "10.0.0.2"), Reflectors: refl})
	if err != nil {
		t.Fatal(err)
	}
	talk(t, la, lb)
	old := la.Path().Remote.Addr
	n2.rebind()
	lb.Send(nil) // B's keepalive, from its new outside port
	time.Sleep(100 * time.Millisecond)
	if la.Path().Remote.Addr == old {
		t.Fatal("A did not follow B to its new address")
	}
	talk(t, la, lb)
}

func TestOfferEncoding(t *testing.T) {
	o := Offer{NAT: NATInfo{Type: NATSymSeq, Step: -2}, Candidates: []Candidate{
		{Kind: Host, Priority: 7}, // invalid address: dropped
	}}
	o.Candidates[0].Addr = o.NAT.Last
	b := o.Encode()
	d, rest, err := DecodeOffer(append(b, 9, 9)) // unknown trailing bytes are fine
	if err != nil || d.NAT.Step != -2 || d.NAT.Type != NATSymSeq || len(rest) != 2 {
		t.Fatal(err, d)
	}
	if _, _, err := DecodeOffer(b[:2]); err == nil {
		t.Fatal("short offer accepted")
	}
}

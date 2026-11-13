package turn_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/turn"
)

func freePort(t *testing.T) int {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func start(t *testing.T, admin string, enabled bool) (*turn.Service, []int) {
	ports := []int{freePort(t), freePort(t)}
	s, err := turn.Start(turn.Config{
		ListenHost: "127.0.0.1", Ports: ports, PublicIP: net.IPv4(127, 0, 0, 1),
		RelayMin: 52000, RelayMax: 52999,
		IsAdmin: func(h string) bool { return h == admin },
		Limits:  turn.Limits{Enabled: enabled, MaxSessions: 2, MaxKbps: 1000, MonthlyGB: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, ports
}

// credentials asks the service as account id.
func credentials(t *testing.T, s *turn.Service, id *identity.Identity) (turn.Credentials, int) {
	day := wire.Day(time.Now())
	req := httptest.NewRequest("POST", "/v1/turn/credentials", nil)
	req.Header.Set(wire.HeaderAccount, hex.EncodeToString(id.Public.Bytes()))
	req.Header.Set(wire.HeaderDay, strconv.Itoa(int(day)))
	req.Header.Set(wire.HeaderSignature, hex.EncodeToString(id.SignDomain(wire.DomainTurnCredentials, wire.QuotaSigBody(day))))
	w := httptest.NewRecorder()
	s.CredentialsHandler(w, req)
	var c turn.Credentials
	json.NewDecoder(w.Body).Decode(&c)
	return c, w.Code
}

func TestReflectorForEveryone(t *testing.T) {
	_, ports := start(t, "", false)
	refl := []string{"127.0.0.1:" + strconv.Itoa(ports[0]), "127.0.0.1:" + strconv.Itoa(ports[1])}
	ag, err := direct.NewAgent(direct.Config{Net: direct.OSNet{Loopback: true}, Reflectors: refl}, [16]byte{}, [32]byte{}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	o := ag.Gather(context.Background())
	if o.NAT.Type != direct.NATOpen {
		t.Fatalf("NAT %s (candidates %v)", o.NAT.Type, o.Candidates)
	}
}

func TestTURNOnlyForAdmins(t *testing.T) {
	admin, _ := identity.Generate()
	user, _ := identity.Generate()
	s, ports := start(t, admin.Hash().String(), true)
	if _, code := credentials(t, s, user); code != http.StatusForbidden {
		t.Fatalf("non-admin got credentials: %d", code)
	}
	cred, code := credentials(t, s, admin)
	if code != 200 || cred.Username == "" || cred.Port != ports[0] {
		t.Fatalf("admin: %d %+v", code, cred)
	}

	// The admin's app allocates; the path goes through the relay only
	// because this test allows nothing else.
	var sid [16]byte
	var key [32]byte
	rand.Read(sid[:])
	rand.Read(key[:])
	srv := "127.0.0.1:" + strconv.Itoa(cred.Port)
	a, _ := direct.NewAgent(direct.Config{Net: direct.OSNet{Loopback: true}, RelayOnly: true, CheckTimeout: 4 * time.Second,
		TURN: &direct.TURNServer{Addr: srv, Username: cred.Username, Password: cred.Password}}, sid, key, true)
	b, _ := direct.NewAgent(direct.Config{Net: direct.OSNet{Loopback: true}, CheckTimeout: 4 * time.Second}, sid, key, false)
	defer a.Close()
	defer b.Close()
	ctx := context.Background()
	oa, ob := a.Gather(ctx), b.Gather(ctx)
	relayed := false
	for _, c := range oa.Candidates {
		relayed = relayed || c.Kind == direct.Relayed
	}
	if !relayed {
		t.Fatalf("no relayed candidate: %v", oa.Candidates)
	}
	if st := s.Stats(); st.Sessions != 1 {
		t.Fatalf("sessions %d", st.Sessions)
	}
	done := make(chan *direct.Link, 1)
	go func() {
		l, err := b.Connect(ctx, oa)
		if err != nil {
			t.Error(err)
		}
		done <- l
	}()
	la, err := a.Connect(ctx, ob)
	if err != nil {
		t.Fatal(err)
	}
	lb := <-done
	if lb == nil {
		t.FailNow()
	}
	if !la.Path().Relayed() {
		t.Fatalf("path %s", la.Path())
	}
	la.Send([]byte("through the relay"))
	select {
	case m := <-lb.Recv():
		if string(m) != "through the relay" {
			t.Fatal("data differs")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no data through TURN")
	}
	if s.Stats().MonthBytes == 0 {
		t.Fatal("traffic not counted")
	}

	// Wrong password: no allocation.
	c, _ := direct.NewAgent(direct.Config{Net: direct.OSNet{Loopback: true},
		TURN: &direct.TURNServer{Addr: srv, Username: cred.Username, Password: "guess"}}, sid, key, true)
	defer c.Close()
	for _, cand := range c.Gather(ctx).Candidates {
		if cand.Kind == direct.Relayed {
			t.Fatal("allocated with a wrong password")
		}
	}

	// Turned off in the panel: no credentials, no new allocations.
	s.SetLimits(turn.Limits{Enabled: false, MaxSessions: 2, MaxKbps: 1000, MonthlyGB: 1})
	if _, code := credentials(t, s, admin); code != http.StatusNotFound {
		t.Fatalf("TURN off: %d", code)
	}
}

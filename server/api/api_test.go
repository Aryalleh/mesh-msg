package api_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

type testRelay struct {
	r   *relay.Relay
	srv *httptest.Server
}

func startRelay(t *testing.T, name, region string, tokens ...string) *testRelay {
	t.Helper()
	key, _ := identity.Generate()
	r := relay.New(relay.Config{Name: name, Region: region})
	h := api.New(api.Config{
		Relay: r, Identity: key, PowBits: 8, SyncTokens: tokens, RateLimit: 1000,
		Relays: []wire.RelayEntry{{Name: name, Region: region, URLs: []string{"https://" + name}}},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &testRelay{r: r, srv: srv}
}

func client(t *testing.T, tr *testRelay, origin, token string) *relayclient.Client {
	c, err := relayclient.New(tr.srv.URL, relayclient.Options{Origin: origin, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func encode(ps ...*packet.Packet) [][]byte {
	var out [][]byte
	for _, p := range ps {
		out = append(out, p.MustEncode())
	}
	return out
}

func TestBlackoutPath(t *testing.T) {
	ctx := context.Background()
	ir := startRelay(t, "ir-1", "ir")
	eu := startRelay(t, "eu-1", "global", "fed-secret")
	us := startRelay(t, "us-1", "global", "fed-secret")

	ali, _ := identity.Generate()
	sara, _ := identity.Generate()
	now := time.Now()

	// Ali (Tehran, domestic network only) announces and sends via IR relay.
	aliC := client(t, ir, "ali-phone", "")
	pk, _ := message.BuildText(ali, sara.Public, "سلام سارا، اینجا اینترنت قطع است", message.Options{KnownContact: true})
	if _, err := aliC.Upload(ctx, append(encode(announce.Build(ali, now, announce.AppData{}, 8)), encode(pk...)...)); err != nil {
		t.Fatal(err)
	}
	// Sara (US) announces to the US relay.
	saraC := client(t, us, "sara-laptop", "")
	if _, err := saraC.Upload(ctx, encode(announce.Build(sara, now, announce.AppData{}, 8))); err != nil {
		t.Fatal(err)
	}

	// Federation EU<->US (token) and a bridge IR<->EU (proof-of-work only).
	fedCur, bridgeCur := &relayclient.Cursors{}, &relayclient.Cursors{}
	fed := func() {
		if _, _, err := relayclient.SyncOnce(ctx,
			relay.LocalSide{R: eu.r, Peer: "us-1"},
			relayclient.RemoteSide{C: client(t, us, "eu-1", "fed-secret"), Label: "us-1"}, fedCur, nil); err != nil {
			t.Fatal("federation:", err)
		}
	}
	bridge := func() {
		if _, _, err := relayclient.SyncOnce(ctx,
			relayclient.RemoteSide{C: client(t, ir, "bridge-1", ""), Label: "ir-1"},
			relayclient.RemoteSide{C: client(t, eu, "bridge-1", ""), Label: "eu-1"}, bridgeCur, nil); err != nil {
			t.Fatal("bridge:", err)
		}
	}
	bridge()
	fed()

	// Sara fetches her mailbox from the US relay.
	raws, _, err := saraC.Mailbox(ctx, sara, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(raws) != 1 {
		t.Fatalf("Sara's mailbox has %d packets", len(raws))
	}
	p, _ := packet.Decode(raws[0])
	got, err := message.Open(sara, []identity.Public{ali.Public}, p)
	if err != nil || got.Text != "سلام سارا، اینجا اینترنت قطع است" {
		t.Fatal("decrypt:", err)
	}

	// Ali's home region survived two hops of sync.
	if region, ok := us.r.HomeRegion(ali.Hash()); !ok || region != "ir" {
		t.Fatalf("home region at US relay = %q", region)
	}

	// Sara's receipt flows back and purges every copy, including in Iran.
	saraC.Upload(ctx, encode(announce.BuildReceipt(sara, p.ID, 8)))
	fed()
	bridge()
	for name, tr := range map[string]*testRelay{"ir": ir, "eu": eu, "us": us} {
		if box, _ := tr.r.Mailbox(sara.Hash(), 0, 0); len(box) != 0 {
			t.Fatalf("%s relay still holds delivered packet", name)
		}
	}

	// Sara replies; a carrier gateway in Iran picks it up for the mesh.
	reply, _ := message.BuildText(sara, ali.Public, "رسید! نگران نباش", message.Options{KnownContact: true})
	saraC.Upload(ctx, encode(reply...))
	fed()
	bridge()
	gw := client(t, ir, "reza-phone", "")
	gpk, _, err := gw.Gateway(ctx, relayclient.GatewayRequest{Carrier: true, QuotaBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range gpk {
		if q, _ := packet.Decode(raw); q.Type == packet.TypeData && q.Dest == ali.Hash() {
			found = true
		}
	}
	if !found {
		t.Fatal("carrier gateway in Iran did not receive the reply for Ali")
	}
}

func TestPowAndAuth(t *testing.T) {
	ctx := context.Background()
	ir := startRelay(t, "ir-1", "ir", "tok")
	ali, _ := identity.Generate()
	pk, _ := message.BuildText(ali, ali.Public, "x", message.Options{})

	// Raw request without proof-of-work is refused.
	resp, err := http.Post(ir.srv.URL+"/v1/packets", wire.ContentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	body := packet.EncodeFrames(encode(pk...))
	resp, _ = http.Post(ir.srv.URL+"/v1/packets", wire.ContentType, bytes.NewReader(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("upload without PoW: %d", resp.StatusCode)
	}

	// Mailbox without signature is refused.
	other, _ := identity.Generate()
	c := client(t, ir, "", "")
	c.Upload(ctx, encode(announce.Build(ali, time.Now(), announce.AppData{}, 8)))
	if _, _, err := c.Mailbox(ctx, other, 0, 0); err == nil {
		t.Fatal("mailbox for unknown identity should fail")
	}
	var se *relayclient.StatusError
	if _, _, _, err := client(t, ir, "x", "wrong").SyncPull(ctx, 0, 10, 0); !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("bad sync token: %v", err)
	}

	// Signed relay list verifies.
	l, err := c.Relays(ctx)
	if err != nil || !api.VerifyRelayList(*l) {
		t.Fatal("relay list signature", err)
	}
}

func TestLongPoll(t *testing.T) {
	ctx := context.Background()
	ir := startRelay(t, "ir-1", "ir")
	sara, _ := identity.Generate()
	ali, _ := identity.Generate()
	c := client(t, ir, "sara", "")
	c.Upload(ctx, encode(announce.Build(sara, time.Now(), announce.AppData{}, 8)))

	go func() {
		time.Sleep(300 * time.Millisecond)
		pk, _ := message.BuildText(ali, sara.Public, "live", message.Options{})
		client(t, ir, "ali", "").Upload(ctx, encode(pk...))
	}()
	start := time.Now()
	raws, _, err := c.Mailbox(ctx, sara, 0, 5*time.Second)
	if err != nil || len(raws) != 1 {
		t.Fatal("long poll", err, len(raws))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("long poll returned late: %v", d)
	}
}

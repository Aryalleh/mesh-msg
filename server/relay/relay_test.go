package relay

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/announce"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
)

func encodeAll(ps []*packet.Packet) [][]byte {
	var out [][]byte
	for _, p := range ps {
		out = append(out, p.MustEncode())
	}
	return out
}

func setup(t *testing.T) (*Relay, *identity.Identity, *identity.Identity, *time.Time) {
	now := time.Unix(2_000_000, 0)
	r := New(Config{Name: "ir-1", Region: "ir", Now: func() time.Time { return now }})
	ali, _ := identity.Generate()
	sara, _ := identity.Generate()
	return r, ali, sara, &now
}

func TestIngestDedupAndReceipt(t *testing.T) {
	r, ali, sara, _ := setup(t)
	pk, _ := message.BuildText(ali, sara.Public, "salam", message.Options{KnownContact: true})
	raws := encodeAll(pk)
	if n := r.Ingest(raws, "client:a", "ir"); n != 1 {
		t.Fatalf("accepted %d", n)
	}
	if n := r.Ingest(raws, "client:b", "ir"); n != 0 {
		t.Fatal("duplicate accepted")
	}
	// Receipt without a known announce is stored but cannot purge.
	rc := announce.BuildReceipt(sara, pk[0].ID, 8).MustEncode()
	r.Ingest([][]byte{rc}, "client:s", "ir")
	if got, _ := r.Mailbox(sara.Hash(), 0, 0); len(got) != 1 {
		t.Fatal("unverifiable receipt must not purge")
	}
	// After Sara's announce, a fresh copy of the receipt via another path
	// is a duplicate, but a re-uploaded DATA is refused and new receipts purge.
	r.Ingest([][]byte{announce.Build(sara, time.Unix(2_000_000, 0), announce.AppData{}, 8).MustEncode()}, "client:s", "ir")
	pk2, _ := message.BuildText(ali, sara.Public, "second", message.Options{KnownContact: true})
	r.Ingest(encodeAll(pk2), "client:a", "ir")
	r.Ingest([][]byte{announce.BuildReceipt(sara, pk2[0].ID, 8).MustEncode()}, "client:s", "ir")
	box, _ := r.Mailbox(sara.Hash(), 0, 0)
	if len(box) != 1 {
		t.Fatalf("verified receipt should purge second message, mailbox=%d", len(box))
	}
	// Re-upload of a delivered packet is refused.
	if n := r.Ingest(encodeAll(pk2), "client:x", "ir"); n != 0 {
		t.Fatal("delivered packet re-accepted")
	}
}

func TestFetchAuth(t *testing.T) {
	r, _, sara, _ := setup(t)
	r.Ingest([][]byte{announce.Build(sara, time.Unix(2_000_000, 0), announce.AppData{}, 8).MustEncode()}, "c", "ir")
	n := r.NewChallenge()
	if r.AuthorizeFetch(sara.Hash(), n, []byte("bad")) {
		t.Fatal("bad signature accepted")
	}
	n = r.NewChallenge()
	if !r.AuthorizeFetch(sara.Hash(), n, announce.SignFetch(sara, []byte(n))) {
		t.Fatal("good signature rejected")
	}
	if r.AuthorizeFetch(sara.Hash(), n, announce.SignFetch(sara, []byte(n))) {
		t.Fatal("challenge reused")
	}
}

func TestPullExcludesOrigin(t *testing.T) {
	r, ali, sara, _ := setup(t)
	a, _ := message.BuildText(ali, sara.Public, "from eu", message.Options{KnownContact: true})
	b, _ := message.BuildText(ali, sara.Public, "from client", message.Options{KnownContact: true})
	r.Ingest(encodeAll(a), "eu-1", "")
	r.Ingest(encodeAll(b), "client:x", "ir")
	got, cur, more := r.Pull(0, 10, "eu-1")
	if len(got) != 1 || more || cur != 2 {
		t.Fatalf("pull: %d items cur=%d more=%v", len(got), cur, more)
	}
	got, _, _ = r.Pull(cur, 10, "eu-1")
	if len(got) != 0 {
		t.Fatal("cursor not advanced")
	}
}

func TestGatewayTargetedAndCarrier(t *testing.T) {
	r, ali, sara, now := setup(t)
	reza, _ := identity.Generate() // not delegated
	r.Ingest([][]byte{
		announce.Build(sara, *now, announce.AppData{Flags: announce.FlagDelegate}, 8).MustEncode(),
		announce.Build(reza, *now, announce.AppData{}, 8).MustEncode(),
	}, "client:gw", "ir")
	toSara, _ := message.BuildText(ali, sara.Public, "for sara", message.Options{KnownContact: true})
	toReza, _ := message.BuildText(ali, reza.Public, "for reza", message.Options{KnownContact: true})
	r.Ingest(encodeAll(toSara), "eu-1", "")
	r.Ingest(encodeAll(toReza), "eu-1", "")

	got, _ := r.Gateway(GatewayRequest{Origin: "gw", Serve: []packet.Hash{sara.Hash(), reza.Hash()}})
	if len(got) != 1 {
		t.Fatalf("targeted: want only delegated Sara's packet, got %d", len(got))
	}
	got, _ = r.Gateway(GatewayRequest{Origin: "gw", Carrier: true, QuotaBytes: 10_000})
	if len(got) != 0 {
		t.Fatal("carrier must wait CarrierMinAge so online recipients fetch directly")
	}
	*now = now.Add(11 * time.Minute)
	got, _ = r.Gateway(GatewayRequest{Origin: "gw", Carrier: true, QuotaBytes: 10_000})
	if len(got) != 2 {
		t.Fatalf("carrier: home region ir → both, got %d", len(got))
	}
	got, _ = r.Gateway(GatewayRequest{Origin: "gw", Carrier: true, QuotaBytes: 10})
	if len(got) != 0 {
		t.Fatal("quota not enforced")
	}
}

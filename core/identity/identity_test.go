package identity

import "testing"

func TestSeedRoundTrip(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := FromSeed(a.Seed())
	if err != nil {
		t.Fatal(err)
	}
	if a.Public != b.Public || a.Hash() != b.Hash() {
		t.Fatal("seed round trip changed keys")
	}
}

func TestSign(t *testing.T) {
	a, _ := Generate()
	sig := a.SignDomain("TEST", []byte("msg"))
	if !a.Verify("TEST", []byte("msg"), sig) {
		t.Fatal("valid signature rejected")
	}
	if a.Verify("OTHER", []byte("msg"), sig) {
		t.Fatal("domain separation broken")
	}
}

func TestURI(t *testing.T) {
	a, _ := Generate()
	c := Contact{Public: a.Public, Name: "علی رضایی"}
	got, err := ParseURI(c.URI())
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParseURI("http://x"); err == nil {
		t.Fatal("expected error")
	}
}

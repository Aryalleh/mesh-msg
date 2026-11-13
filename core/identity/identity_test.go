package identity

import (
	"strings"
	"testing"
)

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

func TestLink(t *testing.T) {
	a, _ := Generate()
	c := Contact{Public: a.Public, Name: "علی رضایی"}
	l := c.Link()
	if !strings.HasPrefix(l, "https://eu-1.mfadem.ir/add#") || strings.Contains(l, "meshmsg:") {
		t.Fatal(l)
	}
	for _, s := range []string{l, " " + l + "\n", strings.Replace(l, "eu-1.mfadem.ir", "other.example/", 1) /* trailing slash */, "http://h/add/#" + strings.TrimPrefix(c.URI(), "meshmsg:")} {
		if got, err := ParseURI(s); err != nil || got != c {
			t.Fatalf("%q: %v %+v", s, err, got)
		}
	}
	for _, s := range []string{"https://eu-1.mfadem.ir/other#x", "https://eu-1.mfadem.ir/add", "https://eu-1.mfadem.ir/add#garbage"} {
		if _, err := ParseURI(s); err == nil {
			t.Fatalf("%q accepted", s)
		}
	}
}

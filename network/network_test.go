package network

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestBootstrap(t *testing.T) {
	l := Bootstrap()
	if r := l.Relays[0]; r.Name != "eu-1" || !r.Origin || r.URLs[0] != "https://eu-1.mfadem.ir" || len(r.PubKey) != 128 {
		t.Fatalf("first relay must be the original eu-1: %+v", r)
	}
}

func TestCheckURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://87.106.5.192":          "http://87.106.5.192",
		"HTTPS://Relay.Example.org/x/": "https://relay.example.org/x",
		"http://abc.onion":             "http://abc.onion",
	} {
		if got, err := CheckURL(in, false); err != nil || got != want {
			t.Errorf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"ftp://a.org", "http://user@a.org", "http://127.0.0.1:8080", "http://192.168.1.5", "http://10.0.0.1",
		"http://[::1]", "http://localhost", "http://router", "http://a.org/?x=1", "javascript:alert(1)", "http://169.254.1.1", "http://0.0.0.0"} {
		if _, err := CheckURL(bad, false); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if _, err := CheckURL("http://127.0.0.1:8080", true); err != nil {
		t.Error(err)
	}
}

func TestSignedList(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	b := []byte(`{"version":1,"updated":"2026-11-16","serial":3,"relays":[{"name":"x","region":"global","urls":["https://x.example"]}]}`)
	s := Signed{JSON: b, Sig: Sign(b, priv)}
	got, l, err := DecodeSigned(s.Encode(), pub)
	if err != nil || l.Serial != 3 || string(got.JSON) != string(b) {
		t.Fatalf("decode: %v %+v", err, l)
	}
	bad := s.Encode()
	bad[len(bad)-3] ^= 1
	if _, _, err := DecodeSigned(bad, pub); err == nil {
		t.Fatal("tampered list accepted")
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, _, err := DecodeSigned(s.Encode(), other); err == nil {
		t.Fatal("list signed by another key accepted")
	}
}

// A relays.json.sig that no longer matches relays.json means the list was
// edited without re-signing (go run ./tools/relays-sign).
func TestEmbeddedSignatureCurrent(t *testing.T) {
	if len(strings.TrimSpace(string(bootstrapSig))) == 0 {
		t.Skip("relays.json is not signed yet")
	}
	if BootstrapSigned().Sig == nil {
		t.Fatal("network/relays.json.sig does not match relays.json: bump serial and run go run ./tools/relays-sign")
	}
	if Bootstrap().Serial <= 0 {
		t.Fatal("relays.json needs a serial")
	}
}

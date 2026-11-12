package network

import "testing"

func TestBootstrap(t *testing.T) {
	l := Bootstrap()
	if r := l.Relays[0]; r.Name != "eu-1" || !r.Origin || r.URLs[0] != "http://87.106.5.192" || len(r.PubKey) != 128 {
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

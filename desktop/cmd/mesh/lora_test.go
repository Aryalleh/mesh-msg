package main

import "testing"

func TestParseWifi(t *testing.T) {
	w, err := parseWifi([]string{"ssid=Home Net", "password=secret123", "relay=https://r.example"})
	if err != nil {
		t.Fatal(err)
	}
	if *w.SSID != "Home Net" || *w.Password != "secret123" || *w.Relay != "https://r.example" {
		t.Fatalf("%+v", w)
	}
	if w, err := parseWifi([]string{"ssid="}); err != nil || *w.SSID != "" {
		t.Fatalf("forget: %v %+v", err, w)
	}
	for _, bad := range [][]string{nil, {"x"}, {"password=short"}, {"relay=ftp://x"}, {"color=red"}, {"ssid=" + string(make([]byte, 33))}} {
		if _, err := parseWifi(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestParsePatch(t *testing.T) {
	p, err := parsePatch([]string{"role=Gateway", "region=eu_868", "preset=long", "freq=869.525", "power=14", "name=roof", "announce=0"})
	if err != nil {
		t.Fatal(err)
	}
	if *p.Role != "gateway" || *p.Region != "EU_868" || *p.Preset != "LONG" || *p.FreqHz != 869525000 ||
		*p.TxDBm != 14 || *p.Name != "roof" || *p.AnnounceMin != 0 {
		t.Fatalf("%+v", p)
	}
	for _, bad := range [][]string{nil, {"x"}, {"freq=abc"}, {"freq=5"}, {"power=99"}, {"announce=-1"}, {"color=red"}} {
		if _, err := parsePatch(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

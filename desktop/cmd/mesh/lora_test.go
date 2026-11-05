package main

import "testing"

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

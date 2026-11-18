package direct

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

// spec/test-vectors/calls.json: the probe and data-link bytes the Mava
// board's C++ (firmware/lib/meshcall) is tested against.
func TestDirectVectors(t *testing.T) {
	raw, err := os.ReadFile("../../spec/test-vectors/calls.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	h := func(k string) []byte {
		b, err := hex.DecodeString(v[k])
		if err != nil || v[k] == "" {
			t.Fatalf("vector %s", k)
		}
		return b
	}
	var key [32]byte
	var sid [16]byte
	copy(key[:], h("path_key"))
	copy(sid[:], h("call_id"))
	ctl := &Agent{sid: sid, key: key, controlling: true, macKey: derive(key, "mava direct probe")}
	oth := &Agent{sid: sid, key: key, macKey: derive(key, "mava direct probe")}
	if !bytes.Equal(ctl.macKey, h("probe_mac_key")) || !bytes.Equal(derive(key, "mava direct data a"), h("data_key_a")) ||
		!bytes.Equal(derive(key, "mava direct data b"), h("data_key_b")) {
		t.Fatal("derived keys")
	}
	var tx [8]byte
	copy(tx[:], "txtxtxtx")
	if !bytes.Equal(ctl.probe(probeReq, tx, netip.AddrPort{}), h("probe_req")) {
		t.Fatal("probe request")
	}
	if !bytes.Equal(oth.probe(probeResp, tx, netip.MustParseAddrPort("198.51.100.7:4242")), h("probe_resp")) {
		t.Fatal("probe response")
	}
	typ, gotTx, seen, ok := ctl.checkProbe(h("probe_resp"))
	if !ok || typ != probeResp || gotTx != tx || seen != netip.MustParseAddrPort("198.51.100.7:4242") {
		t.Fatal("probe response check")
	}
	l := newLink(ctl, nil, netip.AddrPort{}, Path{})
	hd := make([]byte, dataHead)
	copy(hd, dataMagic)
	hd[11] = 5
	var nonce [12]byte
	nonce[11] = 5
	if !bytes.Equal(l.seal.Seal(hd, nonce[:], []byte("datagram from the controlling side"), hd), h("link_datagram5")) {
		t.Fatal("link datagram")
	}
}

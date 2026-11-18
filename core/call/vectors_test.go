package call

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"os"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/flynn/noise"
)

// spec/test-vectors/calls.json pins the bytes of the call protocol: the
// Mava board's C++ (firmware/lib/meshcall) is tested against the same file.

type fixedRand struct{ b []byte }

func (r *fixedRand) Read(p []byte) (int, error) {
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func loadCallVectors(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("../../spec/test-vectors/calls.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func unhex(t *testing.T, v map[string]string, k string) []byte {
	t.Helper()
	b, err := hex.DecodeString(v[k])
	if err != nil || v[k] == "" {
		t.Fatalf("vector %s: %v", k, err)
	}
	return b
}

func TestCallVectors(t *testing.T) {
	v := loadCallVectors(t)
	a, _ := identity.FromSeed(unhex(t, v, "seed_a"))
	b, _ := identity.FromSeed(unhex(t, v, "seed_b"))
	var callID [16]byte
	copy(callID[:], unhex(t, v, "call_id"))
	mk := func(self *identity.Identity, peer identity.Public, init bool, eph []byte) *noise.HandshakeState {
		hs, err := noise.NewHandshakeState(noise.Config{
			CipherSuite: suite, Pattern: noise.HandshakeKK, Initiator: init,
			Prologue:      append([]byte("mava call v1"), callID[:]...),
			StaticKeypair: keypair(self), PeerStatic: append([]byte(nil), peer.X[:]...),
			Random: &fixedRand{eph},
		})
		if err != nil {
			t.Fatal(err)
		}
		return hs
	}
	hsA, hsB := mk(a, b.Public, true, unhex(t, v, "eph_a")), mk(b, a.Public, false, unhex(t, v, "eph_b"))
	m1, _, _, _ := hsA.WriteMessage(nil, nil)
	if !bytes.Equal(m1, unhex(t, v, "noise_msg1")) {
		t.Fatal("noise message 1")
	}
	if _, _, _, err := hsB.ReadMessage(nil, m1); err != nil {
		t.Fatal(err)
	}
	m2, b1, b2, _ := hsB.WriteMessage(nil, nil)
	if !bytes.Equal(m2, unhex(t, v, "noise_msg2")) {
		t.Fatal("noise message 2")
	}
	_, a1, a2, err := hsA.ReadMessage(nil, m2)
	if err != nil {
		t.Fatal(err)
	}
	cA := &crypto{initiator: true, send: a1.Cipher(), rcv: a2.Cipher(), hash: hsA.ChannelBinding()}
	cB := &crypto{send: b2.Cipher(), rcv: b1.Cipher(), hash: hsB.ChannelBinding()}
	if !bytes.Equal(cA.hash, unhex(t, v, "handshake_hash")) || cA.code() != v["code"] || cB.code() != v["code"] {
		t.Fatal("handshake hash / code")
	}
	if !bytes.Equal(cA.seal(Frame{Stream: 0, Seq: 7, TS: 140, Payload: []byte("opus frame from A")}), unhex(t, v, "frame_a")) {
		t.Fatal("frame a")
	}
	if !bytes.Equal(cB.seal(Frame{Stream: 1, Seq: 3, TS: 60, Flags: FlagKeyframe | FlagEnd, Payload: []byte("video from B")}), unhex(t, v, "frame_b")) {
		t.Fatal("frame b")
	}

	cand := []direct.Candidate{
		{Kind: direct.Host, Addr: netip.MustParseAddrPort("192.168.1.20:40000"), Priority: 126 << 24},
		{Kind: direct.Reflexive, Addr: netip.MustParseAddrPort("203.0.113.9:51000"), Priority: 100 << 24},
		{Kind: direct.Host, Addr: netip.MustParseAddrPort("[2001:db8::5]:40002"), Priority: 126<<24 + 1<<16},
	}
	var key [32]byte
	copy(key[:], unhex(t, v, "path_key"))
	sigs := map[string]Signal{
		"signal_offer": {CallID: callID, Op: OpOffer, Expires: 1700000045, Key: key,
			Caps:  Caps{Version: 1, Streams: []StreamCap{{ID: 0, Media: MediaAudio, Dir: DirBoth, Codecs: []byte{CodecOpus, CodecAMRWB}, MaxKbps: 24}}},
			Offer: direct.Offer{NAT: direct.NATInfo{Type: direct.NATIndep, Last: netip.MustParseAddrPort("203.0.113.9:51000")}, Candidates: cand}},
		"signal_answer": {CallID: callID, Op: OpAnswer,
			Caps:  Caps{Version: 1, Streams: []StreamCap{{ID: 0, Media: MediaAudio, Dir: DirBoth, Codecs: []byte{CodecOpus}, MaxKbps: 24}}},
			Offer: direct.Offer{NAT: direct.NATInfo{Type: direct.NATSymSeq, Step: -2}, Candidates: cand[:1]}},
		"signal_end":     {CallID: callID, Op: OpEnd, Reason: EndNoPath},
		"signal_control": {CallID: callID, Op: OpControl, Stream: 0, Kind: 2, Body: []byte{1}},
	}
	for k, s := range sigs {
		if !bytes.Equal(s.Encode(), unhex(t, v, k)) {
			t.Fatalf("%s", k)
		}
		if d, err := Decode(unhex(t, v, k)); err != nil || !bytes.Equal(d.Encode(), unhex(t, v, k)) {
			t.Fatalf("%s decode: %v", k, err)
		}
	}
}

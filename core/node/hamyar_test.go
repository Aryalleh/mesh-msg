package node

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

func TestSignHamyar(t *testing.T) {
	n, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	now := time.Unix(1795300000, 0)
	h := n.SignHamyar("post", "/v1/mava/account", now)
	pubBytes, _ := hex.DecodeString(h[wire.HeaderAccount])
	pub, err := identity.PublicFromBytes(pubBytes)
	if err != nil || pub.Hash() != n.Identity().Hash() {
		t.Fatal("account header is not this identity", err)
	}
	sig, _ := hex.DecodeString(h[wire.HeaderSignature])
	what := sha256.Sum256([]byte("POST /v1/mava/account"))
	if !pub.Verify(HamyarDomain, wire.TripSigBody(wire.Day(now), what[:]), sig) {
		t.Fatal("signature does not verify")
	}
	other := sha256.Sum256([]byte("POST /v1/other"))
	if pub.Verify(HamyarDomain, wire.TripSigBody(wire.Day(now), other[:]), sig) {
		t.Fatal("signature verifies for another path")
	}
	if pub.Verify(wire.DomainTripPut, wire.TripSigBody(wire.Day(now), what[:]), sig) {
		t.Fatal("signature verifies under another domain")
	}
}

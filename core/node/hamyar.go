package node

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/wire"
)

// HamyarDomain is the signature domain of the Hamyar API (docs/hamyar-app.md).
const HamyarDomain = "HAMYAR-API-V1"

// SignHamyar returns the headers that sign a request to a Hamyar API as this
// account, the same scheme as trip requests under Hamyar's own domain:
// Ed25519(HAMYAR-API-V1 || u32be(day) || SHA-256("<METHOD> <path>")).
// The signature only proves the account for that method and path on that day.
func (n *Node) SignHamyar(method, path string, now time.Time) map[string]string {
	what := sha256.Sum256([]byte(strings.ToUpper(method) + " " + path))
	day := wire.Day(now)
	return map[string]string{
		wire.HeaderAccount:   hex.EncodeToString(n.id.Public.Bytes()),
		wire.HeaderDay:       strconv.FormatUint(uint64(day), 10),
		wire.HeaderSignature: hex.EncodeToString(n.id.SignDomain(HamyarDomain, wire.TripSigBody(day, what[:]))),
	}
}

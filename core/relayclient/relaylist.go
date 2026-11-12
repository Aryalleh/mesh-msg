package relayclient

import (
	"encoding/hex"
	"encoding/json"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// DomainRelayList separates relay-list signatures from other signatures.
const DomainRelayList = "MESHMSG-RELAYS"

// RelayListPayload is the signed part of a relay list.
func RelayListPayload(relays []wire.RelayEntry, issued int64) []byte {
	b, _ := json.Marshal(struct {
		Relays []wire.RelayEntry `json:"relays"`
		Issued int64             `json:"issued"`
	}{relays, issued})
	return b
}

// VerifyRelayList checks a list's signature against its embedded key.
func VerifyRelayList(l wire.RelayList) bool {
	pk, err := hex.DecodeString(l.PubKey)
	if err != nil {
		return false
	}
	pub, err := identity.PublicFromBytes(pk)
	if err != nil {
		return false
	}
	sig, err := hex.DecodeString(l.Signature)
	if err != nil {
		return false
	}
	return pub.Verify(DomainRelayList, RelayListPayload(l.Relays, l.Issued), sig)
}

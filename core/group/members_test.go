package group

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
)

func TestMembersCodec(t *testing.T) {
	id, _ := NewID(rand.Reader)
	e, err := Decode(EncodeText(id, "سلام به همه"))
	if err != nil || e.ID != id || e.Op != OpText || e.Text != "سلام به همه" {
		t.Fatalf("%+v %v", e, err)
	}
	var r Roster
	r.Version, r.Name = 7, "خانواده"
	for i := 0; i < MaxMembers; i++ {
		p, _ := identity.Generate()
		r.Members = append(r.Members, Member{Public: p.Public, Name: strings.Repeat("ن", 40)})
	}
	b, err := EncodeRoster(id, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > message.MaxContentBytes {
		t.Fatalf("a full roster (%d bytes) does not fit one message (%d)", len(b), message.MaxContentBytes)
	}
	e, err = Decode(b)
	if err != nil || e.Op != OpRoster || e.Roster.Version != 7 || e.Roster.Name != "خانواده" || len(e.Roster.Members) != MaxMembers {
		t.Fatalf("%+v %v", e, err)
	}
	if !e.Roster.Has(r.Members[3].Public.Hash()) || e.Roster.Members[3].Public != r.Members[3].Public || len(e.Roster.Members[3].Name) > MaxNameLen {
		t.Fatal("member lost")
	}
	r.Members = append(r.Members, r.Members[0])
	if _, err := EncodeRoster(id, r); err == nil {
		t.Fatal("over-full roster encoded")
	}
	for _, op := range []Op{OpLeave, OpRemoved} {
		if e, err := Decode(EncodeOp(id, op)); err != nil || e.Op != op {
			t.Fatal(op, err)
		}
	}
	// Every truncation of a roster is rejected, never a panic.
	for i := 0; i < len(b); i++ {
		Decode(b[:i])
	}
	if _, err := Decode(append(EncodeOp(id, OpLeave)[:IDSize], 9)); err == nil {
		t.Fatal("unknown op accepted")
	}
}

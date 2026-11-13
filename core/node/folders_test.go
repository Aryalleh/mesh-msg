package node_test

import (
	"strings"
	"testing"

	"github.com/Aryalleh/mesh-msg/core/node"
)

func TestFolders(t *testing.T) {
	n := startNode(t, node.Config{Name: "A"})
	if fs := n.Folders(); len(fs) != 0 {
		t.Fatal(fs)
	}
	h := strings.Repeat("ab", 16)
	if err := n.SetFolders([]node.Folder{{Name: " Work ", Chats: []string{h, h, "bad"}}, {Name: "Family"}}); err != nil {
		t.Fatal(err)
	}
	fs := n.Folders()
	if len(fs) != 2 || fs[0].Name != "Work" || len(fs[0].Chats) != 1 || fs[1].Name != "Family" || fs[1].Chats == nil {
		t.Fatal(fs)
	}
	if n.SetFolders([]node.Folder{{Name: "x"}, {Name: "X"}}) == nil {
		t.Fatal("duplicate names accepted")
	}
	if n.SetFolders([]node.Folder{{Name: ""}}) == nil {
		t.Fatal("empty name accepted")
	}
}

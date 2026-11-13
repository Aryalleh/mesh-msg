package node_test

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

func TestMergeContact(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	ali := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	saraOld := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	saraNew := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	befriend(t, ali, saraOld, "ali", "sara")
	befriend(t, ali, saraNew, "ali", "sara2")
	ev, cancel := ali.Subscribe()
	defer cancel()

	ali.Send("sara", "before")
	saraOld.Send("ali", "old reply")
	waitEvent(t, ev, "message", 20*time.Second)
	ali.Send("sara2", "after")
	newHash := saraNew.Identity().Hash().String()
	oldHash := saraOld.Identity().Hash().String()
	ali.SetFolders([]node.Folder{{Name: "Family", Chats: []string{oldHash}}})

	if _, err := ali.MergeContact("sara", "sara"); err == nil {
		t.Fatal("merged with itself")
	}
	k, err := ali.MergeContact("sara", "sara2")
	if err != nil || k != 2 {
		t.Fatalf("merge: %d %v", k, err)
	}
	ms, _ := ali.Messages("sara2", 10)
	if len(ms) != 3 || ms[0].Text != "before" || ms[1].Text != "old reply" || ms[2].Text != "after" {
		t.Fatalf("history: %+v", ms)
	}
	if _, err := ali.Messages("sara", 10); err == nil {
		t.Fatal("old contact still there")
	}
	if fs := ali.Folders(); len(fs) != 1 || len(fs[0].Chats) != 1 || fs[0].Chats[0] != newHash {
		t.Fatalf("folders: %+v", fs)
	}
}

package node_test

import (
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

func TestStorageListAndDelete(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	ali := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	sara := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	reza := startNode(t, node.Config{Relays: []node.RelayConfig{{Name: "ir-1", URL: url}}})
	befriend(t, ali, sara, "ali", "sara")
	befriend(t, ali, reza, "ali", "reza")

	photo, err := ali.SendMedia("sara", randomFile(3000), "image/jpeg", "a.jpg", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ali.SendMedia("sara", randomFile(5000), "application/pdf", "b.pdf", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ali.SendMedia("reza", randomFile(2000), "audio/ogg", "c.ogg", "", false); err != nil {
		t.Fatal(err)
	}
	u, err := ali.Storage()
	if err != nil {
		t.Fatal(err)
	}
	saraHash, rezaHash := sara.Identity().Hash().String(), reza.Identity().Hash().String()
	if u.Files != 3 || len(u.Chats) != 2 || u.Chats[0].Chat != saraHash || u.Chats[0].Files != 2 || u.Chats[0].ByKind["image"] == 0 {
		t.Fatalf("usage: %+v", u)
	}
	if _, _, err := ali.DeleteFiles(node.StorageFilter{}); err == nil {
		t.Fatal("empty filter deleted")
	}
	// Nothing older than an hour ago.
	if k, _, _ := ali.DeleteFiles(node.StorageFilter{To: time.Now().Add(-time.Hour)}); k != 0 {
		t.Fatalf("deleted %d old files", k)
	}
	// Sara's images only.
	k, freed, err := ali.DeleteFiles(node.StorageFilter{Chats: []string{saraHash}, Kinds: []string{"image"}})
	if err != nil || k != 1 || freed < 3000 {
		t.Fatalf("delete: %d %d %v", k, freed, err)
	}
	if mi, ok := ali.MediaInfo(photo.Media.ID); !ok || mi.State != "deleted" {
		t.Fatalf("photo: %+v", mi)
	}
	if err := ali.FetchMedia(photo.Media.ID); err == nil {
		t.Fatal("deleted file fetched again")
	}
	// The message stays.
	if ms, _ := ali.Messages("sara", 10); len(ms) != 2 {
		t.Fatalf("messages: %d", len(ms))
	}
	// Everything from the last hour in Reza's chat.
	if k, _, _ := ali.DeleteFiles(node.StorageFilter{Chats: []string{rezaHash}, From: time.Now().Add(-time.Hour), To: time.Now()}); k != 1 {
		t.Fatalf("reza: %d", k)
	}
	if u, _ := ali.Storage(); u.Files != 1 || u.Chats[0].ByKind["file"] == 0 {
		t.Fatalf("after: %+v", u)
	}
}

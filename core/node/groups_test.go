package node_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// waitText waits for a message event with the given text.
func waitText(t *testing.T, ch <-chan node.Event, text string, timeout time.Duration) *node.Message {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			if e.Type == "message" && e.Message.Text == text {
				return e.Message
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", text)
		}
	}
}

func TestPrivateGroupThroughRelay(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	relays := []node.RelayConfig{{Name: "ir-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays})
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays})
	reza := startNode(t, node.Config{Name: "Reza", Relays: relays})
	// Nobody has anyone as a contact: the invite link is all it takes.
	g, err := ali.CreateGroup("Family")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.URI, "meshgroup:") || g.Kind != "private" {
		t.Fatalf("group %+v", g)
	}
	for _, n := range []*node.Node{sara, reza} {
		if j, err := n.JoinGroup(g.URI); err != nil || j.Hash != g.Hash || j.Name != "Family" {
			t.Fatalf("join: %+v %v", j, err)
		}
	}
	saraEv, c1 := sara.Subscribe()
	defer c1()
	rezaEv, c2 := reza.Subscribe()
	defer c2()
	aliEv, c3 := ali.Subscribe()
	defer c3()

	if _, err := ali.SendGroup("Family", "شام ساعت ۸"); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []<-chan node.Event{saraEv, rezaEv} {
		m := waitText(t, ev, "شام ساعت ۸", 30*time.Second)
		if !m.Group || m.Peer != g.Hash || m.From != ali.Identity().Hash().String() || m.FromName != "Ali" || m.Outgoing {
			t.Fatalf("got %+v", m)
		}
	}
	if _, err := sara.SendGroup(g.Hash[:8], "باشه"); err != nil {
		t.Fatal(err)
	}
	waitText(t, aliEv, "باشه", 30*time.Second)
	waitText(t, rezaEv, "باشه", 30*time.Second)

	// History and chat list on Reza's side.
	ms, err := reza.Messages("Family", 10)
	if err != nil || len(ms) != 2 || ms[0].FromName != "Ali" || ms[1].FromName != "Sara" {
		t.Fatalf("history %v %+v", err, ms)
	}
	// A saved contact name wins over the name the sender chose.
	if _, err := reza.AddContact(sara.Identity().URI(), "sister"); err != nil {
		t.Fatal(err)
	}
	ms, _ = reza.Messages("Family", 10)
	if ms[1].FromName != "sister" {
		t.Fatalf("contact name not used: %q", ms[1].FromName)
	}
	chats, _ := reza.Chats()
	var found bool
	for _, c := range chats {
		if c.Group != nil && c.Group.Hash == g.Hash {
			found = c.Last != nil && c.Last.Text == "باشه" && c.Unread == 2
		}
	}
	if !found {
		t.Fatalf("group chat missing or wrong in %+v", chats)
	}
	if err := reza.MarkRead("Family"); err != nil {
		t.Fatal(err)
	}
	// Own messages show as "you".
	ms, _ = ali.Messages("Family", 10)
	if !ms[0].Outgoing || ms[0].FromName != "you" {
		t.Fatalf("own message %+v", ms[0])
	}

	// Reza leaves: no more messages for him.
	if err := reza.LeaveGroup("Family"); err != nil {
		t.Fatal(err)
	}
	if _, err := ali.SendGroup("Family", "after leave"); err != nil {
		t.Fatal(err)
	}
	waitText(t, saraEv, "after leave", 30*time.Second)
	time.Sleep(time.Second)
	if ms, _ := reza.Messages("", 100); len(ms) != 2 {
		t.Fatalf("left member still receives: %+v", ms)
	}
	if _, err := reza.SendGroup("Family", "x"); err == nil {
		t.Fatal("sent to a left group")
	}
}

func TestGlobalChannelAcrossBridge(t *testing.T) {
	irURL := startRelay(t, "ir-1", "ir")
	euURL := startRelay(t, "eu-1", "global")
	ali := startNode(t, node.Config{Name: "Ali", Relays: []node.RelayConfig{{Name: "ir-1", URL: irURL}}})
	sara := startNode(t, node.Config{Name: "Sara", Relays: []node.RelayConfig{{Name: "eu-1", URL: euURL}}})
	startNode(t, node.Config{
		Relays:  []node.RelayConfig{{Name: "ir-1", URL: irURL}, {Name: "eu-1", URL: euURL}},
		Bridges: []node.BridgeConfig{{A: "ir-1", B: "eu-1", EverySecs: 1, BudgetMBPerDay: 5}},
	})
	for _, n := range []*node.Node{ali, sara} {
		if g, err := n.JoinGroup("global"); err != nil || g.Kind != "global" || g.Title != "#global" {
			t.Fatalf("%+v %v", g, err)
		}
	}
	ev, cancel := sara.Subscribe()
	defer cancel()
	if _, err := ali.SendGroup("#global", "hello world from Tehran"); err != nil {
		t.Fatal(err)
	}
	if m := waitText(t, ev, "hello world from Tehran", 40*time.Second); m.FromName != "Ali" {
		t.Fatalf("got %+v", m)
	}
}

func TestRegionalChannelOverLAN(t *testing.T) {
	a := startNode(t, node.Config{Name: "A", LAN: node.LANConfig{Enabled: true}})
	time.Sleep(100 * time.Millisecond)
	b := startNode(t, node.Config{Name: "B", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	if _, err := a.JoinGroup("#Tehran"); err != nil {
		t.Fatal(err)
	}
	if g, err := b.JoinGroup("tehran"); err != nil || g.Kind != "regional" {
		t.Fatalf("%+v %v", g, err)
	}
	ev, cancel := a.Subscribe()
	defer cancel()
	if _, err := b.SendGroup("#tehran", "نانوایی سر کوچه باز است"); err != nil {
		t.Fatal(err)
	}
	waitText(t, ev, "نانوایی سر کوچه باز است", 20*time.Second)
}

func TestGroupsPersist(t *testing.T) {
	dir := t.TempDir()
	n, err := node.Open(node.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := n.CreateGroup("Team")
	n.JoinGroup("global")
	if _, err := n.CreateGroup("  "); err == nil {
		t.Fatal("unnamed group created")
	}
	if _, err := n.JoinGroup("meshgroup:bad"); err == nil {
		t.Fatal("bad link accepted")
	}
	n.Close()
	n, err = node.Open(node.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	gs, _ := n.Groups()
	if len(gs) != 2 || gs[0].Hash != g.Hash || gs[0].URI != g.URI || gs[1].Title != "#global" {
		t.Fatalf("groups after reopen: %+v", gs)
	}
	if _, err := n.SendGroup("Team", "still a member"); err != nil {
		t.Fatal(err)
	}
	if s := n.Status(); s.Groups != 2 {
		t.Fatalf("status groups %d", s.Groups)
	}
}

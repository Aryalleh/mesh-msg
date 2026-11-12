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

// waitGroup waits until n's group satisfies ok.
func waitGroup(t *testing.T, n *node.Node, ref string, ok func(node.Group) bool, timeout time.Duration) node.Group {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		gs, _ := n.Groups()
		for _, g := range gs {
			if (g.Hash == ref || g.Name == ref) && ok(g) {
				return g
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("group %q never reached the expected state: %+v", ref, gs)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func memberNames(g node.Group) string {
	var s []string
	for _, m := range g.Members {
		s = append(s, m.Name)
	}
	return strings.Join(s, ",")
}

func TestPrivateGroupMemberList(t *testing.T) {
	url := startRelay(t, "ir-1", "ir")
	relays := []node.RelayConfig{{Name: "ir-1", URL: url}}
	ali := startNode(t, node.Config{Name: "Ali", Relays: relays})
	sara := startNode(t, node.Config{Name: "Sara", Relays: relays})
	reza := startNode(t, node.Config{Name: "Reza", Relays: relays})
	// Ali knows both; Sara and Reza don't know each other.
	befriend(t, ali, sara, "ali", "sara")
	befriend(t, ali, reza, "ali", "reza")
	aliEv, c1 := ali.Subscribe()
	defer c1()
	saraEv, c2 := sara.Subscribe()
	defer c2()
	rezaEv, c3 := reza.Subscribe()
	defer c3()

	g, err := ali.CreateGroup("Family", []string{"sara", "reza"})
	if err != nil {
		t.Fatal(err)
	}
	if !g.IsOwner || g.Kind != "private" || len(g.Members) != 3 || g.URI != "" {
		t.Fatalf("group %+v", g)
	}
	// Members get the list (with the names Ali gave) and see Ali as owner.
	for _, n := range []*node.Node{sara, reza} {
		sg := waitGroup(t, n, g.Hash, func(g node.Group) bool { return len(g.Members) == 3 }, 20*time.Second)
		if sg.Name != "Family" || sg.IsOwner || sg.Owner != ali.Identity().Hash().String() {
			t.Fatalf("member's view %+v", sg)
		}
	}

	m, err := ali.SendGroup("Family", "شام ساعت ۸")
	if err != nil || m.Recipients != 2 {
		t.Fatalf("%+v %v", m, err)
	}
	for _, ev := range []<-chan node.Event{saraEv, rezaEv} {
		got := waitText(t, ev, "شام ساعت ۸", 30*time.Second)
		if !got.Group || got.Peer != g.Hash || got.From != ali.Identity().Hash().String() {
			t.Fatalf("got %+v", got)
		}
	}
	// One receipt per member; delivered when both confirmed.
	deadline := time.Now().Add(30 * time.Second)
	for {
		ms, _ := ali.Messages("Family", 10)
		if len(ms) == 1 && ms[0].DeliveredTo == 2 && ms[0].Delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not delivered to both: %+v", ms)
		}
		waitEvent(t, aliEv, "delivered", 30*time.Second)
	}

	// Sara replies: it reaches Reza, whom she has never added. Her name is the one Ali gave.
	if _, err := sara.SendGroup(g.Hash, "باشه"); err != nil {
		t.Fatal(err)
	}
	if got := waitText(t, rezaEv, "باشه", 30*time.Second); got.FromName != "sara" {
		t.Fatalf("sender name %q", got.FromName)
	}
	waitText(t, aliEv, "باشه", 30*time.Second)
	// Group members do not become contacts.
	if cs, _ := reza.Contacts(); len(cs) != 1 {
		t.Fatalf("reza's contacts %+v", cs)
	}
	// Only the owner manages members.
	if _, err := sara.RemoveMember("Family", "reza"); err == nil {
		t.Fatal("non-owner removed a member")
	}

	// Ali removes Reza: Reza is told, Sara gets the new list, Reza gets nothing more.
	if _, err := ali.RemoveMember("Family", "reza"); err != nil {
		t.Fatal(err)
	}
	waitGroup(t, reza, g.Hash, func(g node.Group) bool { return g.Removed }, 20*time.Second)
	waitGroup(t, sara, g.Hash, func(g node.Group) bool { return len(g.Members) == 2 }, 20*time.Second)
	if _, err := reza.SendGroup(g.Hash, "x"); err == nil {
		t.Fatal("removed member could send")
	}
	if m, _ := ali.SendGroup("Family", "after removal"); m.Recipients != 1 {
		t.Fatalf("recipients %d", m.Recipients)
	}
	waitText(t, saraEv, "after removal", 30*time.Second)

	// Ali adds Reza back; then Sara leaves and the owner updates the list.
	if _, err := ali.AddMembers("Family", []string{"reza"}); err != nil {
		t.Fatal(err)
	}
	waitGroup(t, reza, g.Hash, func(g node.Group) bool { return !g.Removed && len(g.Members) == 3 }, 20*time.Second)
	if err := sara.LeaveGroup("Family"); err != nil {
		t.Fatal(err)
	}
	waitGroup(t, ali, g.Hash, func(g node.Group) bool { return len(g.Members) == 2 }, 20*time.Second)
	waitGroup(t, reza, g.Hash, func(g node.Group) bool { return len(g.Members) == 2 && !strings.Contains(memberNames(g), "sara") }, 20*time.Second)
	if gs, _ := sara.Groups(); len(gs) != 0 {
		t.Fatalf("sara still has %+v", gs)
	}
}

func TestPrivateGroupOverLAN(t *testing.T) {
	a := startNode(t, node.Config{Name: "A", LAN: node.LANConfig{Enabled: true}})
	time.Sleep(100 * time.Millisecond)
	b := startNode(t, node.Config{Name: "B", LAN: node.LANConfig{Enabled: true, StaticPeers: []string{a.LANAddr()}}})
	befriend(t, a, b, "a", "b")
	ev, cancel := b.Subscribe()
	defer cancel()
	g, err := a.CreateGroup("Team", []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	waitGroup(t, b, g.Hash, func(g node.Group) bool { return g.Name == "Team" }, 20*time.Second)
	if _, err := a.SendGroup("Team", "over LAN"); err != nil {
		t.Fatal(err)
	}
	waitText(t, ev, "over LAN", 20*time.Second)
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
	friend := t.TempDir()
	f, _ := node.Open(node.Config{DataDir: friend})
	n.AddContact(f.Identity().URI(), "friend")
	f.Close()
	g, err := n.CreateGroup("Team", []string{"friend"})
	if err != nil {
		t.Fatal(err)
	}
	n.JoinGroup("global")
	if _, err := n.CreateGroup("  ", nil); err == nil {
		t.Fatal("unnamed group created")
	}
	if _, err := n.CreateGroup("x", []string{"nobody"}); err == nil {
		t.Fatal("unknown member accepted")
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
	if len(gs) != 2 || gs[0].Hash != g.Hash || len(gs[0].Members) != 2 || !gs[0].IsOwner || gs[1].Title != "#global" {
		t.Fatalf("groups after reopen: %+v", gs)
	}
	if m, err := n.SendGroup("Team", "still a member"); err != nil || m.Recipients != 1 {
		t.Fatal(m, err)
	}
	if s := n.Status(); s.Groups != 2 {
		t.Fatalf("status groups %d", s.Groups)
	}
}

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
)

// The interactive app: `mesh` with no arguments. A chat list, chats with live
// incoming messages and receipts, and every command and setting as /commands.

const appHelp = `In a chat, type and press Enter to send. Anywhere:
  /chats  (or /back)        the chat list           <number> or <name>   open a chat
  /add <meshmsg:…> [name]   add a contact            /id                  your contact link
  /group <name>             new private group        /invite              its invite link
  /join <link|#name>        join a group or channel  /leave               leave this group
  /global                   the global public chat   /region <name>       your city's / country's chat
  /rename <name>            rename this chat's contact (or /rename <contact> <name>)
  /rm                       remove this chat's contact (or /rm <contact>)
  /history [N]              show the last N messages of this chat
  /status                   links, peers, stored packets
  /settings                 all settings             /set key=value…      change them (/keys)
  /relay add <name> <url> [via] | rm <name>          /bridge add <a> <b> [MB/day] | rm <a> <b>
  /lora | /lora set k=v… | /lora plans               /ports               USB serial ports
  /backup [file]            back up your account     /restore             restore one
  /ui                       open the web UI          /clear               clear the screen
  /help                     this help                /quit (or Ctrl+D)    leave (meshd keeps running)`

type app struct {
	w io.Writer // terminal (or a buffer in tests)
	// readSecret / readLine prompt for input in the middle of a command.
	readSecret func(prompt string) (string, error)
	readLine   func(prompt string) (string, error)
	setPrompt  func(string)

	mu sync.Mutex
	// contacts holds every chat: real contacts, and groups as pseudo-contacts
	// (Name = "#global", "Family"; their details are in groups).
	contacts []node.Contact
	groups   map[string]node.Group
	listed   []node.Contact // the chat list as last shown; numbers index it
	cur      *node.Contact
	unread   map[string]int
	quit     bool
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runApp() error {
	if !isTerminal() {
		return errors.New("the app needs a terminal; see mesh -h for commands")
	}
	if !meshdRunning() {
		return notRunning(errors.New("no answer on " + dataDir))
	}
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "")
	fitTerminal(t)
	a := &app{
		w:          t,
		readSecret: t.ReadPassword,
		setPrompt:  t.SetPrompt,
		unread:     map[string]int{},
	}
	a.readLine = func(prompt string) (string, error) {
		t.SetPrompt(prompt)
		defer a.updatePrompt()
		return t.ReadLine()
	}
	t.AutoCompleteCallback = a.complete
	out = t
	askPassphrase = t.ReadPassword
	defer func() { out = os.Stdout }()

	go a.events()
	go func() { // follow terminal resizes
		for range time.Tick(time.Second) {
			fitTerminal(t)
		}
	}()

	a.banner()
	a.showChats()
	for !a.quit {
		line, err := t.ReadLine()
		if err == io.EOF {
			break
		} else if err != nil && !errors.Is(err, term.ErrPasteIndicator) {
			return err
		}
		a.handle(line)
	}
	fmt.Fprintln(t, "bye — meshd keeps running in the background")
	return nil
}

// fitTerminal tells the line editor the terminal width (80 if unknown).
func fitTerminal(t *term.Terminal) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w, h = 80, 24
	}
	t.SetSize(w, h)
}

func (a *app) printf(format string, args ...any) { fmt.Fprintf(a.w, format, args...) }

func (a *app) updatePrompt() {
	if a.setPrompt == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p := "mesh> "
	if a.cur != nil {
		p = displayName(*a.cur) + "> "
	}
	others := 0
	for _, n := range a.unread {
		others += n
	}
	if others > 0 {
		p = fmt.Sprintf("(%d new) %s", others, p)
	}
	a.setPrompt(p)
}

func displayName(c node.Contact) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Hash[:12]
}

func (a *app) banner() {
	var s node.Status
	if err := call("GET", "/status", nil, &s); err != nil {
		a.printf("%v\n", err)
		return
	}
	a.printf("mesh-msg %s · you are %s\n", version, s.Hash[:12])
	a.printf("%s\n", linkSummary(s))
}

func linkSummary(s node.Status) string {
	names := make([]string, 0, len(s.Links))
	for n := range s.Links {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := []string{}
	for _, n := range names {
		parts = append(parts, n+" "+s.Links[n])
	}
	if len(parts) == 0 {
		return "links: none — add a relay with /relay add, or turn on LAN / LoRa in /settings"
	}
	return "links: " + strings.Join(parts, ", ")
}

// loadContacts fetches the chat list (contacts and groups, most recent first).
func (a *app) loadContacts() error {
	_, err := a.loadChats()
	return err
}

func (a *app) loadChats() ([]node.Chat, error) {
	var chats []node.Chat
	if err := call("GET", "/chats", nil, &chats); err != nil {
		return nil, err
	}
	cs := make([]node.Contact, 0, len(chats))
	gs := map[string]node.Group{}
	for _, ch := range chats {
		if ch.Group != nil {
			gs[ch.Group.Hash] = *ch.Group
			cs = append(cs, node.Contact{Hash: ch.Group.Hash, Name: ch.Group.Title, URI: ch.Group.URI})
		} else {
			cs = append(cs, ch.Contact)
		}
	}
	a.mu.Lock()
	a.contacts, a.groups = cs, gs
	a.mu.Unlock()
	return chats, nil
}

func (a *app) groupOf(hash string) (node.Group, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.groups[hash]
	return g, ok
}

// showChats lists contacts, most recent conversation first, numbered.
func (a *app) showChats() {
	a.mu.Lock()
	a.cur = nil
	a.mu.Unlock()
	defer a.updatePrompt()
	chats, err := a.loadChats()
	if err != nil {
		a.printf("%v\n", err)
		return
	}
	a.mu.Lock()
	cs := append([]node.Contact(nil), a.contacts...)
	a.listed = cs
	a.unread = map[string]int{}
	a.mu.Unlock()

	if len(cs) == 0 {
		a.printf("\nNo chats yet. Share your link (/id) and add a contact: /add meshmsg:… Name\n")
		a.printf("Or join a public chat: /global, or /region tehran (any city or country name).\n")
		return
	}
	a.printf("\nChats\n")
	for i, ch := range chats {
		c := cs[i]
		line := fmt.Sprintf("%3d  %-16s", i+1, trunc(displayName(c), 16))
		if m := ch.Last; m != nil {
			arrow := "←"
			if m.Outgoing {
				arrow = "→"
			}
			if ch.Group != nil && !m.Outgoing {
				arrow += " " + trunc(senderName(*m), 10) + ":"
			}
			line += fmt.Sprintf(" %s %s  %s", arrow, trunc(oneLine(m.Text), 40), msgTime(*m).Local().Format("01-02 15:04"))
		}
		if ch.Unread > 0 {
			line += fmt.Sprintf("  ● %d new", ch.Unread)
		}
		a.printf("%s\n", line)
	}
	a.printf("Type a number or a name to open a chat, /help for everything.\n")
}

func senderName(m node.Message) string {
	if m.FromName != "" {
		return m.FromName
	}
	if len(m.From) > 8 {
		return m.From[:8]
	}
	return m.From
}

func msgTime(m node.Message) time.Time {
	if m.Outgoing {
		return m.Sent
	}
	return m.Received
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// find resolves a list number, name or hash prefix to a contact.
func (a *app) find(ref string) (*node.Contact, error) {
	if len(a.contacts) == 0 {
		a.loadContacts()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 1 || n > len(a.listed) {
			return nil, fmt.Errorf("no chat number %d", n)
		}
		c := a.listed[n-1]
		return &c, nil
	}
	var hits []node.Contact
	for _, c := range a.contacts {
		if strings.EqualFold(c.Name, ref) {
			return &c, nil
		}
		if strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(ref)) ||
			(len(ref) >= 6 && strings.HasPrefix(c.Hash, strings.ToLower(ref))) {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 1:
		return &hits[0], nil
	case 0:
		return nil, fmt.Errorf("no contact %q (/chats lists them)", ref)
	}
	names := []string{}
	for _, c := range hits {
		names = append(names, displayName(c))
	}
	return nil, fmt.Errorf("%q matches %s", ref, strings.Join(names, ", "))
}

func (a *app) open(c *node.Contact) {
	a.mu.Lock()
	a.cur = c
	delete(a.unread, c.Hash)
	a.mu.Unlock()
	kind := c.Hash[:12]
	if g, ok := a.groupOf(c.Hash); ok {
		kind = map[string]string{"private": "private group · /invite to add people", "regional": "regional public chat",
			"global": "global public chat"}[string(g.Kind)] + " · /leave to leave"
	}
	a.printf("\n── %s · %s ── /back for the chat list\n", displayName(*c), kind)
	a.history(20)
	call("POST", "/read/"+url.PathEscape(c.Hash), nil, nil)
	a.updatePrompt()
}

func (a *app) history(n int) {
	a.mu.Lock()
	c := a.cur
	a.mu.Unlock()
	if c == nil {
		a.printf("open a chat first\n")
		return
	}
	var ms []node.Message
	if err := call("GET", fmt.Sprintf("/messages?with=%s&limit=%d", url.QueryEscape(c.Hash), n), nil, &ms); err != nil {
		a.printf("%v\n", err)
		return
	}
	if len(ms) == 0 {
		a.printf("(no messages yet — say hello)\n")
	}
	for _, m := range ms {
		a.printMsg(m)
	}
}

func (a *app) printMsg(m node.Message) {
	who := "you"
	if !m.Outgoing {
		who = a.nameOf(m.Peer)
		if m.Group {
			who = senderName(m)
		}
	}
	mark := ""
	if m.Outgoing && !m.Group { // groups have no delivery receipts
		mark = " ✓"
		if m.Delivered {
			mark = " ✓✓"
		}
	}
	a.printf("%s %s: %s%s\n", msgTime(m).Local().Format("15:04"), who, m.Text, mark)
}

func (a *app) known(hash string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.contacts {
		if c.Hash == hash {
			return true
		}
	}
	return false
}

func (a *app) nameOf(hash string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.contacts {
		if c.Hash == hash {
			return displayName(c)
		}
	}
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// handle runs one line typed by the user.
func (a *app) handle(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.mu.Lock()
	cur := a.cur
	a.mu.Unlock()
	if !strings.HasPrefix(line, "/") {
		if cur != nil {
			a.send(cur, line)
			return
		}
		if c, err := a.find(line); err != nil {
			a.printf("%v\n", err)
		} else {
			a.open(c)
		}
		return
	}
	f := strings.Fields(line[1:])
	if len(f) == 0 {
		a.printf("%s\n", appHelp)
		return
	}
	cmd, args := strings.ToLower(f[0]), f[1:]
	var err error
	switch cmd {
	case "help", "h", "?":
		a.printf("%s\n", appHelp)
	case "quit", "q", "exit":
		a.quit = true
	case "chats", "back", "b", "list", "ls":
		a.showChats()
	case "open", "o", "chat":
		if len(args) == 0 {
			err = errors.New("usage: /open <number|name>")
			break
		}
		var c *node.Contact
		if c, err = a.find(strings.Join(args, " ")); err == nil {
			a.open(c)
		}
	case "history", "more":
		n := 50
		if len(args) > 0 {
			n, _ = strconv.Atoi(args[0])
		}
		a.history(n)
	case "add":
		if len(args) == 0 {
			err = errors.New("usage: /add <meshmsg:…> [name]")
			break
		}
		if strings.HasPrefix(args[0], "meshgroup:") {
			err = a.join(args[0])
			break
		}
		var c node.Contact
		if err = call("POST", "/contacts", map[string]string{"uri": args[0], "name": strings.Join(args[1:], " ")}, &c); err == nil {
			a.loadContacts()
			a.printf("added %s — opening the chat\n", displayName(c))
			a.open(&c)
		}
	case "group", "newgroup":
		if len(args) > 0 && args[0] == "new" {
			args = args[1:]
		}
		if len(args) == 0 {
			err = errors.New("usage: /group <name> — creates a private group and shows its invite link")
			break
		}
		var g node.Group
		if err = call("POST", "/groups", map[string]string{"name": strings.Join(args, " ")}, &g); err == nil {
			a.joined(g)
			a.printf("Invite people with this link (anyone holding it can read and post):\n%s\n", g.URI)
		}
	case "join":
		if len(args) == 0 {
			err = errors.New("usage: /join <meshgroup:… invite link | #region | global>")
			break
		}
		err = a.join(strings.Join(args, " "))
	case "global":
		err = a.join("global")
	case "region", "regional":
		if len(args) == 0 {
			var rs []string
			call("GET", "/regions", nil, &rs)
			a.printf("usage: /region <name> — any city, province or country; everyone using the same name meets there.\nsuggestions: %s\n", strings.Join(rs, " "))
			break
		}
		err = a.join("#" + strings.TrimPrefix(strings.Join(args, "-"), "#"))
	case "groups":
		var gs []node.Group
		if err = call("GET", "/groups", nil, &gs); err == nil {
			if len(gs) == 0 {
				a.printf("no groups — /group <name>, /join <link>, /global, /region <name>\n")
			}
			for _, g := range gs {
				a.printf("%-20s %-9s %s\n", g.Title, g.Kind, g.Hash[:12])
			}
		}
	case "invite", "share":
		ref := strings.Join(args, " ")
		if ref == "" && cur != nil {
			ref = cur.Hash
		}
		var c *node.Contact
		if c, err = a.find(ref); err != nil {
			break
		}
		if g, ok := a.groupOf(c.Hash); ok {
			if g.Kind == "private" {
				a.printf("Invite link for %s — anyone holding it can read and post, send it only to members:\n%s\n", g.Title, g.URI)
			} else {
				a.printf("%s is public: anyone can join with /join %s or this link:\n%s\n", g.Title, g.Title, g.URI)
			}
		} else {
			err = fmt.Errorf("%s is a contact, not a group (/id shows your own link)", displayName(*c))
		}
	case "leave":
		ref := strings.Join(args, " ")
		if ref == "" && cur != nil {
			ref = cur.Hash
		}
		var c *node.Contact
		if c, err = a.find(ref); err != nil {
			break
		}
		if _, ok := a.groupOf(c.Hash); !ok {
			err = fmt.Errorf("%s is not a group (/rm removes a contact)", displayName(*c))
			break
		}
		var ans string
		if ans, err = a.readLine(fmt.Sprintf("Leave %s? You stop receiving its messages. [y/N] ", displayName(*c))); err != nil {
			break
		}
		if !strings.HasPrefix(strings.ToLower(ans), "y") {
			a.printf("stayed\n")
			break
		}
		if err = call("DELETE", "/groups/"+url.PathEscape(c.Hash), nil, nil); err == nil {
			a.printf("left %s\n", displayName(*c))
			a.showChats()
		}
	case "rename":
		if cur != nil && len(args) > 0 {
			if _, ok := a.groupOf(cur.Hash); ok {
				err = errors.New("group names come from the invite link; /rename works for contacts")
				break
			}
		}
		ref, name := "", strings.Join(args, " ")
		if cur != nil {
			ref = cur.Hash
		} else if len(args) >= 2 {
			ref, name = args[0], strings.Join(args[1:], " ")
		}
		if ref == "" || name == "" {
			err = errors.New("usage: /rename <name> in a chat, or /rename <contact> <name>")
			break
		}
		if c, e := a.find(ref); e != nil {
			err = e
		} else if err = call("POST", "/contacts/"+url.PathEscape(c.Hash)+"/rename", map[string]string{"name": name}, nil); err == nil {
			a.loadContacts()
			if cur != nil {
				cur.Name = name
				a.updatePrompt()
			}
			a.printf("renamed to %s\n", name)
		}
	case "rm", "remove", "delete":
		ref := strings.Join(args, " ")
		if ref == "" && cur != nil {
			ref = cur.Hash
		}
		if ref == "" {
			err = errors.New("usage: /rm <contact> (or /rm inside a chat)")
			break
		}
		var c *node.Contact
		if c, err = a.find(ref); err != nil {
			break
		}
		if _, ok := a.groupOf(c.Hash); ok {
			err = errors.New("that is a group: /leave leaves it")
			break
		}
		var ans string
		if ans, err = a.readLine(fmt.Sprintf("Remove %s and their messages? [y/N] ", displayName(*c))); err != nil {
			break
		}
		if !strings.HasPrefix(strings.ToLower(ans), "y") {
			a.printf("kept\n")
			break
		}
		if err = call("DELETE", "/contacts/"+url.PathEscape(c.Hash), nil, nil); err == nil {
			a.printf("removed %s\n", displayName(*c))
			a.showChats()
		}
	case "settings", "config":
		err = configCmd(nil)
		if err == nil {
			a.printf("change with /set key=value… (/keys lists them), /relay, /bridge, /lora set\n")
		}
	case "set":
		err = configCmd(append([]string{"set"}, args...))
		a.updatePrompt()
	case "keys":
		a.printf("%s\n", configKeys)
	case "backup":
		err = a.backup(args)
	case "restore":
		err = a.restore()
	case "clear", "cls":
		a.printf("\x1b[2J\x1b[H")
	case "send":
		if len(args) < 2 {
			err = errors.New("usage: /send <contact> <text…>")
			break
		}
		var c *node.Contact
		if c, err = a.find(args[0]); err == nil {
			a.send(c, strings.Join(args[1:], " "))
		}
	case "status", "id", "contacts", "relay", "relays", "bridge", "bridges", "lora", "ports", "ui", "version", "read":
		err = run(append([]string{cmd}, args...))
	default:
		err = fmt.Errorf("unknown command /%s — /help lists them", cmd)
	}
	if err != nil {
		a.printf("%v\n", err)
	}
}

func (a *app) join(link string) error {
	var g node.Group
	if err := call("POST", "/groups", map[string]string{"link": link}, &g); err != nil {
		return err
	}
	a.joined(g)
	if g.Kind != "private" {
		a.printf("This chat is public: anyone can read it and post. Don't share anything private here.\n")
	}
	return nil
}

// joined opens a group that was just created or joined.
func (a *app) joined(g node.Group) {
	a.loadContacts()
	a.printf("joined %s\n", g.Title)
	a.open(&node.Contact{Hash: g.Hash, Name: g.Title, URI: g.URI})
}

func (a *app) send(c *node.Contact, text string) {
	var m node.Message
	if err := call("POST", "/send", map[string]string{"to": c.Hash, "text": text}, &m); err != nil {
		a.printf("not sent: %v\n", err)
		return
	}
	a.mu.Lock()
	open := a.cur != nil && a.cur.Hash == c.Hash
	a.mu.Unlock()
	if open {
		a.printMsg(m)
	} else {
		a.printf("→ %s: %s ✓\n", displayName(*c), text)
	}
}

func (a *app) backup(args []string) error {
	a.printf("Choose a passphrase (at least %d characters). Without it the backup is useless; with it, anyone holding the backup is you.\n", identity.MinPassphraseLength)
	pass, err := a.readSecret("Passphrase: ")
	if err != nil {
		return err
	}
	again, err := a.readSecret("Again: ")
	if err != nil {
		return err
	}
	if again != pass {
		return errors.New("passphrases do not match")
	}
	var r struct{ Backup string }
	if err := call("POST", "/identity/export", map[string]string{"passphrase": pass}, &r); err != nil {
		return err
	}
	if len(args) > 0 {
		if err := os.WriteFile(args[0], []byte(r.Backup+"\n"), 0o600); err != nil {
			return err
		}
		a.printf("written to %s\n", args[0])
		return nil
	}
	a.printf("%s\nKeep it somewhere safe (it restores here and on Android).\n", r.Backup)
	return nil
}

func (a *app) restore() error {
	a.printf("This switches meshd to the account in the backup (the current key is kept on disk).\n")
	text, err := a.readLine("Paste the backup (meshkey1:…), or a file path: ")
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("cancelled")
	}
	if !strings.HasPrefix(text, "meshkey") {
		b, err := os.ReadFile(text)
		if err != nil {
			return err
		}
		text = string(b)
	}
	pass, err := a.readSecret("Passphrase: ")
	if err != nil {
		return err
	}
	if err := restoreViaDaemon(text, pass); err != nil {
		return err
	}
	a.mu.Lock()
	a.unread = map[string]int{}
	a.mu.Unlock()
	a.showChats()
	return nil
}

// events follows meshd's event stream and reconnects when meshd restarts
// its node (settings applied) or itself.
func (a *app) events() {
	for {
		req, _ := http.NewRequest("GET", "http://meshd/events", nil)
		resp, err := client.Do(req)
		if err == nil {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				data, ok := strings.CutPrefix(sc.Text(), "data: ")
				if !ok {
					continue
				}
				var e node.Event
				if json.Unmarshal([]byte(data), &e) == nil {
					a.event(e)
				}
			}
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *app) event(e node.Event) {
	switch e.Type {
	case "message":
		m := e.Message
		if m == nil || m.Outgoing {
			return
		}
		if !a.known(m.Peer) {
			a.loadContacts()
		}
		a.mu.Lock()
		open := a.cur != nil && a.cur.Hash == m.Peer
		if !open {
			a.unread[m.Peer]++
		}
		a.mu.Unlock()
		if open {
			a.printMsg(*m)
		} else {
			a.updatePrompt() // repainted by the print below
			who := a.nameOf(m.Peer)
			if m.Group {
				who += " · " + senderName(*m)
			}
			a.printf("● %s: %s\n", who, trunc(oneLine(m.Text), 60))
		}
	case "delivered":
		a.mu.Lock()
		open := a.cur != nil
		a.mu.Unlock()
		if open {
			a.printf("   ✓✓ delivered\n")
		}
	case "contact", "group":
		a.loadContacts()
	case "link":
		a.printf("   link %s\n", e.Info)
	}
}

// complete offers /commands and contact names on Tab.
func (a *app) complete(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' || pos != len(line) {
		return "", 0, false
	}
	var cands []string
	prefix, word := "", line
	if i := strings.LastIndexByte(line, ' '); i >= 0 {
		prefix, word = line[:i+1], line[i+1:]
	}
	if prefix == "" && strings.HasPrefix(word, "/") {
		for _, c := range []string{"/chats", "/back", "/open", "/add", "/group", "/join", "/global", "/region", "/invite", "/leave", "/groups", "/rename", "/rm", "/history", "/id", "/status",
			"/settings", "/set", "/keys", "/relay", "/bridge", "/lora", "/ports", "/backup", "/restore", "/ui", "/clear", "/help", "/quit", "/send"} {
			if strings.HasPrefix(c, word) {
				cands = append(cands, c)
			}
		}
	} else {
		a.mu.Lock()
		for _, c := range a.contacts {
			if c.Name != "" && strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(word)) {
				cands = append(cands, c.Name)
			}
		}
		a.mu.Unlock()
	}
	if len(cands) != 1 {
		return "", 0, false
	}
	nl := prefix + cands[0] + " "
	return nl, len(nl), true
}

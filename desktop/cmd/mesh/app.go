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

	mu       sync.Mutex
	contacts []node.Contact
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

func (a *app) loadContacts() error {
	var cs []node.Contact
	if err := call("GET", "/contacts", nil, &cs); err != nil {
		return err
	}
	a.mu.Lock()
	a.contacts = cs
	a.mu.Unlock()
	return nil
}

// showChats lists contacts, most recent conversation first, numbered.
func (a *app) showChats() {
	a.mu.Lock()
	a.cur = nil
	a.mu.Unlock()
	defer a.updatePrompt()
	if err := a.loadContacts(); err != nil {
		a.printf("%v\n", err)
		return
	}
	var ms []node.Message
	call("GET", "/messages?limit=1000", nil, &ms)
	last := map[string]node.Message{}
	for _, m := range ms {
		last[m.Peer] = m // oldest first, so the last one wins
	}
	a.mu.Lock()
	sort.SliceStable(a.contacts, func(i, j int) bool {
		return msgTime(last[a.contacts[i].Hash]).After(msgTime(last[a.contacts[j].Hash]))
	})
	cs := append([]node.Contact(nil), a.contacts...)
	a.listed = cs
	unread := map[string]int{}
	for k, v := range a.unread {
		unread[k] = v
	}
	a.mu.Unlock()

	if len(cs) == 0 {
		a.printf("\nNo chats yet. Share your link (/id) and add a contact: /add meshmsg:… Name\n")
		return
	}
	a.printf("\nChats\n")
	for i, c := range cs {
		line := fmt.Sprintf("%3d  %-16s", i+1, trunc(displayName(c), 16))
		if m, ok := last[c.Hash]; ok {
			arrow := "←"
			if m.Outgoing {
				arrow = "→"
			}
			line += fmt.Sprintf(" %s %s  %s", arrow, trunc(oneLine(m.Text), 40), msgTime(m).Local().Format("01-02 15:04"))
		}
		if n := unread[c.Hash]; n > 0 {
			line += fmt.Sprintf("  ● %d new", n)
		}
		a.printf("%s\n", line)
	}
	a.printf("Type a number or a name to open a chat, /help for everything.\n")
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
	a.printf("\n── %s · %s ── /back for the chat list\n", displayName(*c), c.Hash[:12])
	a.history(20)
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
	}
	mark := ""
	if m.Outgoing {
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
		var c node.Contact
		if err = call("POST", "/contacts", map[string]string{"uri": args[0], "name": strings.Join(args[1:], " ")}, &c); err == nil {
			a.loadContacts()
			a.printf("added %s — opening the chat\n", displayName(c))
			a.open(&c)
		}
	case "rename":
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
			a.printf("● %s: %s\n", a.nameOf(m.Peer), trunc(oneLine(m.Text), 60))
		}
	case "delivered":
		a.mu.Lock()
		open := a.cur != nil
		a.mu.Unlock()
		if open {
			a.printf("   ✓✓ delivered\n")
		}
	case "contact":
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
		for _, c := range []string{"/chats", "/back", "/open", "/add", "/rename", "/rm", "/history", "/id", "/status",
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

// Command mesh is the command-line client for meshd.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/desktop/daemon"
)

const usage = `mesh — command-line client for meshd

Usage:
  mesh                           open the interactive app (chats, contacts, settings)
  mesh status                    links, peers, stored packets
  mesh id                        your contact link (share it)
  mesh contacts                  list contacts
  mesh add <meshmsg:…> [name]    add a contact
  mesh rename <contact> <name>   rename a contact
  mesh rm <contact>              remove a contact
  mesh send <chat> <text…>       send a message ("-" reads text from stdin)
  mesh groups                    joined groups and public chats
  mesh group new <name> [contact…]  create a private group with these contacts
                                 (each message goes to every member separately, ✓✓ per member)
  mesh group members <group>     a private group's members
  mesh group add <group> <contact…>   add members (owner only)
  mesh group remove <group> <member>  remove a member (owner only)
  mesh group join <#name|global> join a public chat: global, or a region such as #tehran
  mesh group leave <group>       leave a group
  mesh regions                   suggested regional chats
  mesh read [contact] [-n N]     show messages
  mesh watch                     stream incoming messages and events
  mesh ui                        open the web UI in your browser
  mesh lora                      attached LoRa node(s): status and config
  mesh lora set key=value…       configure the node: role=client|repeater|gateway
                                 region=EU_868… preset=FAST|BALANCED|LONG
                                 freq=869.525 (MHz) power=14 (dBm) name=… announce=30 (min)
  mesh lora plans                region frequency plans and presets
  mesh config                    show all settings
  mesh config set key=value…     change settings (mesh config keys lists them)
  mesh relay add <name> <url> [via] | rm <name>
  mesh relay known | forget <name>  relays found automatically (official list, relays, nearby devices)
                                 relays; via = direct | tor | socks5://host:port
  mesh bridge add <a> <b> [MB/day] | rm <a> <b>
                                 sync two relays that can't reach each other
  mesh ports                     USB serial ports (for the LoRa node)
  mesh version                   print the version
  mesh identity export [-o file] back up your account key, sealed with a passphrase
  mesh identity import [-replace] [file|-]
                                 restore an account from a backup (meshd must be stopped)

<contact> is a name, a hash or a hash prefix (6+ chars); <chat> is a contact or a group
("#global", "#tehran", a group's name).
Flags: -data <dir> (default ~/.local/share/meshd)
`

var (
	out     io.Writer = os.Stdout // command output; the app points it at the terminal
	client  *http.Client
	dataDir string
	version = "0.0.0-dev" // set by the release build (-X main.version=…)
)

func main() {
	_, defData := daemon.DefaultPaths()
	fs := flag.NewFlagSet("mesh", flag.ExitOnError)
	fs.StringVar(&dataDir, "data", defData, "meshd data directory")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.Parse(os.Args[1:])
	args := fs.Args()
	sock := daemon.SocketPath(dataDir)
	client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	if len(args) == 0 {
		if !isTerminal() {
			fs.Usage()
			os.Exit(2)
		}
		args = []string{"app"}
	}
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, "mesh:", err)
		os.Exit(1)
	}
}

func call(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://meshd"+path, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return notRunning(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// notRunning explains how to start meshd when its socket is not there.
func notRunning(err error) error {
	return fmt.Errorf(`meshd is not running (%v)
start it and leave it running, then retry:
  systemctl --user start meshd    # if installed as a service (see README)
  meshd &                         # or in the background of this terminal
  meshd                           # or in a second terminal`, err)
}

func run(args []string) error {
	switch cmd, rest := args[0], args[1:]; cmd {
	case "status":
		var s node.Status
		if err := call("GET", "/status", nil, &s); err != nil {
			return err
		}
		fmt.Fprintf(out, "identity  %s\nuptime    %s\nstored    %d packets\nknown     %d nodes\ncontacts  %d\ngateway   %v\n",
			s.Hash, s.Uptime, s.Stored, s.Routes, s.Contacts, s.Gateway)
		fmt.Fprintf(out, "router    in %d, delivered %d, forwarded %d, duplicates %d, purged %d\n",
			s.Router.In, s.Router.Delivered, s.Router.Forwarded, s.Router.Dup, s.Router.Purged)
		for name, st := range s.Links {
			fmt.Fprintf(out, "link      %-20s %s\n", name, st)
		}
		for _, b := range s.Bridges {
			fmt.Fprintf(out, "bridge    %s\n", b)
		}
	case "app":
		return runApp()
	case "groups":
		return groupCmd([]string{"list"})
	case "group":
		return groupCmd(rest)
	case "regions":
		var rs []string
		if err := call("GET", "/regions", nil, &rs); err != nil {
			return err
		}
		fmt.Fprintln(out, "Regional public chats (any other name works too): mesh group join #<name>")
		fmt.Fprintln(out, strings.Join(rs, " "))
	case "config":
		return configCmd(rest)
	case "relay", "relays":
		return relayCmd(rest)
	case "bridge", "bridges":
		return bridgeCmd(rest)
	case "ports":
		var ps []string
		if err := call("GET", "/serial-ports", nil, &ps); err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Fprintln(out, "no USB serial ports found (plug in the LoRa node)")
		}
		for _, p := range ps {
			fmt.Fprintln(out, p)
		}
	case "lora":
		return loraCmd(rest)
	case "identity":
		return identityCmd(rest)
	case "version":
		fmt.Fprintln(out, "mesh", version)
	case "id":
		var id map[string]string
		if err := call("GET", "/id", nil, &id); err != nil {
			return err
		}
		fmt.Fprintln(out, id["uri"])
	case "contacts":
		var cs []node.Contact
		if err := call("GET", "/contacts", nil, &cs); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, c := range cs {
			name := c.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Fprintf(tw, "%s\t%s\n", name, c.Hash)
		}
		tw.Flush()
	case "add":
		if len(rest) < 1 {
			return errors.New("usage: mesh add <meshmsg:…> [name]")
		}
		var c node.Contact
		if err := call("POST", "/contacts", map[string]string{"uri": rest[0], "name": strings.Join(rest[1:], " ")}, &c); err != nil {
			return err
		}
		fmt.Fprintf(out, "added %s (%s)\n", c.Name, c.Hash)
	case "rename":
		if len(rest) < 2 {
			return errors.New("usage: mesh rename <contact> <name>")
		}
		return call("POST", "/contacts/"+url.PathEscape(rest[0])+"/rename", map[string]string{"name": strings.Join(rest[1:], " ")}, nil)
	case "rm":
		if len(rest) != 1 {
			return errors.New("usage: mesh rm <contact>")
		}
		return call("DELETE", "/contacts/"+url.PathEscape(rest[0]), nil, nil)
	case "send":
		if len(rest) < 2 {
			return errors.New("usage: mesh send <contact> <text…>")
		}
		text := strings.Join(rest[1:], " ")
		if text == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = strings.TrimRight(string(b), "\n")
		}
		var m node.Message
		if err := call("POST", "/send", map[string]string{"to": rest[0], "text": text}, &m); err != nil {
			return err
		}
		fmt.Fprintln(out, "queued", m.ID)
	case "read":
		fs := flag.NewFlagSet("read", flag.ExitOnError)
		n := fs.Int("n", 50, "number of messages")
		with := ""
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			with, rest = rest[0], rest[1:]
		}
		fs.Parse(rest)
		var ms []node.Message
		if err := call("GET", fmt.Sprintf("/messages?with=%s&limit=%d", url.QueryEscape(with), *n), nil, &ms); err != nil {
			return err
		}
		names := contactNames()
		for _, m := range ms {
			printMessage(m, names)
		}
	case "watch":
		return watch()
	case "ui":
		var id map[string]string
		if err := call("GET", "/id", nil, &id); err != nil {
			return err
		}
		if id["ui"] == "" {
			return errors.New("web UI disabled (set ui = \"127.0.0.1:7878\" in config.toml)")
		}
		fmt.Fprintln(out, id["ui"])
		openBrowser(id["ui"])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
	return nil
}

func contactNames() map[string]string {
	var cs []node.Contact
	call("GET", "/contacts", nil, &cs)
	m := map[string]string{}
	var gs []node.Group
	call("GET", "/groups", nil, &gs)
	for _, g := range gs {
		m[g.Hash] = g.Title
	}
	for _, c := range cs {
		if c.Name != "" {
			m[c.Hash] = c.Name
		} else {
			m[c.Hash] = c.Hash[:12]
		}
	}
	return m
}

func printMessage(m node.Message, names map[string]string) {
	who := names[m.Peer]
	if who == "" {
		who = m.Peer[:12]
	}
	if m.Group {
		if m.Outgoing {
			fmt.Fprintf(out, "%s %s you: %s%s\n", m.Sent.Local().Format("01-02 15:04"), who, m.Text, groupMark(m))
		} else {
			fmt.Fprintf(out, "%s %s %s: %s\n", m.Received.Local().Format("01-02 15:04"), who, senderName(m), m.Text)
		}
		return
	}
	ts, arrow, mark := m.Received, "←", ""
	if m.Outgoing {
		ts, arrow, mark = m.Sent, "→", " ✓"
		if m.Delivered {
			mark = " ✓✓"
		}
	}
	fmt.Fprintf(out, "%s %s %s: %s%s\n", ts.Local().Format("01-02 15:04"), arrow, who, m.Text, mark)
}

func watch() error {
	req, _ := http.NewRequest("GET", "http://meshd/events", nil)
	resp, err := client.Do(req)
	if err != nil {
		return notRunning(err)
	}
	defer resp.Body.Close()
	names := contactNames()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var e node.Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Type {
		case "message":
			if _, ok := names[e.Message.Peer]; !ok {
				names = contactNames()
			}
			printMessage(*e.Message, names)
		default:
			fmt.Fprintf(out, "%s [%s] %s\n", time.Now().Format("15:04:05"), e.Type, e.Info)
		}
	}
	return sc.Err()
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	cmd.Start()
}

func groupCmd(args []string) error {
	const usage = "usage: mesh group new <name> [contact…] | members <group> | add <group> <contact…> | remove <group> <member> | join <#region|global> | leave <group> | list"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "list", "ls":
		var gs []node.Group
		if err := call("GET", "/groups", nil, &gs); err != nil {
			return err
		}
		if len(gs) == 0 {
			fmt.Fprintln(out, "no groups (mesh group new <name>, mesh group join global)")
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, g := range gs {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", g.Title, g.Kind, g.Hash)
		}
		return tw.Flush()
	case "new", "create":
		if len(args) < 2 {
			return errors.New(usage)
		}
		var g node.Group
		if err := call("POST", "/groups", map[string]any{"name": args[1], "members": args[2:]}, &g); err != nil {
			return err
		}
		fmt.Fprintf(out, "created %s with %d members\n", g.Title, len(g.Members))
		printMembers(g)
	case "join":
		if len(args) < 2 {
			return errors.New(usage)
		}
		var g node.Group
		if err := call("POST", "/groups", map[string]string{"link": strings.Join(args[1:], " ")}, &g); err != nil {
			return err
		}
		fmt.Fprintf(out, "joined %s (%s)\n", g.Title, g.Kind)
		if g.Kind != "private" {
			fmt.Fprintln(out, "this chat is public: anyone can read it and post")
		}
	case "members", "invite", "link":
		if len(args) < 2 {
			return errors.New(usage)
		}
		g, err := findGroup(strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		if g.Kind != "private" {
			fmt.Fprintf(out, "%s is public: anyone can join with: mesh group join %s\n", g.Title, g.Title)
			return nil
		}
		printMembers(g)
	case "add":
		if len(args) < 3 {
			return errors.New(usage)
		}
		var g node.Group
		if err := call("POST", "/groups/"+url.PathEscape(args[1])+"/members", map[string]any{"add": args[2:]}, &g); err != nil {
			return err
		}
		printMembers(g)
	case "remove", "kick":
		if len(args) != 3 {
			return errors.New(usage)
		}
		var g node.Group
		if err := call("DELETE", "/groups/"+url.PathEscape(args[1])+"/members/"+url.PathEscape(args[2]), nil, &g); err != nil {
			return err
		}
		printMembers(g)
	case "leave", "rm":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return call("DELETE", "/groups/"+url.PathEscape(strings.Join(args[1:], " ")), nil, nil)
	default:
		return errors.New(usage)
	}
	return nil
}

func findGroup(ref string) (node.Group, error) {
	var gs []node.Group
	if err := call("GET", "/groups", nil, &gs); err != nil {
		return node.Group{}, err
	}
	for _, g := range gs {
		if g.Hash == ref || strings.EqualFold(g.Title, ref) || strings.EqualFold(g.Name, ref) || (len(ref) >= 6 && strings.HasPrefix(g.Hash, ref)) {
			return g, nil
		}
	}
	return node.Group{}, fmt.Errorf("no group %q", ref)
}

func printMembers(g node.Group) {
	if g.Removed {
		fmt.Fprintf(out, "%s: you were removed from this group\n", g.Title)
	}
	for _, m := range g.Members {
		name := m.Name
		if name == "" {
			name = m.Hash[:12]
		}
		tag := ""
		if m.Owner {
			tag += " (owner)"
		}
		if m.You {
			tag += " (you)"
		}
		fmt.Fprintf(out, "  %-20s %s%s\n", name, m.Hash[:12], tag)
	}
}

// groupMark is the delivery mark of an outgoing group message: ✓✓ when every
// member confirmed it, else ✓ n/m (public chats have no receipts).
func groupMark(m node.Message) string {
	switch {
	case m.Recipients == 0:
		return ""
	case m.Delivered || m.DeliveredTo >= m.Recipients:
		return " ✓✓"
	}
	return fmt.Sprintf(" ✓ %d/%d", m.DeliveredTo, m.Recipients)
}

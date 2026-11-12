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
  mesh status                    links, peers, stored packets
  mesh id                        your contact link (share it)
  mesh contacts                  list contacts
  mesh add <meshmsg:…> [name]    add a contact
  mesh rename <contact> <name>   rename a contact
  mesh rm <contact>              remove a contact
  mesh send <contact> <text…>    send a message ("-" reads text from stdin)
  mesh read [contact] [-n N]     show messages
  mesh watch                     stream incoming messages and events
  mesh ui                        open the web UI in your browser
  mesh lora                      attached LoRa node(s): status and config
  mesh lora set key=value…       configure the node: role=client|repeater|gateway
                                 region=EU_868… preset=FAST|BALANCED|LONG
                                 freq=869.525 (MHz) power=14 (dBm) name=… announce=30 (min)
  mesh lora plans                region frequency plans and presets
  mesh version                   print the version
  mesh identity export [-o file] back up your account key, sealed with a passphrase
  mesh identity import [-replace] [file|-]
                                 restore an account from a backup (meshd must be stopped)

<contact> is a name, a hash or a hash prefix (6+ chars).
Flags: -data <dir> (default ~/.local/share/meshd)
`

var (
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
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	sock := daemon.SocketPath(dataDir)
	client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
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
		return fmt.Errorf("cannot reach meshd (is it running?): %w", err)
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

func run(args []string) error {
	switch cmd, rest := args[0], args[1:]; cmd {
	case "status":
		var s node.Status
		if err := call("GET", "/status", nil, &s); err != nil {
			return err
		}
		fmt.Printf("identity  %s\nuptime    %s\nstored    %d packets\nknown     %d nodes\ncontacts  %d\ngateway   %v\n",
			s.Hash, s.Uptime, s.Stored, s.Routes, s.Contacts, s.Gateway)
		fmt.Printf("router    in %d, delivered %d, forwarded %d, duplicates %d, purged %d\n",
			s.Router.In, s.Router.Delivered, s.Router.Forwarded, s.Router.Dup, s.Router.Purged)
		for name, st := range s.Links {
			fmt.Printf("link      %-20s %s\n", name, st)
		}
		for _, b := range s.Bridges {
			fmt.Printf("bridge    %s\n", b)
		}
	case "lora":
		return loraCmd(rest)
	case "identity":
		return identityCmd(rest)
	case "version":
		fmt.Println("mesh", version)
	case "id":
		var id map[string]string
		if err := call("GET", "/id", nil, &id); err != nil {
			return err
		}
		fmt.Println(id["uri"])
	case "contacts":
		var cs []node.Contact
		if err := call("GET", "/contacts", nil, &cs); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
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
		fmt.Printf("added %s (%s)\n", c.Name, c.Hash)
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
		fmt.Println("queued", m.ID)
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
		fmt.Println(id["ui"])
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
	ts, arrow, mark := m.Received, "←", ""
	if m.Outgoing {
		ts, arrow, mark = m.Sent, "→", " ✓"
		if m.Delivered {
			mark = " ✓✓"
		}
	}
	fmt.Printf("%s %s %s: %s%s\n", ts.Local().Format("01-02 15:04"), arrow, who, m.Text, mark)
}

func watch() error {
	req, _ := http.NewRequest("GET", "http://meshd/events", nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach meshd: %w", err)
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
			fmt.Printf("%s [%s] %s\n", time.Now().Format("15:04:05"), e.Type, e.Info)
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

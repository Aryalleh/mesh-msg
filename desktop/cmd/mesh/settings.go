package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/desktop/daemon"
)

// Settings from the command line: the same GET/PUT /config the web UI uses,
// so a change is applied at once and written to config.toml.

const configKeys = `keys for mesh config set key=value…:
  name=…                 display name in announces (empty = none)
  delegate=on|off        let gateways fetch your messages from relays
  announce=30            announce interval, minutes
  gateway=on|off         share your internet with the mesh
  gateway.mode=targeted|carrier
  gateway.quota=512      KB per hour
  lan=on|off             mesh with meshd nodes on the local network
  lan.port=0             TCP port (0 = random)
  lan.discovery=on|off   find LAN peers by multicast
  lan.peers=a:1,b:2      fixed LAN peers (host:port, comma separated; empty = none)
  lora=on|off            use a LoRa node on USB
  lora.port=/dev/ttyACM0 (mesh ports lists them)
  discovery=on|off       find relays automatically and share yours (mesh relay known)
relays: mesh relay add|rm …   bridges: mesh bridge add|rm …   the node itself: mesh lora set …`

func getConfig() (*daemon.Config, error) {
	var c daemon.Config
	if err := call("GET", "/config", nil, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func putConfig(c *daemon.Config) error {
	if err := call("PUT", "/config", c, nil); err != nil {
		return err
	}
	fmt.Fprintln(out, "saved and applied")
	return nil
}

func configCmd(args []string) error {
	if len(args) == 0 || args[0] == "show" {
		c, err := getConfig()
		if err != nil {
			return err
		}
		printConfig(c)
		return nil
	}
	switch args[0] {
	case "keys":
		fmt.Fprintln(out, configKeys)
		return nil
	case "set":
		if len(args) < 2 {
			return errors.New(configKeys)
		}
		c, err := getConfig()
		if err != nil {
			return err
		}
		if err := applyKeys(c, args[1:]); err != nil {
			return err
		}
		return putConfig(c)
	}
	return fmt.Errorf("unknown: mesh config %s\n%s", args[0], configKeys)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func parseBool(k, v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s: want on or off, got %q", k, v)
}

func parseInt(k, v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: want a number ≥ 0, got %q", k, v)
	}
	return n, nil
}

// applyKeys changes c from key=value pairs (see configKeys).
func applyKeys(c *daemon.Config, kvs []string) error {
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("%q: want key=value\n%s", kv, configKeys)
		}
		var err error
		switch k = strings.ToLower(strings.TrimSpace(k)); k {
		case "name":
			c.Name = v
		case "delegate":
			c.Delegate, err = parseBool(k, v)
		case "announce", "announce_minutes":
			c.AnnounceMinutes, err = parseInt(k, v)
		case "discovery":
			var on bool
			on, err = parseBool(k, v)
			c.Discovery.Off = !on
		case "gateway":
			c.Gateway.Enabled, err = parseBool(k, v)
		case "gateway.mode":
			if v != "targeted" && v != "carrier" {
				return fmt.Errorf("gateway.mode: targeted or carrier, got %q", v)
			}
			c.Gateway.Mode = v
		case "gateway.quota":
			c.Gateway.QuotaKBPerHour, err = parseInt(k, v)
		case "lan":
			c.LAN.Enabled, err = parseBool(k, v)
		case "lan.port":
			c.LAN.Port, err = parseInt(k, v)
		case "lan.discovery":
			c.LAN.Discovery, err = parseBool(k, v)
		case "lan.peers":
			c.LAN.StaticPeers = []string{}
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					c.LAN.StaticPeers = append(c.LAN.StaticPeers, p)
				}
			}
		case "lora":
			c.LoRa.Enabled, err = parseBool(k, v)
		case "lora.port":
			c.LoRa.Port = v
		default:
			return fmt.Errorf("unknown key %q\n%s", k, configKeys)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func printConfig(c *daemon.Config) {
	name := c.Name
	if name == "" {
		name = "(none)"
	}
	fmt.Fprintf(out, "name          %s\n", name)
	fmt.Fprintf(out, "delegate      %s\n", onOff(c.Delegate))
	fmt.Fprintf(out, "announce      every %d min\n", c.AnnounceMinutes)
	fmt.Fprintf(out, "gateway       %s, %s, %d KB/h\n", onOff(c.Gateway.Enabled), c.Gateway.Mode, c.Gateway.QuotaKBPerHour)
	fmt.Fprintf(out, "discovery     %s (relays from the official list, other relays and nearby devices)\n", onOff(!c.Discovery.Off))
	peers := strings.Join(c.LAN.StaticPeers, ", ")
	if peers == "" {
		peers = "none"
	}
	fmt.Fprintf(out, "lan           %s, port %d, discovery %s, fixed peers: %s\n", onOff(c.LAN.Enabled), c.LAN.Port, onOff(c.LAN.Discovery), peers)
	port := c.LoRa.Port
	if port == "" {
		port = "no port set"
	}
	fmt.Fprintf(out, "lora          %s, %s\n", onOff(c.LoRa.Enabled), port)
	if len(c.Relays) == 0 {
		fmt.Fprintln(out, "relays        none (add one: mesh relay add <name> <url>)")
	}
	for _, r := range c.Relays {
		fmt.Fprintf(out, "relay         %-10s %s via %s\n", r.Name, r.URL, viaOf(r))
	}
	for _, b := range c.Bridges {
		budget := "unlimited"
		if b.BudgetMBPerDay > 0 {
			budget = fmt.Sprintf("%d MB/day", b.BudgetMBPerDay)
		}
		fmt.Fprintf(out, "bridge        %s ⇄ %s, %s\n", b.A, b.B, budget)
	}
	if c.UI != "" {
		fmt.Fprintf(out, "web ui        %s (mesh ui)\n", c.UI)
	}
	if c.Path != "" {
		fmt.Fprintf(out, "file          %s\n", c.Path)
	}
}

func viaOf(r node.RelayConfig) string {
	if r.Via == "" {
		return "direct"
	}
	return r.Via
}

func relayCmd(args []string) error {
	const usage = "usage: mesh relay [list] | known | forget <name|url> | add <name> <url> [direct|tor|socks5://host:port] | rm <name>"
	c, err := getConfig()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		if len(c.Relays) == 0 {
			fmt.Fprintln(out, "no relays")
		}
		for _, r := range c.Relays {
			fmt.Fprintf(out, "%-10s %s via %s\n", r.Name, r.URL, viaOf(r))
		}
		return nil
	}
	switch args[0] {
	case "known", "discovered":
		var ks []node.KnownRelay
		if err := call("GET", "/relays/known", nil, &ks); err != nil {
			return err
		}
		if len(ks) == 0 {
			fmt.Fprintln(out, "no relays discovered yet (or discovery is off)")
		}
		for _, k := range ks {
			use := ""
			switch {
			case k.Config:
				use = "configured"
			case k.Active:
				use = "in use"
			}
			fmt.Fprintf(out, "%-10s %-28s %-9s %-10s %s\n", k.Name, k.URL, k.State, "from "+k.Source, use)
		}
		return nil
	case "forget":
		if len(args) != 2 {
			return errors.New(usage)
		}
		return call("POST", "/relays/forget", map[string]string{"relay": args[1]}, nil)
	case "add":
		if len(args) < 3 || len(args) > 4 {
			return errors.New(usage)
		}
		r := node.RelayConfig{Name: args[1], URL: args[2], Via: "direct"}
		if len(args) == 4 {
			r.Via = args[3]
		}
		replaced := false
		for i := range c.Relays {
			if c.Relays[i].Name == r.Name {
				r.Token = c.Relays[i].Token
				c.Relays[i], replaced = r, true
			}
		}
		if !replaced {
			c.Relays = append(c.Relays, r)
		}
		return putConfig(c)
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New(usage)
		}
		kept := c.Relays[:0]
		for _, r := range c.Relays {
			if r.Name != args[1] {
				kept = append(kept, r)
			}
		}
		if len(kept) == len(c.Relays) {
			return fmt.Errorf("no relay named %q", args[1])
		}
		c.Relays = kept
		return putConfig(c)
	}
	return errors.New(usage)
}

func bridgeCmd(args []string) error {
	const usage = "usage: mesh bridge [list] | add <relay a> <relay b> [MB/day] | rm <relay a> <relay b>"
	c, err := getConfig()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		if len(c.Bridges) == 0 {
			fmt.Fprintln(out, "no bridges")
		}
		for _, b := range c.Bridges {
			fmt.Fprintf(out, "%s ⇄ %s  %d MB/day (0 = unlimited)\n", b.A, b.B, b.BudgetMBPerDay)
		}
		return nil
	}
	same := func(b node.BridgeConfig, a, z string) bool {
		return (b.A == a && b.B == z) || (b.A == z && b.B == a)
	}
	switch args[0] {
	case "add":
		if len(args) < 3 || len(args) > 4 {
			return errors.New(usage)
		}
		b := node.BridgeConfig{A: args[1], B: args[2]}
		if len(args) == 4 {
			if b.BudgetMBPerDay, err = parseInt("MB/day", args[3]); err != nil {
				return err
			}
		}
		for i := range c.Bridges {
			if same(c.Bridges[i], b.A, b.B) {
				c.Bridges[i].BudgetMBPerDay = b.BudgetMBPerDay
				return putConfig(c)
			}
		}
		c.Bridges = append(c.Bridges, b)
		return putConfig(c)
	case "rm", "remove":
		if len(args) != 3 {
			return errors.New(usage)
		}
		kept := c.Bridges[:0]
		for _, b := range c.Bridges {
			if !same(b, args[1], args[2]) {
				kept = append(kept, b)
			}
		}
		if len(kept) == len(c.Bridges) {
			return fmt.Errorf("no bridge %s ⇄ %s", args[1], args[2])
		}
		c.Bridges = kept
		return putConfig(c)
	}
	return errors.New(usage)
}

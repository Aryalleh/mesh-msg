package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
)

// MaxNameLen bounds the display name sent in announces.
const MaxNameLen = 32

var relayNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// Validate checks the settings a UI can change, filling in defaults.
func (c *Config) Validate() error {
	c.Name = strings.TrimSpace(c.Name)
	if len([]rune(c.Name)) > MaxNameLen {
		return fmt.Errorf("name: at most %d characters", MaxNameLen)
	}
	if c.AnnounceMinutes == 0 {
		c.AnnounceMinutes = 30
	}
	if c.AnnounceMinutes < 1 || c.AnnounceMinutes > 24*60 {
		return errors.New("announce_minutes: 1 to 1440")
	}
	names := map[string]bool{}
	for i := range c.Relays {
		r := &c.Relays[i]
		r.Name, r.URL, r.Via = strings.TrimSpace(r.Name), strings.TrimSpace(r.URL), strings.TrimSpace(r.Via)
		if !relayNameRE.MatchString(r.Name) {
			return fmt.Errorf("relay %q: name must be 1-32 letters, digits, . _ -", r.Name)
		}
		if names[r.Name] {
			return fmt.Errorf("relay %q: name used twice", r.Name)
		}
		names[r.Name] = true
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("relay %s: URL must look like https://host or http://host", r.Name)
		}
		if r.Via == "" {
			r.Via = "direct"
		}
		if r.Via != "direct" && r.Via != "tor" {
			p, err := url.Parse(r.Via)
			if err != nil || p.Host == "" || (p.Scheme != "socks5" && p.Scheme != "socks5h" && p.Scheme != "http" && p.Scheme != "https") {
				return fmt.Errorf("relay %s: via must be direct, tor, or a proxy like socks5://127.0.0.1:1080", r.Name)
			}
		}
	}
	switch c.Gateway.Mode {
	case "":
		c.Gateway.Mode = "targeted"
	case "targeted", "carrier":
	default:
		return fmt.Errorf("gateway mode %q: targeted or carrier", c.Gateway.Mode)
	}
	if c.Gateway.QuotaKBPerHour < 0 {
		return errors.New("gateway quota: must not be negative")
	}
	for _, b := range c.Bridges {
		if !names[b.A] || !names[b.B] {
			return fmt.Errorf("bridge %s-%s: both ends must be relays in the list", b.A, b.B)
		}
		if b.A == b.B {
			return fmt.Errorf("bridge %s-%s: needs two different relays", b.A, b.B)
		}
		if b.BudgetMBPerDay < 0 {
			return errors.New("bridge budget: must not be negative")
		}
	}
	if c.LAN.Port < 0 || c.LAN.Port > 65535 {
		return errors.New("lan port: 0 to 65535 (0 = any)")
	}
	peers := c.LAN.StaticPeers[:0]
	for _, p := range c.LAN.StaticPeers {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(p); err != nil {
			return fmt.Errorf("lan peer %q: want host:port", p)
		}
		peers = append(peers, p)
	}
	c.LAN.StaticPeers = peers
	c.LoRa.Port = strings.TrimSpace(c.LoRa.Port)
	if c.LoRa.Enabled && c.LoRa.Port == "" {
		return errors.New("lora: pick the USB port of the node")
	}
	return nil
}

// Save writes the config to c.Path (atomically; comments are not kept).
func (c *Config) Save() error {
	if c.Path == "" {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteString("# meshd configuration — written by meshd (web UI settings). See docs/desktop.md\n\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

// apply restarts the node with cfg. prepare, if set, runs while no node is
// open (to swap the identity). If the new node can't start, the old
// settings come back and the error is returned.
func (d *Daemon) apply(cfg *Config, prepare func() error) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.cfg
	d.stopNode()
	var err error
	if prepare != nil {
		err = prepare()
	}
	if err == nil {
		var n *node.Node
		if n, err = node.Open(cfg.Config); err == nil {
			d.node, d.cfg = n, cfg
			d.startNode()
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("applied, but could not save %s: %w", cfg.Path, err)
			}
			return nil
		}
	}
	n, err2 := node.Open(old.Config)
	if err2 != nil {
		log.Fatalf("meshd: restoring the previous settings failed: %v (after: %v)", err2, err)
	}
	d.node = n
	d.startNode()
	return err
}

// settingsAPI adds the settings and account endpoints.
func (d *Daemon) settingsAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		d.mu.RLock()
		defer d.mu.RUnlock()
		writeJSON(w, d.cfg)
	})
	mux.HandleFunc("PUT /config", func(w http.ResponseWriter, r *http.Request) {
		d.mu.RLock()
		cfg := *d.cfg
		d.mu.RUnlock()
		next := Config{}
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			httpErr(w, err, 400)
			return
		}
		// Not changeable from here: where things live and the UI address.
		next.DataDir, next.Logf, next.Path, next.UI = cfg.DataDir, cfg.Logf, cfg.Path, cfg.UI
		if err := d.apply(&next, nil); err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /serial-ports", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serialPorts())
	})
	mux.HandleFunc("POST /identity/export", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Passphrase string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		d.mu.RLock()
		b, err := d.node.BackupIdentity(req.Passphrase)
		d.mu.RUnlock()
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]string{"backup": b})
	})
	mux.HandleFunc("POST /identity/import", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Backup, Passphrase string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		id, err := identity.FromBackup(req.Backup, req.Passphrase)
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		d.mu.RLock()
		cfg := *d.cfg
		d.mu.RUnlock()
		var replaced string
		err = d.apply(&cfg, func() (err error) {
			replaced, err = node.RestoreIdentity(cfg.DataDir, id, true)
			return err
		})
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		writeJSON(w, map[string]string{"hash": id.Hash().String(), "replaced": replaced})
	})
}

// serialPorts lists likely USB serial devices for a LoRa node.
func serialPorts() []string {
	out := []string{}
	for _, pat := range []string{"/dev/ttyACM*", "/dev/ttyUSB*", "/dev/cu.usb*"} {
		m, _ := filepath.Glob(pat)
		out = append(out, m...)
	}
	return out
}

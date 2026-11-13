// Command relay runs a Mava (mesh-msg) relay server.
//
//	relay -config /etc/meshmsg/relay.json
//	relay -config /etc/meshmsg/relay.json admin list|add <id>|rm <id>
//
// The admin panel is at /admin (log in with a code sent to your Mava account).
//
// See server/deploy/ for an example config, systemd unit, Dockerfile,
// Caddyfile (TLS) and torrc (onion service).
package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/store"
	"github.com/Aryalleh/mesh-msg/core/update"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/network"
	"github.com/Aryalleh/mesh-msg/server/admin"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/ledger"
	"github.com/Aryalleh/mesh-msg/server/relay"
	"github.com/Aryalleh/mesh-msg/server/turn"
)

// Config is the JSON config file.
type Config struct {
	Name       string            `json:"name"`        // e.g. "ir-1"
	Region     string            `json:"region"`      // e.g. "ir" or "global"
	Listen     string            `json:"listen"`      // e.g. "127.0.0.1:8080" behind Caddy
	DataDir    string            `json:"data_dir"`    // SQLite + key + cursors
	PowBits    int               `json:"pow_bits"`    // default 16
	TTLHours   int               `json:"ttl_hours"`   // default 168 (7 days)
	SyncTokens []string          `json:"sync_tokens"` // accepted from peers/bridges
	Peers      []Peer            `json:"peers"`       // relays this one syncs with
	Relays     []wire.RelayEntry `json:"relays"`      // published relay list
	TLSCert    string            `json:"tls_cert"`    // optional: serve TLS directly
	TLSKey     string            `json:"tls_key"`
	Log        bool              `json:"log"` // default off: no request logs
	// Media (important encrypted files only): off unless media_max_mb > 0.
	// Files are deleted once every recipient fetched them, or after
	// media_important_hours. Each account may upload media_important_per_day
	// files per UTC day.
	MediaMaxMB   int `json:"media_max_mb"`
	MediaStoreMB int `json:"media_store_mb"` // total cap, default 2048
	// Important-media rules (docs/server.md §6a; defaults: api.DefaultMediaPolicy).
	MediaPerDay           *int    `json:"media_important_per_day"`   // default 5
	MediaImportantHours   *int    `json:"media_important_hours"`     // default 168
	MediaNewAccountDays   *int    `json:"media_new_account_days"`    // default 3
	MediaNewAccountPerDay *int    `json:"media_new_account_per_day"` // default 1
	MediaPowBaseBits      *int    `json:"media_pow_base_bits"`       // default 20
	MediaPowMaxBits       *int    `json:"media_pow_max_bits"`        // default 28
	MediaPowAgeTiers      *string `json:"media_pow_age_tiers"`       // default "3:3, 30:2, 180:1"
	// UsagePeers: relays to exchange the usage ledger with (URLs). Default:
	// the origin relays of network/relays.json plus the federation peers.
	UsagePeers []string `json:"usage_peers"`
	// UDP services for direct links (docs/server.md §6c, §6d): the
	// reflector (STUN, UDP 3478 + 3479) is on unless "reflector": false;
	// TURN for this relay's admins is off unless turn_enabled. public_ip is
	// the address TURN relays from (default: the first public IPv4 of an
	// interface; without one, TURN stays unavailable).
	Reflector       *bool  `json:"reflector"`
	PublicIP        string `json:"public_ip"`
	TurnEnabled     *bool  `json:"turn_enabled"`
	TurnMaxSessions *int   `json:"turn_max_sessions"` // default 10
	TurnMaxKbps     *int   `json:"turn_max_kbps"`     // default 256
	TurnMonthlyGB   *int   `json:"turn_monthly_gb"`   // default 50
	// MediaAccounts: if set, only these accounts (identity hashes, as
	// "mesh id" shows them) may upload media. Empty: anyone (with PoW).
	MediaAccounts []string `json:"media_accounts"`
	// Release manifests for the apps' update check (GET /v1/release): fetched
	// from GitHub every hour, or from release_upstream (another relay's URL)
	// when set. "release_check": false turns it off.
	ReleaseCheck    *bool  `json:"release_check"`
	ReleaseUpstream string `json:"release_upstream"`
}

// Peer is another relay to federate with.
type Peer struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	Via       string `json:"via"`        // direct | tor | socks5://…
	EverySecs int    `json:"every_secs"` // default 15
}

var version = "dev"

func main() {
	cfgPath := flag.String("config", "relay.json", "config file")
	genToken := flag.Bool("gen-token", false, "print a random sync token and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *genToken {
		var b [24]byte
		rand.Read(b[:])
		fmt.Println(hex.EncodeToString(b[:]))
		return
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if flag.Arg(0) == "admin" {
		if err := adminCmd(cfg, flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// adminCmd lists or changes the panel's admins (data_dir/panel.json).
func adminCmd(cfg *Config, args []string) error {
	const usage = "usage: meshmsg-relay -config FILE admin list | add <account id> | rm <account id>"
	if len(args) == 0 {
		return errors.New(usage)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	s, err := admin.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list", "ls":
	case "add", "rm", "remove":
		if len(args) < 2 {
			return errors.New(usage)
		}
		id, err := admin.ParseID(args[1])
		if err != nil {
			return err
		}
		if args[0] == "add" {
			s.Admins = append(s.Admins, id)
		} else {
			if id == admin.OwnerID {
				return errors.New("the owner account can't be removed")
			}
			s.Admins = slices.DeleteFunc(s.Admins, func(a string) bool { return strings.EqualFold(a, id) })
		}
		if err := admin.SaveSettings(cfg.DataDir, s); err != nil {
			return err
		}
		matchOwner(cfg.DataDir)
	default:
		return errors.New(usage)
	}
	fmt.Println(admin.OwnerID + "  (owner)")
	for _, a := range admin.CleanAdmins(s.Admins) {
		fmt.Println(a)
	}
	return nil
}

// matchOwner gives panel.json to the data dir's owner (the CLI runs as root,
// the service as its own user).
func matchOwner(dataDir string) {
	if st, err := os.Stat(dataDir); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			os.Chown(admin.SettingsPath(dataDir), int(sys.Uid), int(sys.Gid))
		}
	}
}

// applySettings lays the panel's saved settings over relay.json.
func applySettings(cfg *Config) error {
	s, err := admin.LoadSettings(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("%s: %w", admin.SettingsPath(cfg.DataDir), err)
	}
	set := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	set(&cfg.PowBits, s.PowBits)
	set(&cfg.TTLHours, s.TTLHours)
	set(&cfg.MediaMaxMB, s.MediaMaxMB)
	set(&cfg.MediaStoreMB, s.MediaStoreMB)
	if s.MediaAccounts != nil {
		cfg.MediaAccounts = s.MediaAccounts
	}
	if s.Log != nil {
		cfg.Log = *s.Log
	}
	return nil
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Listen: "127.0.0.1:8080", DataDir: "data", PowBits: 16, TTLHours: 168, Region: "global"}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Name == "" {
		return nil, errors.New("config: name is required")
	}
	return cfg, nil
}

func loadOrCreateKey(path string) (*identity.Identity, error) {
	if b, err := os.ReadFile(path); err == nil {
		seed, err := hex.DecodeString(string(b))
		if err != nil {
			return nil, fmt.Errorf("key file: %w", err)
		}
		return identity.FromSeed(seed)
	}
	id, err := identity.Generate()
	if err != nil {
		return nil, err
	}
	return id, os.WriteFile(path, []byte(hex.EncodeToString(id.Seed())), 0o600)
}

func run(cfg *Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := applySettings(cfg); err != nil {
		return err
	}
	key, err := loadOrCreateKey(filepath.Join(cfg.DataDir, "relay.key"))
	if err != nil {
		return err
	}
	st, err := store.OpenSQLite(filepath.Join(cfg.DataDir, "relay.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	logf := func(string, ...any) {}
	if cfg.Log {
		logf = log.Printf
	}
	// Important-media rules: defaults < relay.json < panel.json.
	base, err := admin.Settings{
		MediaPerDay: cfg.MediaPerDay, MediaImportantHours: cfg.MediaImportantHours,
		MediaNewAccountDays: cfg.MediaNewAccountDays, MediaNewAccountPerDay: cfg.MediaNewAccountPerDay,
		MediaPowBaseBits: cfg.MediaPowBaseBits, MediaPowMaxBits: cfg.MediaPowMaxBits, MediaPowAgeTiers: cfg.MediaPowAgeTiers,
	}.Policy(api.DefaultMediaPolicy())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	saved, _ := admin.LoadSettings(cfg.DataDir)
	policy, err := saved.Policy(base)
	if err != nil {
		return fmt.Errorf("%s: %w", admin.SettingsPath(cfg.DataDir), err)
	}
	led, err := ledger.Open(filepath.Join(cfg.DataDir, "ledger.db"))
	if err != nil {
		return err
	}
	defer led.Close()
	rl := relay.New(relay.Config{
		Name: cfg.Name, Region: cfg.Region, Store: st,
		DataTTL: time.Duration(cfg.TTLHours) * time.Hour, Logf: logf,
		OnAnnounce: func(dest packet.Hash, at time.Time) { led.Seen(dest.String(), at) },
	})
	backfillAges(st, led)

	// Reflector and TURN; admins come from the panel (set below).
	baseTURN, err := admin.Settings{TurnEnabled: cfg.TurnEnabled, TurnMaxSessions: cfg.TurnMaxSessions,
		TurnMaxKbps: cfg.TurnMaxKbps, TurnMonthlyGB: cfg.TurnMonthlyGB}.TurnLimits(turn.DefaultLimits())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	turnLimits, err := saved.TurnLimits(baseTURN)
	if err != nil {
		return fmt.Errorf("%s: %w", admin.SettingsPath(cfg.DataDir), err)
	}
	var panel *admin.Panel
	var udp *turn.Service
	if cfg.Reflector == nil || *cfg.Reflector {
		udp, err = turn.Start(turn.Config{
			PublicIP: publicIP(cfg.PublicIP), DataDir: cfg.DataDir, Identity: key, Limits: turnLimits, Logf: logf,
			IsAdmin: func(h string) bool { return panel != nil && slices.Contains(panel.Admins(), h) },
		})
		if err != nil {
			log.Printf("reflector/TURN off: %v", err)
			udp = nil
		} else {
			defer udp.Close()
		}
	}
	var rel *update.Cache
	if cfg.ReleaseCheck == nil || *cfg.ReleaseCheck {
		rel = &update.Cache{Upstream: cfg.ReleaseUpstream, Logf: log.Printf}
	}
	h := api.New(api.Config{
		Release: rel,
		Relay:   rl, Identity: key, PowBits: cfg.PowBits,
		SyncTokens: cfg.SyncTokens, Relays: cfg.Relays, Logf: logf, Ledger: led,
		UDP: func() (bool, bool) {
			if udp == nil {
				return false, false
			}
			return true, udp.Available() && udp.Limits().Enabled
		},
		Media: api.MediaConfig{
			Dir: filepath.Join(cfg.DataDir, "media"), MaxMB: cfg.MediaMaxMB, StoreMB: cfg.MediaStoreMB,
			Policy: &policy, Accounts: cfg.MediaAccounts,
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var peers []string
	for _, p := range cfg.Peers {
		peers = append(peers, p.Name)
	}
	if udp != nil {
		h.Handle("POST /v1/turn/credentials", http.HandlerFunc(udp.CredentialsHandler))
	}
	panel = admin.New(admin.Config{
		Info: admin.Info{
			Name: cfg.Name, Region: cfg.Region, Version: version, PubKey: hex.EncodeToString(key.Bytes()),
			Listen: cfg.Listen, Peers: peers, Started: time.Now(),
		},
		DataDir: cfg.DataDir, Identity: key, Relay: rl, API: h, BasePolicy: base, TURN: udp, BaseTURN: baseTURN,
		Effective: func() map[string]any {
			p := h.MediaPolicy()
			return map[string]any{
				"pow_bits": cfg.PowBits, "ttl_hours": cfg.TTLHours, "log": cfg.Log,
				"media_max_mb": cfg.MediaMaxMB, "media_store_mb": cmp.Or(cfg.MediaStoreMB, 2048),
				"media_important_per_day": p.PerDay, "media_important_hours": p.Hours,
				"media_new_account_days": p.NewAccountDays, "media_new_account_per_day": p.NewAccountPerDay,
				"media_pow_base_bits": p.PowBaseBits, "media_pow_max_bits": p.PowMaxBits,
				"media_pow_age_tiers": api.FormatTiers(p.AgeTiers),
				"media_accounts":      cfg.MediaAccounts,
				"turn_enabled":        turnNow(udp).Enabled, "turn_max_sessions": turnNow(udp).MaxSessions,
				"turn_max_kbps": turnNow(udp).MaxKbps, "turn_monthly_gb": turnNow(udp).MonthlyGB,
			}
		},
		// Exit cleanly; systemd (Restart=always) or Docker starts it again.
		Restart: stop,
	})
	panel.Routes(h.Handle)

	var wg sync.WaitGroup
	for _, u := range usagePeers(cfg, key) {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			syncUsage(ctx, cfg, led, u)
		}(u)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			led.Expire()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()
	if rel != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel.Run(ctx)
		}()
	}
	for _, p := range cfg.Peers {
		wg.Add(1)
		go func(p Peer) {
			defer wg.Done()
			federate(ctx, cfg, rl, p)
		}(p)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rl.Expire()
			}
		}
	}()

	srv := &http.Server{Addr: cfg.Listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("relay %s %s (region %s) listening on %s, pubkey %s, admin panel at /admin", cfg.Name, version, cfg.Region, cfg.Listen, hex.EncodeToString(key.Bytes())[:16])
	if cfg.TLSCert != "" {
		err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	wg.Wait()
	return err
}

// federate syncs with one peer relay forever, persisting cursors.
func federate(ctx context.Context, cfg *Config, rl *relay.Relay, p Peer) {
	every := time.Duration(max(p.EverySecs, 1)) * time.Second
	if p.EverySecs == 0 {
		every = 15 * time.Second
	}
	c, err := relayclient.New(p.URL, relayclient.Options{Origin: cfg.Name, Region: cfg.Region, Token: p.Token, Via: p.Via})
	if err != nil {
		log.Printf("peer %s: %v", p.Name, err)
		return
	}
	curPath := filepath.Join(cfg.DataDir, "cursor-"+p.Name+".json")
	cur := &relayclient.Cursors{}
	if b, err := os.ReadFile(curPath); err == nil {
		json.Unmarshal(b, cur)
	}
	local := relay.LocalSide{R: rl, Peer: p.Name}
	remote := relayclient.RemoteSide{C: c, Label: p.Name}
	backoff := every
	for {
		ab, ba, err := relayclient.SyncOnce(ctx, local, remote, cur, nil)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("peer %s: sync: %v (retry in %v)", p.Name, err, backoff)
			backoff = min(backoff*2, 10*time.Minute)
		} else {
			backoff = every
			if b, err := json.Marshal(cur); err == nil {
				os.WriteFile(curPath, b, 0o600)
			}
			if (ab > 0 || ba > 0) && cfg.Log {
				log.Printf("peer %s: sent %d, received %d", p.Name, ab, ba)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// backfillAges records the accounts whose announces are already stored
// (relays from before the ledger): their age counts from that announce.
func backfillAges(st store.Store, led *ledger.Ledger) {
	var after int64
	for {
		items, err := st.Since(after, 1000, store.Filter{Types: []packet.Type{packet.TypeAnnounce}})
		if err != nil || len(items) == 0 {
			return
		}
		for _, it := range items {
			led.Seen(it.Dest.String(), it.Received)
			after = it.Seq
		}
	}
}

// usagePeers are the relays this one exchanges the usage ledger with:
// usage_peers if set, else the origin relays (network/relays.json, not
// itself) and the federation peers.
func usagePeers(cfg *Config, key *identity.Identity) []string {
	if len(cfg.UsagePeers) > 0 {
		return cfg.UsagePeers
	}
	me := hex.EncodeToString(key.Bytes())
	var out []string
	for _, r := range network.Bootstrap().Relays {
		if r.Origin && !strings.EqualFold(r.PubKey, me) && len(r.URLs) > 0 {
			out = append(out, r.URLs[0])
		}
	}
	for _, p := range cfg.Peers {
		if !slices.Contains(out, p.URL) {
			out = append(out, p.URL)
		}
	}
	return out
}

// syncUsage pushes this relay's new ledger entries to a relay and pulls
// that relay's, every minute. Entries are self-verifying, so any relay
// will do; the cursors live in the ledger.
func syncUsage(ctx context.Context, cfg *Config, led *ledger.Ledger, u string) {
	c, err := relayclient.New(u, relayclient.Options{Origin: cfg.Name, Region: cfg.Region})
	if err != nil {
		log.Printf("usage peer %s: %v", u, err)
		return
	}
	for {
		push, pull := led.Cursors(u)
		err := func() error {
			for {
				es, next, more, err := led.Since(push, 1000)
				if err != nil || len(es) == 0 {
					return err
				}
				var b []byte
				for _, e := range es {
					b = append(b, e.Encode()...)
				}
				if _, err := c.PushUsage(ctx, b); err != nil {
					return err
				}
				push = next
				led.SetCursors(u, push, pull)
				if !more {
					break
				}
			}
			for {
				b, next, more, err := c.PullUsage(ctx, pull)
				if err != nil {
					return err
				}
				es, err := ledger.DecodeAll(b)
				if err != nil {
					return err
				}
				led.Add(es, u)
				pull = next
				led.SetCursors(u, push, pull)
				if !more || len(es) == 0 {
					return nil
				}
			}
		}()
		if err != nil && cfg.Log && ctx.Err() == nil {
			log.Printf("usage peer %s: %v", u, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

func turnNow(s *turn.Service) turn.Limits {
	if s == nil {
		return turn.DefaultLimits()
	}
	return s.Limits()
}

// publicIP: the configured address, else the first public IPv4 of an
// interface (nil: none; TURN can't run, the reflector still does).
func publicIP(conf string) net.IP {
	if conf != "" {
		return net.ParseIP(conf)
	}
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok {
			if ip := n.IP.To4(); ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !(ip[0] == 100 && ip[1]&0xc0 == 64) {
				return ip
			}
		}
	}
	return nil
}

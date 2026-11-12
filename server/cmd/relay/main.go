// Command relay runs a mesh-msg relay server.
//
//	relay -config /etc/meshmsg/relay.json
//
// See server/deploy/ for an example config, systemd unit, Dockerfile,
// Caddyfile (TLS) and torrc (onion service).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/store"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
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
	// Media (encrypted files): off unless media_max_mb > 0. Files are deleted
	// once every recipient fetched them, or after media_max_hours (files up
	// to 1 MB; bigger ones sooner, down to media_min_hours).
	MediaMaxMB    int `json:"media_max_mb"`
	MediaStoreMB  int `json:"media_store_mb"`  // total cap, default 2048
	MediaMaxHours int `json:"media_max_hours"` // default 8
	MediaMinHours int `json:"media_min_hours"` // default 1
	// MediaAccounts: if set, only these accounts (identity hashes, as
	// "mesh id" shows them) may upload media. Empty: anyone (with PoW).
	MediaAccounts []string `json:"media_accounts"`
}

// Peer is another relay to federate with.
type Peer struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	Via       string `json:"via"`        // direct | tor | socks5://…
	EverySecs int    `json:"every_secs"` // default 15
}

func main() {
	cfgPath := flag.String("config", "relay.json", "config file")
	genToken := flag.Bool("gen-token", false, "print a random sync token and exit")
	flag.Parse()
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
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
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
	rl := relay.New(relay.Config{
		Name: cfg.Name, Region: cfg.Region, Store: st,
		DataTTL: time.Duration(cfg.TTLHours) * time.Hour, Logf: logf,
	})
	h := api.New(api.Config{
		Relay: rl, Identity: key, PowBits: cfg.PowBits,
		SyncTokens: cfg.SyncTokens, Relays: cfg.Relays, Logf: logf,
		Media: api.MediaConfig{
			Dir: filepath.Join(cfg.DataDir, "media"), MaxMB: cfg.MediaMaxMB, StoreMB: cfg.MediaStoreMB,
			MaxHours: cfg.MediaMaxHours, MinHours: cfg.MediaMinHours, Accounts: cfg.MediaAccounts,
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
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
	log.Printf("relay %s (region %s) listening on %s, pubkey %s", cfg.Name, cfg.Region, cfg.Listen, hex.EncodeToString(key.Bytes())[:16])
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

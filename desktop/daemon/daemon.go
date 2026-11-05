package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

//go:embed ui/index.html
var uiFS embed.FS

// Daemon runs a node plus its control interfaces.
type Daemon struct {
	cfg   *Config
	node  *node.Node
	token string
}

// New opens the node.
func New(cfg *Config) (*Daemon, error) {
	n, err := node.Open(cfg.Config)
	if err != nil {
		return nil, err
	}
	tok, err := loadOrCreateToken(filepath.Join(cfg.DataDir, "ui.token"))
	if err != nil {
		n.Close()
		return nil, err
	}
	return &Daemon{cfg: cfg, node: n, token: tok}, nil
}

func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	var b [24]byte
	rand.Read(b[:])
	t := hex.EncodeToString(b[:])
	return t, os.WriteFile(path, []byte(t), 0o600)
}

// Node returns the running node.
func (d *Daemon) Node() *node.Node { return d.node }

// Run serves until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	defer d.node.Close()
	api := d.apiHandler()

	sock := SocketPath(d.cfg.DataDir)
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	os.Chmod(sock, 0o600)
	ctlSrv := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second}
	go ctlSrv.Serve(ln)
	defer ctlSrv.Close()

	if d.cfg.UI != "" {
		host, _, err := net.SplitHostPort(d.cfg.UI)
		if err != nil || !(host == "127.0.0.1" || host == "::1" || host == "localhost") {
			return fmt.Errorf("ui address %q must be loopback", d.cfg.UI)
		}
		uiLn, err := net.Listen("tcp", d.cfg.UI)
		if err != nil {
			return fmt.Errorf("web ui: %w", err)
		}
		uiSrv := &http.Server{Handler: d.uiHandler(api), ReadHeaderTimeout: 5 * time.Second}
		go uiSrv.Serve(uiLn)
		defer uiSrv.Close()
	}
	return d.node.Run(ctx)
}

// UIURL is the URL that logs the browser into the web UI.
func (d *Daemon) UIURL() string {
	if d.cfg.UI == "" {
		return ""
	}
	return "http://" + d.cfg.UI + "/?token=" + d.token
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// apiHandler is the JSON control API (shared by the socket and the web UI).
func (d *Daemon) apiHandler() http.Handler {
	n := d.node
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.Status())
	})
	mux.HandleFunc("GET /lora", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.LoRa())
	})
	mux.HandleFunc("GET /lora/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"regions": hostlink.Regions, "presets": hostlink.Presets})
	})
	mux.HandleFunc("POST /lora/{name}/config", func(w http.ResponseWriter, r *http.Request) {
		var p node.LoRaPatch
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			httpErr(w, err, 400)
			return
		}
		if err := n.LoRaConfigure(r.PathValue("name"), p); err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /id", func(w http.ResponseWriter, r *http.Request) {
		id := n.Identity()
		writeJSON(w, map[string]string{"hash": id.Hash().String(), "uri": id.URI(), "ui": d.UIURL()})
	})
	mux.HandleFunc("GET /contacts", func(w http.ResponseWriter, r *http.Request) {
		cs, err := n.Contacts()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		writeJSON(w, cs)
	})
	mux.HandleFunc("POST /contacts", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ URI, Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		c, err := n.AddContact(req.URI, req.Name)
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, c)
	})
	mux.HandleFunc("POST /contacts/{ref}/rename", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		if err := n.RenameContact(r.PathValue("ref"), req.Name); err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /contacts/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if err := n.RemoveContact(r.PathValue("ref")); err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ To, Text string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			httpErr(w, errors.New("empty message"), 400)
			return
		}
		m, err := n.Send(req.To, req.Text)
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, m)
	})
	mux.HandleFunc("GET /messages", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		ms, err := n.Messages(r.URL.Query().Get("with"), limit)
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		if ms == nil {
			ms = []node.Message{}
		}
		writeJSON(w, ms)
	})
	// Server-sent events stream.
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			httpErr(w, errors.New("streaming unsupported"), 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch, cancel := n.Subscribe()
		defer cancel()
		fmt.Fprint(w, ": ok\n\n")
		fl.Flush()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
			case e := <-ch:
				b, _ := json.Marshal(e)
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			fl.Flush()
		}
	})
	return mux
}

// uiHandler serves the web UI and proxies /api/* to the control API, after
// checking the token cookie, the Host header (DNS rebinding) and, for
// writes, a custom header (CSRF).
func (d *Daemon) uiHandler(api http.Handler) http.Handler {
	const cookie = "meshd_token"
	validHost := func(h string) bool {
		host, _, err := net.SplitHostPort(h)
		if err != nil {
			host = h
		}
		return host == "127.0.0.1" || host == "localhost" || host == "::1" || host == "[::1]"
	}
	authed := func(r *http.Request) bool {
		c, err := r.Cookie(cookie)
		return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(d.token)) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if t := r.URL.Query().Get("token"); t != "" {
			if subtle.ConstantTimeCompare([]byte(t), []byte(d.token)) != 1 {
				http.Error(w, "bad token", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookie, Value: d.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if !authed(r) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "Open the web UI with:  mesh ui")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Header.Get("X-Meshd") != "1" {
				http.Error(w, "missing X-Meshd header", http.StatusForbidden)
				return
			}
			http.StripPrefix("/api", api).ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := uiFS.ReadFile("ui/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}

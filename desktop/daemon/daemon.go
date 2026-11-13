package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Aryalleh/mesh-msg/core/media"
	"io"
	"log"
	mimepkg "mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/node"
)

//go:embed ui/index.html
var uiFS embed.FS

// Daemon runs a node plus its control interfaces. Settings changes from the
// UI restart the node in place (see apply), so the node is behind mu.
type Daemon struct {
	token   string
	version string          // running version (SetVersion)
	upd     *updater        // set by Run
	ctx     context.Context // daemon lifetime, set by Run

	opMu sync.Mutex   // serialises restarts
	mu   sync.RWMutex // guards the fields below; held (R) by API requests
	cfg  *Config
	node *node.Node
	stop context.CancelFunc
	done chan struct{} // closed when the running node has stopped
}

// New opens the node.
func New(cfg *Config) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
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
func (d *Daemon) Node() *node.Node {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.node
}

// startNode runs d.node until stopNode. Callers hold d.mu.
func (d *Daemon) startNode() {
	ctx, cancel := context.WithCancel(d.ctx)
	done := make(chan struct{})
	n := d.node
	d.stop, d.done = cancel, done
	go func() {
		defer close(done)
		if err := n.Run(ctx); err != nil {
			log.Printf("meshd: %v", err)
		}
	}()
}

// stopNode stops and closes d.node. Callers hold d.mu.
func (d *Daemon) stopNode() {
	d.stop()
	<-d.done
	d.node.Close()
}

// Run serves until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	d.ctx = ctx
	d.mu.Lock()
	d.startNode()
	d.startUpdater(ctx)
	dataDir, uiAddr := d.cfg.DataDir, d.cfg.UI // d.cfg is replaced when settings change
	d.mu.Unlock()
	defer func() {
		d.opMu.Lock()
		d.mu.Lock()
		d.stopNode()
		d.mu.Unlock()
		d.opMu.Unlock()
	}()
	api := d.apiHandler()

	sock := SocketPath(dataDir)
	if Running(dataDir) {
		return ErrRunning
	}
	os.Remove(sock) // stale: nobody answers on it
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	os.Chmod(sock, 0o600)
	ctlSrv := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second}
	go ctlSrv.Serve(ln)
	defer ctlSrv.Close()

	if uiAddr != "" {
		host, _, err := net.SplitHostPort(uiAddr)
		if err != nil || !(host == "127.0.0.1" || host == "::1" || host == "localhost") {
			return fmt.Errorf("ui address %q must be loopback", uiAddr)
		}
		uiLn, err := net.Listen("tcp", uiAddr)
		if err != nil {
			return fmt.Errorf("web ui: %w", err)
		}
		uiSrv := &http.Server{Handler: d.uiHandler(api), ReadHeaderTimeout: 5 * time.Second}
		go uiSrv.Serve(uiLn)
		defer uiSrv.Close()
	}
	<-ctx.Done()
	return nil
}

// ErrRunning: another meshd already serves this data directory.
var ErrRunning = errors.New("meshd is already running for this data directory (stop it first: systemctl --user stop meshd)")

// Running reports whether a meshd answers on the control socket in dataDir.
// A second meshd must not start: it would take over (and on exit delete) the
// first one's socket and open the same store.
func Running(dataDir string) bool {
	c, err := net.DialTimeout("unix", SocketPath(dataDir), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
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
// Requests hold d.mu (read) so the node can't be swapped under them; the
// event stream and the calls that restart the node don't.
func (d *Daemon) apiHandler() http.Handler {
	mux := http.NewServeMux()
	d.settingsAPI(mux)
	d.updateAPI(mux)
	h := func(f func(n *node.Node, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			d.mu.RLock()
			defer d.mu.RUnlock()
			f(d.node, w, r)
		}
	}
	mux.HandleFunc("GET /status", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.Status())
	}))
	mux.HandleFunc("GET /relays/known", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		ks := n.KnownRelays()
		if ks == nil {
			ks = []node.KnownRelay{}
		}
		writeJSON(w, ks)
	}))
	mux.HandleFunc("POST /relays/forget", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		var req struct{ Relay string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		if err := n.ForgetRelay(req.Relay); err != nil {
			httpErr(w, err, 400)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /lora", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.LoRa())
	}))
	mux.HandleFunc("GET /lora/plans", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"regions": hostlink.Regions, "presets": hostlink.Presets})
	}))
	mux.HandleFunc("POST /lora/{name}/config", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
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
	}))
	mux.HandleFunc("GET /folders", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		writeJSON(w, n.Folders())
	}))
	mux.HandleFunc("PUT /folders", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		var fs []node.Folder
		if err := json.NewDecoder(r.Body).Decode(&fs); err != nil {
			httpErr(w, err, 400)
			return
		}
		if err := n.SetFolders(fs); err != nil {
			httpErr(w, err, 400)
			return
		}
		w.WriteHeader(204)
	}))
	// Read receipts (relays only): {"on": bool}.
	mux.HandleFunc("GET /read-receipts", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"on": n.ReadReceipts()})
	}))
	mux.HandleFunc("PUT /read-receipts", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		var v struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			httpErr(w, err, 400)
			return
		}
		if err := n.SetReadReceipts(v.On); err != nil {
			httpErr(w, err, 500)
			return
		}
		w.WriteHeader(204)
	}))
	// Profile pictures (fetched from contacts over direct links only).
	mux.HandleFunc("GET /avatar/{hash}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		hash := r.PathValue("hash")
		if hash == "me" {
			hash = n.Identity().Hash().String()
		}
		p := n.AvatarPath(hash)
		if p == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, p)
	}))
	mux.HandleFunc("PUT /avatar", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(io.LimitReader(r.Body, node.MaxAvatar+1))
		if err == nil {
			err = n.SetAvatar(b)
		}
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /avatar", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		n.SetAvatar(nil)
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /avatar/{hash}/request", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		n.RequestAvatar(r.PathValue("hash"))
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /id", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		id := n.Identity()
		writeJSON(w, map[string]string{"hash": id.Hash().String(), "uri": id.URI(), "link": id.Link(), "ui": d.UIURL()})
	}))
	mux.HandleFunc("GET /contacts", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		cs, err := n.Contacts()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		writeJSON(w, cs)
	}))
	mux.HandleFunc("POST /contacts", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
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
	}))
	mux.HandleFunc("POST /contacts/{ref}/rename", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
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
	}))
	mux.HandleFunc("DELETE /contacts/{ref}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		if err := n.RemoveContact(r.PathValue("ref")); err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /chats", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		cs, err := n.Chats()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		writeJSON(w, cs)
	}))
	mux.HandleFunc("POST /read/{ref}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		if err := n.MarkRead(r.PathValue("ref")); err != nil {
			httpErr(w, err, 404)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /groups", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		gs, err := n.Groups()
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		if gs == nil {
			gs = []node.Group{}
		}
		writeJSON(w, gs)
	}))
	mux.HandleFunc("GET /regions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, node.Regions)
	})
	mux.HandleFunc("POST /groups", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		// {"name": "…", "members": ["sara", …]} creates a private group;
		// {"link": "global" | "#tehran" | "meshgroup:#tehran"} joins a public chat.
		var req struct {
			Name, Link string
			Members    []string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		var g node.Group
		var err error
		if req.Link != "" {
			g, err = n.JoinGroup(req.Link)
		} else {
			g, err = n.CreateGroup(req.Name, req.Members)
		}
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, g)
	}))
	mux.HandleFunc("POST /groups/{ref}/members", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		var req struct{ Add []string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, err, 400)
			return
		}
		g, err := n.AddMembers(r.PathValue("ref"), req.Add)
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, g)
	}))
	mux.HandleFunc("DELETE /groups/{ref}/members/{member}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		g, err := n.RemoveMember(r.PathValue("ref"), r.PathValue("member"))
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, g)
	}))
	mux.HandleFunc("DELETE /groups/{ref}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		if err := n.LeaveGroup(r.PathValue("ref")); err != nil {
			httpErr(w, err, 404)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /send", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
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
	}))
	// Files: the body is the file itself; to, name, mime, caption, important
	// (1: also through relays) in the query.
	mux.HandleFunc("POST /send-media", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		file, err := io.ReadAll(http.MaxBytesReader(w, r.Body, media.MaxFileBytes))
		if err != nil {
			httpErr(w, fmt.Errorf("file too large (max %d MB)", media.MaxFileBytes>>20), 413)
			return
		}
		mime := q.Get("mime")
		if mime == "" {
			mime = http.DetectContentType(file)
		}
		m, err := n.SendMedia(q.Get("to"), file, mime, q.Get("name"), q.Get("caption"), q.Get("important") == "1")
		if err != nil {
			httpErr(w, err, 400)
			return
		}
		writeJSON(w, m)
	}))
	mux.HandleFunc("GET /media/{id}", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		file, mi, err := n.MediaFile(r.PathValue("id"))
		if err != nil {
			httpErr(w, err, 404)
			return
		}
		// Files come from other people: only plain images, audio and video
		// are shown inline, everything else is a download, and nothing may
		// run in the UI's origin.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		disp := "attachment"
		if safeInline(mi.Mime) && r.URL.Query().Get("download") == "" {
			w.Header().Set("Content-Type", mi.Mime)
			disp = "inline"
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Content-Disposition", mimepkg.FormatMediaType(disp, map[string]string{"filename": mi.Name}))
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file))
	}))
	mux.HandleFunc("GET /media/{id}/info", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		mi, ok := n.MediaInfo(r.PathValue("id"))
		if !ok {
			httpErr(w, errors.New("no such file"), 404)
			return
		}
		writeJSON(w, mi)
	}))
	mux.HandleFunc("POST /media/{id}/fetch", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
		if err := n.FetchMedia(r.PathValue("id")); err != nil {
			httpErr(w, err, 404)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /messages", h(func(n *node.Node, w http.ResponseWriter, r *http.Request) {
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
	}))
	// Server-sent events stream.
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			httpErr(w, errors.New("streaming unsupported"), 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		// The stream ends when the node restarts; clients reconnect.
		d.mu.RLock()
		ch, cancel := d.node.Subscribe()
		stopped := d.done
		d.mu.RUnlock()
		defer cancel()
		fmt.Fprint(w, ": ok\n\n")
		fl.Flush()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-stopped:
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

// safeInline: types a browser shows without running anything.
func safeInline(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp",
		"audio/ogg", "audio/mpeg", "audio/mp4", "audio/aac", "audio/webm", "audio/wav", "audio/x-m4a",
		"video/mp4", "video/webm", "video/3gpp":
		return true
	}
	return false
}

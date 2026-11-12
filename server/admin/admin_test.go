package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/server/admin"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func (c *client) do(method, path string, body any, csrf bool) (int, map[string]any) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if csrf {
		req.Header.Set("X-Mava-Panel", "1")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// An admin logs in with a code the relay sends to their Mava account, then
// manages admins; the owner can't be removed.
func TestLoginWithCodeSentToMava(t *testing.T) {
	dataDir := t.TempDir()
	key, _ := identity.Generate()
	rl := relay.New(relay.Config{Name: "eu-test", Region: "global"})
	h := api.New(api.Config{Relay: rl, Identity: key, PowBits: 4, RateLimit: 1000,
		Media: api.MediaConfig{Dir: t.TempDir(), MaxMB: 4}})
	restarted := make(chan struct{}, 1)
	admin.New(admin.Config{
		Info:    admin.Info{Name: "eu-test", Region: "global", Version: "test", PubKey: "00", Started: time.Now()},
		DataDir: dataDir, Identity: key, Relay: rl, API: h,
		Effective: func() map[string]any { return map[string]any{"pow_bits": 4} },
		Restart:   func() { restarted <- struct{}{} },
	}).Routes(h.Handle)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// The admin uses Mava with this relay.
	phone, err := node.Open(node.Config{DataDir: t.TempDir(), Name: "Admin",
		Relays:    []node.RelayConfig{{Name: "eu-test", URL: srv.URL}},
		Discovery: node.DiscoveryConfig{ListURL: "none", AllowPrivate: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { phone.Run(ctx); close(done) }()
	defer func() { cancel(); <-done; phone.Close() }()
	ev, unsub := phone.Subscribe()
	defer unsub()
	me := phone.Identity().Hash().String()

	jar, _ := cookiejar.New(nil)
	c := &client{t, srv.URL, &http.Client{Jar: jar}}
	if code, _ := c.do("GET", "/admin/api/state", nil, false); code != 401 {
		t.Fatalf("state without login: %d", code)
	}
	if code, _ := c.do("POST", "/admin/api/code", map[string]string{"id": me}, false); code != 403 {
		t.Fatalf("POST without the panel header: %d", code)
	}
	// Not an admin yet: no code is sent, and the answer doesn't say so.
	if code, out := c.do("POST", "/admin/api/code", map[string]string{"id": me}, true); code != 200 || out["info"] == nil {
		t.Fatalf("%d %v", code, out)
	}
	s, _ := admin.LoadSettings(dataDir)
	s.Admins = append(s.Admins, me)
	admin.SaveSettings(dataDir, s)

	// Wait until the relay has the phone's announce, then ask for a code.
	deadline := time.Now().Add(15 * time.Second)
	for {
		code, out := c.do("POST", "/admin/api/code", map[string]string{"id": me}, true)
		if code == 200 {
			break
		}
		if code != 409 || time.Now().After(deadline) {
			t.Fatalf("code: %d %v", code, out)
		}
		time.Sleep(300 * time.Millisecond)
	}
	var code string
	timeout := time.After(20 * time.Second)
	for code == "" {
		select {
		case e := <-ev:
			if e.Type == "message" {
				if m := regexp.MustCompile(`code for relay eu-test: (\d{8})`).FindStringSubmatch(e.Message.Text); m != nil {
					code = m[1]
				}
			}
		case <-timeout:
			t.Fatal("the code never reached the admin's Mava")
		}
	}
	if st, _ := c.do("POST", "/admin/api/login", map[string]string{"id": me, "code": "00000000"}, true); st != 401 {
		t.Fatalf("wrong code: %d", st)
	}
	if st, out := c.do("POST", "/admin/api/login", map[string]string{"id": me, "code": code}, true); st != 200 {
		t.Fatalf("login: %d %v", st, out)
	}
	// A code works once.
	if st, _ := c.do("POST", "/admin/api/login", map[string]string{"id": me, "code": code}, true); st != 401 {
		t.Fatalf("code reused: %d", st)
	}
	st, state := c.do("GET", "/admin/api/state", nil, false)
	if st != 200 || state["you"] != me || state["owner"] != admin.OwnerID {
		t.Fatalf("state: %d %v", st, state)
	}
	if st, _ := c.do("POST", "/admin/api/admins", map[string]string{"remove": admin.OwnerID}, true); st != 403 {
		t.Fatalf("owner removed: %d", st)
	}
	other := "0123456789abcdef0123456789abcdef"
	if st, out := c.do("POST", "/admin/api/admins", map[string]string{"add": other}, true); st != 200 || len(out["admins"].([]any)) != 3 {
		t.Fatalf("add: %d %v", st, out)
	}
	if st, out := c.do("POST", "/admin/api/settings", map[string]any{"media_max_mb": 16, "media_accounts": []string{other}}, true); st != 200 {
		t.Fatalf("settings: %d %v", st, out)
	}
	if st, _ := c.do("POST", "/admin/api/settings", map[string]any{"pow_bits": 99}, true); st != 400 {
		t.Fatal("bad pow_bits accepted")
	}
	s, _ = admin.LoadSettings(dataDir)
	if *s.MediaMaxMB != 16 || len(s.MediaAccounts) != 1 || len(s.Admins) != 2 {
		t.Fatalf("saved %+v", s)
	}
	c.do("POST", "/admin/api/restart", map[string]any{}, true)
	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatal("no restart")
	}
	// Removing yourself ends your session.
	c.do("POST", "/admin/api/admins", map[string]string{"remove": me}, true)
	if st, _ := c.do("GET", "/admin/api/state", nil, false); st != 401 {
		t.Fatalf("removed admin still in: %d", st)
	}
}

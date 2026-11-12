// Package network holds the official relay list (relays.json), shipped in
// every build and refreshed from the repository at run time.
package network

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
)

//go:embed relays.json
var bootstrap []byte

// SourceURL is where clients refresh the list from (the repository's main
// branch). Relays also share their lists (GET /v1/relays), so the list still
// spreads where GitHub is blocked.
const SourceURL = "https://raw.githubusercontent.com/Aryalleh/mesh-msg/main/network/relays.json"

// Relay is one entry of the list.
type Relay struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	URLs   []string `json:"urls"`
	PubKey string   `json:"pubkey,omitempty"` // hex X||Ed (128 bytes) — pins the relay
	Origin bool     `json:"origin,omitempty"` // the original project relay
}

// List is relays.json.
type List struct {
	Version int     `json:"version"`
	Updated string  `json:"updated"`
	Relays  []Relay `json:"relays"`
}

// Parse decodes and checks a relay list.
func Parse(b []byte) (*List, error) {
	var l List
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if l.Version != 1 {
		return nil, errors.New("network: unsupported relay list version")
	}
	if len(l.Relays) == 0 || len(l.Relays) > 200 {
		return nil, errors.New("network: bad relay list")
	}
	for _, r := range l.Relays {
		if r.Name == "" || len(r.URLs) == 0 {
			return nil, errors.New("network: relay without name or URL")
		}
		if r.PubKey != "" {
			if k, err := hex.DecodeString(r.PubKey); err != nil || len(k) != 64 {
				return nil, errors.New("network: bad relay pubkey")
			}
		}
		for _, u := range r.URLs {
			if _, err := CheckURL(u, true); err != nil {
				return nil, err
			}
		}
	}
	return &l, nil
}

// Bootstrap is the list compiled into this build.
func Bootstrap() *List {
	l, err := Parse(bootstrap)
	if err != nil {
		panic("network: embedded relays.json: " + err.Error())
	}
	return l
}

// CheckURL normalises a relay URL (http/https, host[:port][/path], no user
// info, query or fragment). Unless allowPrivate, it refuses loopback,
// private, link-local and unspecified addresses, so a stranger's hint can't
// point phones at their own local network.
func CheckURL(raw string, allowPrivate bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 200 {
		return "", errors.New("network: bad relay URL")
	}
	host := strings.ToLower(u.Hostname())
	if !allowPrivate {
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
			return "", errors.New("network: local relay URL")
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
			return "", errors.New("network: private relay address")
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

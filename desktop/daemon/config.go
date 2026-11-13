// Package daemon is meshd: it runs a core/node and exposes it to the `mesh`
// CLI (Unix socket) and to a local web UI (127.0.0.1 only, token protected).
package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// Config is the meshd config file (TOML).
type Config struct {
	node.Config
	// UI is the web UI listen address ("" disables). Must be loopback.
	UI string `toml:"ui" json:"ui"`
	// Path is the file the config was loaded from ("" = not saved).
	Path string `toml:"-" json:"path"`
}

// DefaultConfig is written on first start.
const DefaultConfig = `# meshd configuration — see docs/desktop.md

# Display name sent in announces (optional; leave empty for privacy).
name = ""

# Allow gateways to fetch messages for you from relays and carry them into
# the mesh around you (recommended, messages stay end-to-end encrypted).
delegate = true

announce_minutes = 30

# Local web UI. Open it with:  mesh ui
ui = "127.0.0.1:7878"

# Relays you can reach. "via" selects the network path:
#   "direct", "tor", or a proxy such as "socks5://127.0.0.1:1080" (VPN client).
# eu-1 is the project's default relay (HTTPS; messages are end-to-end
# encrypted either way). If the domain is blocked, http://87.106.5.192 is
# the same relay.
[[relay]]
name = "eu-1"
url  = "https://eu-1.mfadem.ir"
via  = "direct"

# [[relay]]
# name = "ir-1"
# url  = "https://relay-ir.example"
# via  = "direct"

# Relay discovery: on top of the relays above, use relays from the official
# list (network/relays.json in the repository, eu-1 first), from the lists
# relays share, and from nearby devices and LoRa nodes, which pass on the
# relays they use. Each one is checked (and its key pinned) before use.
# Share yours too. See them with:  mesh relay known
[discovery]
off = false
max_auto = 6

# Photos, voice and video messages, files: they travel straight between
# nearby devices or through relays that accept media, never through LoRa.
# Files up to this size download without asking.
[media]
auto_download_mb = 2

# Gateway: carry relay traffic into your local mesh (LAN / LoRa). On by default:
# without it, nodes near you that have no internet never get their replies.
[gateway]
enabled = true
mode = "targeted"          # "targeted" or "carrier"
quota_kb_per_hour = 512

# Bridge: sync two relays that can't reach each other (needs both above).
# [[bridge]]
# a = "ir-1"
# b = "eu-1"
# budget_mb_per_day = 50

# A LoRa node (Heltec V3, T-Beam S3, RAK4631, T-Echo with mesh-msg firmware)
# on USB: this PC carries its traffic to relays and to your chats. Configure
# the node itself (region, preset, power, role) with:  mesh lora set …
[lora]
enabled = false
port = "/dev/ttyACM0"

# Mesh with other meshd nodes on your local / private network.
[lan]
enabled = true
port = 0
discovery = true
static_peers = []
`

// LoadConfig reads path, creating it with defaults if missing.
func LoadConfig(path, dataDir string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(DefaultConfig), 0o600); err != nil {
			return nil, err
		}
	}
	cfg := &Config{}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	cfg.DataDir = dataDir
	cfg.Path = path
	return cfg, nil
}

// DefaultPaths returns the XDG config and data locations.
func DefaultPaths() (configPath, dataDir string) {
	cfgHome, err := os.UserConfigDir()
	if err != nil {
		cfgHome = "."
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		} else {
			dataHome = "."
		}
	}
	return filepath.Join(cfgHome, "meshd", "config.toml"), filepath.Join(dataHome, "meshd")
}

// SocketPath is the control socket inside the data dir.
func SocketPath(dataDir string) string { return filepath.Join(dataDir, "meshd.sock") }

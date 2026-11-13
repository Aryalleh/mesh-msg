package update

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GitHubReleasesAPI lists the project's releases (newest first).
const GitHubReleasesAPI = "https://api.github.com/repos/Aryalleh/mesh-msg/releases?per_page=20"

// ManifestName is the release asset that holds the signed manifest.
const ManifestName = "release.json"

// maxManifest bounds what is read from any source.
const maxManifest = 256 << 10

// Status is what an app shows the user.
type Status struct {
	Current   string    `json:"current"`
	Channel   string    `json:"channel"`
	Checked   time.Time `json:"checked"`
	Source    string    `json:"source,omitempty"` // where the manifest came from
	Latest    *Manifest `json:"latest,omitempty"`
	Available bool      `json:"available"` // Latest is newer than Current
	// Required: Current is below Latest.MinSupported (security release); the
	// notice can't be dismissed.
	Required bool   `json:"required"`
	Error    string `json:"error,omitempty"`
}

// Evaluate fills Available and Required for m against the running version.
func Evaluate(current string, m *Manifest) Status {
	st := Status{Current: current, Channel: ChannelOf(current), Latest: m}
	if m == nil {
		return st
	}
	st.Available = Compare(m.Version, current) > 0
	st.Required = m.MinSupported != "" && Compare(current, m.MinSupported) < 0
	return st
}

// Checker fetches the newest release manifest. Requests carry no identity,
// device ID or contact data; only the channel is sent.
type Checker struct {
	Current string            // running version
	Relays  func() []string   // relay base URLs, tried first, in order
	GitHub  string            // releases API URL; "" = GitHubReleasesAPI, "-" = don't use GitHub
	Key     ed25519.PublicKey // nil = ReleaseKey()
	Client  *http.Client      // nil = 20 s timeout client
	Logf    func(string, ...any)
}

func (c *Checker) key() ed25519.PublicKey {
	if c.Key != nil {
		return c.Key
	}
	return ReleaseKey()
}

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Check asks the relays in order, then GitHub, and returns the first manifest
// that verifies and is on a channel this install follows. Relays that answer
// with an older manifest than another source are not trusted to be current:
// the newest verified manifest across the sources that were asked wins.
func (c *Checker) Check(ctx context.Context) Status {
	ch := ChannelOf(c.Current)
	var best *Manifest
	var src string
	var errs []string
	consider := func(m *Manifest, from string) {
		if m != nil && Accepts(ch, m.Channel) && (best == nil || Compare(m.Version, best.Version) > 0) {
			best, src = m, from
		}
	}
	if c.Relays != nil {
		for _, r := range c.Relays() {
			u := strings.TrimRight(r, "/") + "/v1/release?channel=" + ch
			m, _, err := FetchManifest(ctx, c.client(), u, c.key())
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			consider(m, r)
			break // one relay that answers is enough
		}
	}
	if best == nil && c.GitHub != "-" {
		m, _, err := FetchGitHub(ctx, c.client(), cmp.Or(c.GitHub, GitHubReleasesAPI), ch, c.key())
		if err != nil {
			errs = append(errs, err.Error())
		}
		consider(m, "github")
	}
	st := Evaluate(c.Current, best)
	st.Checked, st.Source = time.Now(), src
	if best == nil && len(errs) > 0 {
		st.Error = strings.Join(errs, "; ")
	}
	return st
}

// Run checks at start and then about once a day with random jitter, calling
// report after each check, until ctx ends.
func (c *Checker) Run(ctx context.Context, report func(Status)) {
	delay := time.Duration(rand.Int64N(int64(30 * time.Second)))
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		st := c.Check(ctx)
		if c.Logf != nil && st.Error != "" {
			c.Logf("update check: %s", st.Error)
		}
		report(st)
		delay = 20*time.Hour + time.Duration(rand.Int64N(int64(8*time.Hour)))
		if st.Latest == nil {
			delay = time.Hour + time.Duration(rand.Int64N(int64(time.Hour)))
		}
	}
}

func get(ctx context.Context, cl *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meshmsg-update")
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", host(u), resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: response too large", host(u))
	}
	return b, nil
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// FetchManifest downloads release.json from u and verifies it.
func FetchManifest(ctx context.Context, cl *http.Client, u string, pub ed25519.PublicKey) (*Manifest, []byte, error) {
	b, err := get(ctx, cl, u, maxManifest)
	if err != nil {
		return nil, nil, err
	}
	m, err := Verify(b, pub)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", host(u), err)
	}
	return m, b, nil
}

// FetchGitHub walks the GitHub releases list (newest first) and returns the
// newest verified manifest on a channel ch accepts, with its raw bytes.
func FetchGitHub(ctx context.Context, cl *http.Client, api, ch string, pub ed25519.PublicKey) (*Manifest, []byte, error) {
	b, err := get(ctx, cl, api, 4<<20)
	if err != nil {
		return nil, nil, err
	}
	var rels []struct {
		Draft      bool `json:"draft"`
		Prerelease bool `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, nil, fmt.Errorf("github: %w", err)
	}
	var best *Manifest
	var bestRaw []byte
	var lastErr error
	tried := 0
	for _, r := range rels {
		if r.Draft || (ch == ChannelStable && r.Prerelease) {
			continue
		}
		for _, a := range r.Assets {
			if a.Name != ManifestName {
				continue
			}
			m, raw, err := FetchManifest(ctx, cl, a.URL, pub)
			if err != nil {
				lastErr = err
				continue
			}
			if Accepts(ch, m.Channel) && (best == nil || Compare(m.Version, best.Version) > 0) {
				best, bestRaw = m, raw
			}
		}
		if best != nil {
			break // releases are listed newest first
		}
		if tried++; tried >= 5 {
			break
		}
	}
	if best == nil {
		return nil, nil, cmp.Or(lastErr, errors.New("github: no signed release manifest found"))
	}
	return best, bestRaw, nil
}

// Download fetches f into path and checks its SHA-256 against the manifest.
// The file is removed if the hash doesn't match.
func Download(ctx context.Context, cl *http.Client, f *File, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "meshmsg-update")
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", host(f.URL), resp.Status)
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	limit := f.Size + 1
	if f.Size <= 0 {
		limit = 1 << 31
	}
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, limit))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && f.Size > 0 && n != f.Size {
		err = fmt.Errorf("%s: size %d, manifest says %d", f.Name, n, f.Size)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != strings.ToLower(f.SHA256) {
		err = fmt.Errorf("%s: SHA-256 does not match the signed manifest", f.Name)
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

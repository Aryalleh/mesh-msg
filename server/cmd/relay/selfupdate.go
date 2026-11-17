package main

// Self-update (relay -self-update): the relay follows the releases on GitHub
// by itself. A root systemd timer (meshmsg-relay-update.timer, installed by
// install.sh) runs this every few hours; the relay service itself is
// sandboxed and cannot replace its own binary.
//
// The newest release.json is taken from GitHub and verified against the
// release key (core/update), so only a release signed by the project is
// installed. The relay tarball for this CPU is checked against the SHA-256 in
// that signed manifest, the binary inside is swapped in atomically, and the
// service restarts. Config and data are not touched.

import (
	"archive/tar"
	"crypto/ed25519"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/update"
)

const releasesAPI = "https://api.github.com/repos/Aryalleh/mesh-msg/releases?per_page=20"

// selfUpdate returns a short report; err when the update failed.
func selfUpdate(ctx context.Context, current, exe, service string, restart bool) (string, error) {
	return selfUpdateFrom(ctx, releasesAPI, update.ReleaseKey(), current, exe, service, restart)
}

func selfUpdateFrom(ctx context.Context, api string, pub ed25519.PublicKey, current, exe, service string, restart bool) (string, error) {
	cl := &http.Client{Timeout: 2 * time.Minute}
	ch := update.ChannelOf(current)
	if !update.IsRelease(current) {
		ch = update.ChannelBeta // a dev build follows every release
	}
	m, _, err := update.FetchGitHub(ctx, cl, api, ch, pub)
	if err != nil {
		return "", fmt.Errorf("release check: %w", err)
	}
	if update.IsRelease(current) && update.Compare(m.Version, current) <= 0 {
		return fmt.Sprintf("up to date (%s, newest %s)", current, m.Version), nil
	}
	name := fmt.Sprintf("meshmsg-relay-%s-linux-%s.tar.gz", m.Version, runtime.GOARCH)
	f := m.File(name)
	if f == nil {
		return "", fmt.Errorf("release %s has no %s", m.Version, name)
	}
	dir, err := os.MkdirTemp(filepath.Dir(exe), ".relay-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	tgz := filepath.Join(dir, name)
	if err := update.Download(ctx, cl, f, tgz); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "meshmsg-relay")
	if err := extractBinary(tgz, "meshmsg-relay", bin); err != nil {
		return "", err
	}
	// The new binary must run here before it replaces the old one.
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return "", fmt.Errorf("new binary does not run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != m.Version {
		return "", fmt.Errorf("new binary says version %q, manifest %s", got, m.Version)
	}
	if err := os.Rename(bin, exe); err != nil { // same directory: atomic
		return "", err
	}
	msg := fmt.Sprintf("updated %s → %s", current, m.Version)
	if restart && service != "" {
		if err := exec.CommandContext(ctx, "systemctl", "restart", service).Run(); err != nil {
			return msg, fmt.Errorf("restart %s: %w", service, err)
		}
		msg += ", " + service + " restarted"
	}
	return msg, nil
}

// extractBinary copies the file called base (in any directory of the
// archive) out of a .tar.gz to dst, executable.
func extractBinary(tgz, base, dst string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s has no %s", filepath.Base(tgz), base)
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != base {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(tr, 256<<20))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	}
}

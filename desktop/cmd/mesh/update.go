package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/Aryalleh/mesh-msg/core/update"
)

// statusUpdateLine is the update line of `mesh status` ("" when there is nothing to say).
func statusUpdateLine() string {
	var st update.Status
	if err := call("GET", "/update", nil, &st); err != nil {
		return ""
	}
	return describeUpdate(st)
}

func describeUpdate(st update.Status) string {
	switch {
	case st.Latest != nil && st.Required:
		return fmt.Sprintf("%s is no longer supported (security release %s): run `mesh update`", st.Current, st.Latest.Version)
	case st.Latest != nil && st.Available:
		return fmt.Sprintf("%s available (you have %s): run `mesh update`", st.Latest.Version, st.Current)
	case st.Latest != nil:
		return fmt.Sprintf("up to date (%s)", st.Current)
	case st.Error != "":
		return "could not check: " + st.Error
	}
	return ""
}

// updateCmd: mesh update [check]
func updateCmd(args []string) error {
	var st update.Status
	if err := call("POST", "/update/check", nil, &st); err != nil {
		return err
	}
	if st.Latest == nil {
		if st.Error != "" {
			return errors.New("update check: " + st.Error)
		}
		fmt.Fprintf(out, "no release found (meshd %s)\n", st.Current)
		return nil
	}
	fmt.Fprintln(out, describeUpdate(st))
	if !st.Available {
		return nil
	}
	if n := st.Latest.Notes["en"]; n != "" {
		fmt.Fprintf(out, "\n%s\n\n", n)
	}
	if len(args) > 0 && args[0] == "check" {
		return nil
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("automatic install is Linux only; download it from %s", st.Latest.Page)
	}
	name := fmt.Sprintf("meshmsg-desktop-%s-linux-%s.tar.gz", st.Latest.Version, runtime.GOARCH)
	f := st.Latest.File(name)
	if f == nil {
		return fmt.Errorf("release %s has no %s", st.Latest.Version, name)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	bin := filepath.Dir(self)
	tmp, err := os.MkdirTemp("", "mesh-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	tgz := filepath.Join(tmp, name)
	fmt.Fprintf(out, "downloading %s (%.1f MB)…\n", name, float64(f.Size)/1e6)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := update.Download(ctx, &http.Client{}, f, tgz); err != nil {
		return err
	}
	fmt.Fprintln(out, "SHA-256 matches the signed release manifest")
	if err := installFromTarball(tgz, bin, "meshd", "mesh"); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed meshd and mesh %s to %s\n", st.Latest.Version, bin)
	if exec.Command("systemctl", "--user", "is-active", "--quiet", "meshd").Run() == nil {
		if err := exec.Command("systemctl", "--user", "restart", "meshd").Run(); err == nil {
			fmt.Fprintln(out, "restarted meshd (systemctl --user)")
			return nil
		}
	}
	fmt.Fprintln(out, "restart meshd to run the new version")
	return nil
}

// installFromTarball replaces the named binaries in dir with the ones in the
// release tarball (written next to them, then renamed over them).
func installFromTarball(tgz, dir string, names ...string) error {
	fh, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	staged := map[string]string{}
	defer func() {
		for _, p := range staged {
			os.Remove(p)
		}
	}()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		base := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || !slices.Contains(names, base) {
			continue
		}
		p := filepath.Join(dir, "."+base+".new")
		w, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, io.LimitReader(tr, 256<<20))
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		staged[base] = p
	}
	for _, n := range names {
		if staged[n] == "" {
			return fmt.Errorf("%s is not in the release archive", n)
		}
	}
	for _, n := range names {
		if err := os.Rename(staged[n], filepath.Join(dir, n)); err != nil {
			return err
		}
		delete(staged, n)
	}
	return nil
}

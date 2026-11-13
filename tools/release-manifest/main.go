// Command release-manifest creates the release signing key and builds the
// signed release.json for a release (roadmap B7).
//
//	release-manifest keygen KEYFILE          # new key; prints the public key for core/update
//	release-manifest sign -version 0.1.1-beta1 -dir dist -base-url URL \
//	    -notes-en en.md -notes-fa fa.md [-min-supported 0.1.0] > dist/release.json
//	release-manifest verify release.json
//
// sign reads the private key (hex seed) from $RELEASE_SIGNING_KEY and fails if it
// is missing or doesn't match core/update.ReleaseKeyHex.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-manifest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: release-manifest keygen KEYFILE | sign [flags] | verify FILE")
	os.Exit(2)
}

func keygen(args []string) error {
	if len(args) != 1 {
		usage()
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(args[0], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	fmt.Fprintln(f, hex.EncodeToString(priv.Seed()))
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(hex.EncodeToString(pub))
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	version := fs.String("version", "", "release version without v")
	dir := fs.String("dir", "dist", "directory with the release files")
	base := fs.String("base-url", "", "download URL prefix for the files")
	page := fs.String("page", "", "release page URL")
	notesEN := fs.String("notes-en", "", "English release notes file")
	notesFA := fs.String("notes-fa", "", "Persian release notes file")
	minSup := fs.String("min-supported", "", "oldest version still supported")
	fs.Parse(args)
	if *version == "" || *base == "" || *notesEN == "" || *notesFA == "" {
		return fmt.Errorf("-version, -base-url, -notes-en and -notes-fa are required")
	}
	seed, err := hex.DecodeString(strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("RELEASE_SIGNING_KEY is missing or not a 32-byte hex seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if !bytes.Equal(priv.Public().(ed25519.PublicKey), update.ReleaseKey()) {
		return fmt.Errorf("RELEASE_SIGNING_KEY does not match core/update.ReleaseKeyHex")
	}
	notes := map[string]string{}
	for lang, p := range map[string]string{"en": *notesEN, "fa": *notesFA} {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(b)) == 0 {
			return fmt.Errorf("%s is empty", p)
		}
		notes[lang] = strings.TrimSpace(string(b))
	}
	m := &update.Manifest{
		Version: *version, Channel: update.ChannelOf(*version),
		Date:         time.Now().UTC().Format(time.RFC3339),
		MinSupported: *minSup, Notes: notes, Page: *page,
	}
	ents, err := os.ReadDir(*dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range ents {
		if e.Type().IsRegular() && e.Name() != update.ManifestName && e.Name() != "SHA256SUMS" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no files in %s", *dir)
	}
	for _, n := range names {
		f, err := os.Open(filepath.Join(*dir, n))
		if err != nil {
			return err
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		m.Files = append(m.Files, update.File{
			Name: n, URL: strings.TrimRight(*base, "/") + "/" + n,
			SHA256: hex.EncodeToString(h.Sum(nil)), Size: size,
		})
	}
	out, err := update.Sign(m, priv)
	if err != nil {
		return err
	}
	if _, err := update.Verify(out, update.ReleaseKey()); err != nil {
		return fmt.Errorf("self-check: %w", err)
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}

func verify(args []string) error {
	if len(args) != 1 {
		usage()
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	m, err := update.Verify(b, update.ReleaseKey())
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s (%s, %s), %d files, min supported %q\n", m.Version, m.Channel, m.Date, len(m.Files), m.MinSupported)
	return nil
}

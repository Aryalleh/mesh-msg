package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/node"
)

// askPassphrase reads a passphrase without echo; tests replace it.
var askPassphrase = readPassphraseTTY

const identityUsage = `usage:
  mesh identity export [-o file]           print (or write) a passphrase-sealed backup
  mesh identity import [-replace] [file|-] restore from a backup (asks if no file given)`

func identityCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(identityUsage)
	}
	switch args[0] {
	case "export":
		fs := flag.NewFlagSet("export", flag.ContinueOnError)
		out := fs.String("o", "", "write the backup to this file instead of stdout")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New(identityUsage)
		}
		return exportIdentity(*out, os.Stdout)
	case "import":
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		replace := fs.Bool("replace", false, "replace a different identity already in the data dir")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 1 {
			return errors.New(identityUsage)
		}
		return importIdentity(fs.Arg(0), *replace, os.Stdin)
	}
	return errors.New(identityUsage)
}

func exportIdentity(outPath string, stdout io.Writer) error {
	id, err := node.ReadIdentity(dataDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("no identity in %s (start meshd once to create one)", dataDir)
	} else if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Backing up identity %s.\nChoose a passphrase (at least %d characters). Without it the backup is useless; with it, anyone holding the backup is you.\n",
		id.Hash(), identity.MinPassphraseLength)
	pass, err := askPassphrase("Passphrase: ")
	if err != nil {
		return err
	}
	if again, err := askPassphrase("Again: "); err != nil {
		return err
	} else if again != pass {
		return errors.New("passphrases do not match")
	}
	backup, err := id.Backup(pass)
	if err != nil {
		return err
	}
	if outPath == "" {
		fmt.Fprintln(stdout, backup)
		return nil
	}
	if err := os.WriteFile(outPath, []byte(backup+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "written to", outPath)
	return nil
}

func importIdentity(src string, replace bool, stdin io.Reader) error {
	if meshdRunning() {
		return errors.New("meshd is running: restore from the web UI (mesh ui → ⚙ Settings → Restore from backup), or stop meshd first")
	}
	var text string
	switch src {
	case "":
		fmt.Fprintln(os.Stderr, "Paste the backup (meshkey1:…) and press Enter:")
		line, err := readLine(stdin)
		if err != nil {
			return err
		}
		text = line
	case "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		text = string(b)
	default:
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		text = string(b)
	}
	pass, err := askPassphrase("Passphrase: ")
	if err != nil {
		return err
	}
	id, err := identity.FromBackup(text, pass)
	if err != nil {
		return err
	}
	replaced, err := node.RestoreIdentity(dataDir, id, replace)
	if errors.Is(err, node.ErrOtherIdentity) {
		return fmt.Errorf("%s already has a different identity; rerun with -replace to switch (the old key is kept, but its contacts and messages stay in this data dir)", dataDir)
	} else if err != nil {
		return err
	}
	if replaced != "" {
		fmt.Fprintln(os.Stderr, "previous key kept as", replaced)
	}
	fmt.Fprintf(os.Stderr, "restored identity %s — start meshd\n", id.Hash())
	return nil
}

func meshdRunning() bool {
	resp, err := client.Get("http://meshd/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// readLine reads one line byte by byte, so nothing after it is consumed.
func readLine(r io.Reader) (string, error) {
	var sb strings.Builder
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return strings.TrimRight(sb.String(), "\r"), nil
			}
			sb.WriteByte(b[0])
		}
		if err == io.EOF && sb.Len() > 0 {
			return sb.String(), nil
		}
		if err != nil {
			return "", err
		}
	}
}

// Command relays-sign signs the official relay list with the project key, so
// apps can pass it to each other over Bluetooth / LAN when GitHub and the
// relays are out of reach (network.Signed).
//
//	RELEASE_SIGNING_KEY=$(cat ~/.config/mesh-msg/release-signing.key) go run ./tools/relays-sign
//	go run ./tools/relays-sign verify
//
// It writes network/relays.json.sig next to network/relays.json. Bump
// "serial" in relays.json before signing an edited list.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Aryalleh/mesh-msg/core/update"
	"github.com/Aryalleh/mesh-msg/network"
)

const (
	listPath = "network/relays.json"
	sigPath  = listPath + network.SigSuffix
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "verify" {
		err = verify()
	} else {
		err = sign()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "relays-sign:", err)
		os.Exit(1)
	}
}

func sign() error {
	seed, err := hex.DecodeString(strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("RELEASE_SIGNING_KEY is missing or not a 32-byte hex seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if !bytes.Equal(priv.Public().(ed25519.PublicKey), update.ReleaseKey()) {
		return fmt.Errorf("RELEASE_SIGNING_KEY does not match core/update.ReleaseKeyHex")
	}
	b, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	l, err := network.Parse(b)
	if err != nil {
		return err
	}
	if l.Serial <= 0 {
		return fmt.Errorf("%s needs a positive \"serial\"", listPath)
	}
	if err := os.WriteFile(sigPath, []byte(hex.EncodeToString(network.Sign(b, priv))+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("signed %s (serial %d, %d relays) -> %s\n", listPath, l.Serial, len(l.Relays), sigPath)
	return nil
}

func verify() error {
	b, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	s, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(s)))
	if err != nil || !(network.Signed{JSON: b, Sig: sig}).Verify(nil) {
		return fmt.Errorf("%s does not match %s: re-sign it", sigPath, listPath)
	}
	fmt.Println("ok")
	return nil
}

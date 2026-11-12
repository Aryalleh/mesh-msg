//go:build !linux

package main

import (
	"fmt"
	"os"
)

// readPassphraseTTY prompts on the terminal. Echo is not turned off on this
// platform (meshd targets Linux).
func readPassphraseTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	return readLine(tty)
}

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// readPassphraseTTY prompts on the controlling terminal with echo off, so it
// works even when stdin carries the backup.
func readPassphraseTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errors.New("a passphrase must be typed on a terminal")
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", err
	}
	noEcho := *old
	noEcho.Lflag &^= unix.ECHO
	noEcho.Lflag |= unix.ICANON | unix.ISIG
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &noEcho); err != nil {
		return "", err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, old)
	fmt.Fprint(tty, prompt)
	line, err := readLine(tty)
	fmt.Fprintln(tty)
	return line, err
}

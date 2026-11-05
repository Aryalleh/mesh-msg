//go:build !linux

package node

import (
	"errors"
	"io"
)

// OpenSerial is only implemented on Linux so far; other platforms attach a
// node with AttachLoRa and their own PortOpener.
func OpenSerial(path string) (io.ReadWriteCloser, error) {
	return nil, errors.New("serial ports are not supported on this platform yet")
}

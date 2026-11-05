//go:build linux

package node

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// OpenSerial opens a USB serial (CDC-ACM) port in raw 8N1 mode at 115200
// baud. The descriptor stays non-blocking so Close unblocks a pending Read.
func OpenSerial(path string) (io.ReadWriteCloser, error) {
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var terr error
	err = rc.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if err != nil {
			terr = err
			return
		}
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
		t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | unix.B115200
		t.Ispeed, t.Ospeed = unix.B115200, unix.B115200
		t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
		terr = unix.IoctlSetTermios(int(fd), unix.TCSETS, t)
	})
	if err == nil {
		err = terr
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

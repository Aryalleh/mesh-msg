//go:build android

package direct

import (
	"net"

	"github.com/wlynxg/anet"
)

// Android 11+ forbids the netlink call net.InterfaceAddrs uses. anet reads
// the addresses another way, but only when told the Android version is 11 or
// newer (it defaults to 0 and then just calls net.InterfaceAddrs, which
// fails there: no host candidates at all). Its own reader works on older
// versions too, so it is always used, with the standard call as a fallback.
func init() { anet.SetAndroidVersion(11) }

func interfaceAddrs() ([]net.Addr, error) {
	as, err := anet.InterfaceAddrs()
	if err == nil && len(as) > 0 {
		return as, nil
	}
	if std, err2 := net.InterfaceAddrs(); err2 == nil && len(std) > 0 {
		return std, nil
	}
	return as, err
}

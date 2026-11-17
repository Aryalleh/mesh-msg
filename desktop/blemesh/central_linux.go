//go:build linux && !android

package blemesh

import (
	"context"
	"crypto/rand"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// The phones' mesh service (BleProto.kt), shared by the Mava board.
var (
	serviceUUID = mustUUID("6d657368-6d73-6700-8000-00805f9b34fb")
	rxUUID      = mustUUID("6d657368-6d73-6701-8000-00805f9b34fb")
	txUUID      = mustUUID("6d657368-6d73-6702-8000-00805f9b34fb")
)

func mustUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

const (
	scanFor   = 8 * time.Second
	scanEvery = 15 * time.Second
	retryOff  = 5 * time.Minute // no adapter / BlueZ: try again later
)

// Run joins the Bluetooth mesh through BlueZ until ctx ends: scans for phones
// and boards and connects to each (up to MaxPeers). Without a Bluetooth
// adapter it says so once and tries again every few minutes.
func Run(ctx context.Context, n *node.Node, logf func(string, ...any)) {
	l := New(NodeHost{n})
	n.Router().AddLink(l)
	n.SetLinkStatus(LinkName, "starting")
	defer func() {
		n.Router().RemoveLink(LinkName)
		n.SetLinkStatus(LinkName, "off")
	}()

	ad := bluetooth.DefaultAdapter
	warned := false
	for {
		err := ad.Enable()
		if err == nil {
			break
		}
		if !warned {
			logf("bluetooth: %v (no adapter or BlueZ not running; retrying every 5 min)", err)
			warned = true
		}
		n.SetLinkStatus(LinkName, "no Bluetooth adapter")
		if !sleep(ctx, retryOff) {
			return
		}
	}
	n.SetLinkStatus(LinkName, "no peers")
	ad.SetConnectHandler(func(d bluetooth.Device, connected bool) {
		if !connected {
			l.Down(d.Address.String())
		}
	})
	var session [4]byte
	rand.Read(session[:])
	logf("bluetooth: joining the mesh (scanning for phones and Mava boards)")

	for ctx.Err() == nil {
		found := scan(ctx, ad)
		for id, addr := range found {
			if ctx.Err() != nil || l.Count() >= MaxPeers {
				break
			}
			if l.Has(id) {
				continue
			}
			if err := connect(ad, l, id, addr, session); err != nil {
				logf("bluetooth: %s: %v", id, err)
			}
		}
		if !sleep(ctx, scanEvery) {
			break
		}
	}
	for _, id := range l.ids() {
		l.Down(id)
	}
}

// scan collects the devices advertising the mesh service for a few seconds.
func scan(ctx context.Context, ad *bluetooth.Adapter) map[string]bluetooth.Address {
	var mu sync.Mutex
	found := map[string]bluetooth.Address{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ad.Scan(func(_ *bluetooth.Adapter, r bluetooth.ScanResult) {
			if !r.HasServiceUUID(serviceUUID) {
				return
			}
			mu.Lock()
			found[r.Address.String()] = r.Address
			mu.Unlock()
		})
	}()
	sleep(ctx, scanFor)
	ad.StopScan()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return found
}

func connect(ad *bluetooth.Adapter, l *Link, id string, addr bluetooth.Address, session [4]byte) error {
	dev, err := ad.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return err
	}
	fail := func(err error) error {
		dev.Disconnect()
		return err
	}
	svcs, err := dev.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil || len(svcs) == 0 {
		return fail(errOr(err, "no mesh service"))
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{rxUUID, txUUID})
	if err != nil || len(chars) != 2 {
		return fail(errOr(err, "no mesh characteristics"))
	}
	rx, tx := chars[0], chars[1]
	mtu, err := rx.GetMTU()
	if err != nil || mtu < 23 {
		mtu = 23
	}
	// Up first (the hello is the peer's first write), then notifications.
	l.Up(id, int(mtu), Hello(session), func(b []byte) error {
		_, err := rx.Write(b)
		return err
	}, func() { dev.Disconnect() })
	if err := tx.EnableNotifications(func(b []byte) { l.Receive(id, b) }); err != nil {
		l.Down(id)
		return err
	}
	return nil
}

type msgErr string

func (e msgErr) Error() string { return string(e) }

func errOr(err error, msg string) error {
	if err != nil {
		return err
	}
	return msgErr(msg)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

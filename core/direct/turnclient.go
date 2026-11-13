package direct

import (
	"context"
	"errors"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
)

// allocateTURN gets a relayed address on a relay's TURN service (only that
// relay's admins have credentials). The relayed socket joins the checks
// like any other, and a path through it is chosen only when nothing else
// succeeded (connectivity.md §5, §7).
func (a *Agent) allocateTURN(ctx context.Context) (Candidate, error) {
	t := a.cfg.TURN
	dst, err := resolve(ctx, t.Addr)
	if err != nil {
		return Candidate{}, err
	}
	conn, err := a.cfg.Net.ListenUDP(0)
	if err != nil {
		return Candidate{}, err
	}
	quiet := logging.NewDefaultLoggerFactory()
	quiet.DefaultLogLevel = logging.LogLevelDisabled
	c, err := turn.NewClient(&turn.ClientConfig{
		TURNServerAddr: dst.String(), Username: t.Username, Password: t.Password, Realm: "mava",
		Conn: conn, LoggerFactory: quiet,
	})
	if err != nil {
		conn.Close()
		return Candidate{}, err
	}
	if err := c.Listen(); err != nil {
		c.Close()
		conn.Close()
		return Candidate{}, err
	}
	done := make(chan struct{})
	var relayed *sock
	go func() {
		defer close(done)
		pc, err := c.Allocate()
		if err == nil {
			relayed = &sock{pc: pc, relayed: true}
		} else {
			relayed = nil
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		c.Close()
		conn.Close()
		<-done
		return Candidate{}, ctx.Err()
	}
	if relayed == nil {
		c.Close()
		conn.Close()
		return Candidate{}, errors.New("direct: TURN allocation refused")
	}
	a.addSock(relayed)
	a.mu.Lock()
	a.release = append(a.release, func() { relayed.pc.Close(); c.Close(); conn.Close() })
	a.mu.Unlock()
	ap := addrPort(relayed.pc.LocalAddr())
	return Candidate{Kind: Relayed, Addr: ap, Priority: priority(Relayed, ap.Addr())}, nil
}

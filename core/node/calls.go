package node

import (
	"context"
	"errors"

	"github.com/Aryalleh/mesh-msg/core/call"
	"github.com/Aryalleh/mesh-msg/core/direct"
	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
)

// Calls (docs/calls.md): signaling as content type 7 over any path, audio
// over a direct path only. The app captures, encodes, decodes and plays;
// the node moves frames.

func (n *Node) newCalls() *call.Manager {
	return call.NewManager(call.Env{
		Self: n.id,
		Send: func(peer identity.Public, sig []byte) error {
			// Priority: ringing must not wait behind queued messages.
			_, err := n.router.SendContent(n.id, peer, e2e.ContentCall, sig, n.heardFrom(peer.Hash().String()), true)
			return err
		},
		Direct: func(ctx context.Context) direct.Config {
			if n.direct != nil {
				return n.direct.config(ctx)
			}
			return direct.Config{Logf: n.cfg.Logf}
		},
		Allowed: func(peer identity.Public) bool {
			if n.cfg.CallsFromAnyone {
				return true
			}
			_, err := n.findContact(peer.Hash().String())
			return err == nil
		},
		Event: func(in call.Info) {
			n.emit(Event{Type: "call", Call: &in, Info: in.ID})
		},
		Logf: n.cfg.Logf,
	})
}

// VoiceCaps is the audio offer for the codecs the app can encode and
// decode, best first (call.CodecOpus, call.CodecAMRWB).
func VoiceCaps(codecs ...byte) call.Caps {
	return call.Caps{Version: call.Version, Streams: []call.StreamCap{
		{ID: 0, Media: call.MediaAudio, Dir: call.DirBoth, Codecs: codecs, MaxKbps: 24},
	}}
}

// StartCall calls a contact.
func (n *Node) StartCall(ref string, codecs ...byte) (call.Info, error) {
	c, err := n.findContact(ref)
	if err != nil {
		return call.Info{}, err
	}
	cl, err := n.calls.Start(c.pub, VoiceCaps(codecs...))
	if err != nil {
		return call.Info{}, err
	}
	return cl.Info(), nil
}

var errNoCall = errors.New("node: no such call")

func (n *Node) getCall(id string) (*call.Call, error) {
	if c := n.calls.Get(id); c != nil {
		return c, nil
	}
	return nil, errNoCall
}

// AnswerCall accepts a ringing call.
func (n *Node) AnswerCall(id string, codecs ...byte) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	return c.Answer(VoiceCaps(codecs...))
}

// EndCall hangs up, cancels or declines.
func (n *Node) EndCall(id string) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	c.Hangup()
	return nil
}

// MuteCall tells the other side we muted (the app stops sending audio).
func (n *Node) MuteCall(id string, muted bool) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	c.SetMuted(muted)
	return nil
}

// CallAudio sends one encoded 20 ms audio frame.
func (n *Node) CallAudio(id string, frame []byte) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	return c.SendAudio(frame)
}

// PullCallAudio gives the next frame to play (call.PullFrame, PullLost to
// conceal, PullEmpty for silence); call it every 20 ms.
func (n *Node) PullCallAudio(id string) ([]byte, int) {
	c := n.calls.Get(id)
	if c == nil {
		return nil, call.PullEmpty
	}
	return c.PullAudio()
}

// CurrentCall is the call in progress, if any.
func (n *Node) CurrentCall() (call.Info, bool) {
	if c := n.calls.Current(); c != nil {
		return c.Info(), true
	}
	return call.Info{}, false
}

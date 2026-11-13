package node

import (
	"context"
	"errors"
	"time"

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

// CallCaps is the offer for these audio and video codecs (best first); no
// video codecs: voice only. cameraOn marks a video call (the callee sees
// "video call").
func CallCaps(audio, video []byte, cameraOn bool) call.Caps {
	c := VoiceCaps(audio...)
	if len(video) > 0 {
		c.Streams = append(c.Streams, call.VideoCap(video, call.VideoParams{Width: 1280, Height: 720, FPS: 24, CameraOn: cameraOn}))
	}
	return c
}

// StartCall calls a contact (voice).
func (n *Node) StartCall(ref string, codecs ...byte) (call.Info, error) {
	return n.StartCallWith(ref, codecs, nil, false)
}

// StartCallWith calls a contact with these audio and video codecs; cameraOn
// makes it a video call.
func (n *Node) StartCallWith(ref string, audio, video []byte, cameraOn bool) (call.Info, error) {
	c, err := n.findContact(ref)
	if err != nil {
		return call.Info{}, err
	}
	cl, err := n.calls.Start(c.pub, CallCaps(audio, video, cameraOn))
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

// AnswerCall accepts a ringing call (voice).
func (n *Node) AnswerCall(id string, codecs ...byte) error { return n.AnswerCallWith(id, codecs, nil) }

// AnswerCallWith accepts a ringing call; with video codecs, video is on
// when the caller offered it too (camera on or off is up to the app).
func (n *Node) AnswerCallWith(id string, audio, video []byte) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	return c.Answer(CallCaps(audio, video, false))
}

// CallVideo sends one encoded video frame.
func (n *Node) CallVideo(id string, frame []byte, keyframe bool) error {
	c, err := n.getCall(id)
	if err != nil {
		return err
	}
	return c.SendVideo(frame, keyframe)
}

// PullCallVideo waits up to timeout for the next video frame to decode.
func (n *Node) PullCallVideo(id string, timeout time.Duration) (call.VideoFrame, bool) {
	c := n.calls.Get(id)
	if c == nil {
		return call.VideoFrame{}, false
	}
	return c.PullVideo(timeout)
}

// CallVideoNeeds: does the other side need a keyframe, and the bitrate to encode at.
func (n *Node) CallVideoNeeds(id string) (keyframe bool, kbps int) {
	c := n.calls.Get(id)
	if c == nil {
		return false, 0
	}
	return c.VideoNeeds()
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

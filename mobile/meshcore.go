// Package meshcore is the gomobile binding of the core for the Android app
// (docs/android.md §2, §3; docs/core.md "Interface contract"):
//
//	gomobile bind -target=android -androidapi 26 -o android/app/libs/meshcore.aar ./mobile
//
// gomobile only carries basic types, byte slices, errors and interfaces
// across the language boundary, so lists and structs travel as JSON (the
// same JSON meshd serves to its web UI).
//
// The Go side owns identity, crypto, routing, the stores and the relay /
// gateway / bridge sessions. Kotlin owns the radios and hands them over as:
//
//   - Transport: a point-to-point link with nearby phones (BLE GATT). Kotlin
//     reports peers coming and going and moves opaque chunks; Go frames
//     packets into chunks that fit the link (a GATT write is ~500 bytes, a
//     packet up to 64 KiB).
//   - StreamOpener: the byte stream to a LoRa node (BLE or USB-OTG serial),
//     speaking core/hostlink exactly as meshd does over USB.
package meshcore

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Aryalleh/mesh-msg/core/media"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/node"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/router"
)

// Listener receives node events as JSON: {"type":"message|delivered|link|contact|group",…}.
// Calls come from a Go thread, one at a time.
type Listener interface {
	OnEvent(json string)
}

// NetworkBinder ties a UDP socket to the network that owns a local address
// (Android: Network.bindSocket on the descriptor). Without it Android sends
// the socket's packets through the default network, which is mobile data
// when the Wi-Fi has no internet, and phones on one Wi-Fi can't reach each
// other. The descriptor is only valid during the call.
type NetworkBinder interface {
	BindSocket(fd int32, ip string)
	// LocalAddrs lists this phone's addresses on its up interfaces, comma
	// separated (java.net.NetworkInterface): the core's own listing is not
	// allowed on every Android version.
	LocalAddrs() string
}

// SetNetworkBinder sets the binder; call it before Start.
func (c *Core) SetNetworkBinder(b NetworkBinder) {
	if b == nil {
		return
	}
	c.n.SetSocketBinder(
		func(fd int, ip string) { b.BindSocket(int32(fd), ip) },
		func() []string { return strings.Split(b.LocalAddrs(), ",") },
	)
}

// Transport is a point-to-point link implemented in Kotlin. Send must not
// block for long (queue the chunk); chunks for one peer must go out in order.
type Transport interface {
	Send(peer string, chunk []byte)
}

// Stream is an open byte stream to a LoRa node.
type Stream interface {
	// Read blocks until bytes arrive; an error ends the session.
	Read() ([]byte, error)
	Write(b []byte) error
	Close() error
}

// StreamOpener opens the stream; it is called again after a failure.
type StreamOpener interface {
	Open() (Stream, error)
}

// Core is a running node.
type Core struct {
	n      *node.Node
	dir    string
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	started  bool
	links    map[string]*chunkLink
	loras    map[string]context.CancelFunc
	listener Listener
	relays   []node.RelayConfig // configured relays (update check)
}

// Open loads or creates the identity and databases in dataDir. configJSON is
// a node config ({"name":…,"delegate":true,"relays":[{"name","url","via"}],
// "gateway":{"enabled","mode","quota_kb_per_hour"},"bridges":[…]}); "" = defaults.
// LAN meshing is off unless the config enables it.
func Open(dataDir, configJSON string) (*Core, error) {
	cfg := node.Config{Delegate: true}
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
	}
	cfg.DataDir = dataDir
	cfg.LoRa = node.LoRaConfig{} // nodes are attached with AttachLoRa
	n, err := node.Open(cfg)
	if err != nil {
		return nil, err
	}
	return &Core{relays: cfg.Relays, n: n, dir: dataDir, done: make(chan struct{}), links: map[string]*chunkLink{}, loras: map[string]context.CancelFunc{}}, nil
}

// SetListener sets (or clears, with nil) the event listener.
func (c *Core) SetListener(l Listener) {
	c.mu.Lock()
	c.listener = l
	c.mu.Unlock()
}

// Start runs the node (relay sessions, announces, timers) until Stop.
func (c *Core) Start() {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.mu.Unlock()

	events, unsub := c.n.Subscribe()
	go func() {
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				c.mu.Lock()
				l := c.listener
				c.mu.Unlock()
				if l != nil {
					b, _ := json.Marshal(e)
					l.OnEvent(string(b))
				}
			}
		}
	}()
	go func() {
		defer close(c.done)
		if err := c.n.Run(ctx); err != nil {
			c.n.SetLinkStatus("core", "error: "+err.Error())
		}
	}()
}

// Stop ends every session and closes the databases. The Core is unusable
// afterwards; Open a new one to continue.
func (c *Core) Stop() {
	c.mu.Lock()
	started := c.started
	cancel := c.cancel
	for _, stop := range c.loras {
		stop()
	}
	c.mu.Unlock()
	if started {
		cancel()
		<-c.done
	}
	c.n.Close()
}

// Wipe stops the node and erases the identity, contacts, messages and stored
// packets (panic wipe, docs/android.md §8). Files are overwritten with zeros
// before removal; flash wear-levelling may still keep old blocks, so the data
// directory should also sit on encrypted storage.
func (c *Core) Wipe() error {
	c.Stop()
	return wipeDir(c.dir)
}

func wipeDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		p := dir + string(os.PathSeparator) + e.Name()
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			if f, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
				zeros := make([]byte, 64*1024)
				for left := info.Size(); left > 0; left -= int64(len(zeros)) {
					k := int64(len(zeros))
					if left < k {
						k = left
					}
					f.Write(zeros[:k])
				}
				f.Sync()
				f.Close()
			}
		}
		errs = append(errs, os.RemoveAll(p))
	}
	return errors.Join(errs...)
}

// ---- identity, contacts, messages ----

// URI is this identity's contact link (meshmsg:…), for QR codes and sharing.
func (c *Core) URI() string { return c.n.Identity().URI() }

// Link is the contact link as https://…/add#… for sharing as text: messengers
// make it clickable and it opens the app (AddContact accepts both forms).
func (c *Core) Link() string { return c.n.Identity().Link() }

// ExportIdentity returns the account key sealed with passphrase
// (meshkey1:…, fits a QR code). It fails if the passphrase is shorter than
// MinPassphraseLength characters. Slow (~1 s): call it off the main thread.
func (c *Core) ExportIdentity(passphrase string) (string, error) {
	return c.n.BackupIdentity(passphrase)
}

// MinPassphraseLength is the shortest passphrase ExportIdentity accepts.
const MinPassphraseLength = identity.MinPassphraseLength

// RestoreIdentity opens a backup made by ExportIdentity and installs it as
// the identity in dataDir, before Open. A dataDir that already holds a
// different identity is refused unless replace is set. Returns the restored
// identity's hash. Errors start with "passphrase:" (wrong passphrase or
// damaged backup) or "format:" (not a backup) so the UI can explain them.
// Slow (~1 s): call it off the main thread.
func RestoreIdentity(dataDir, backup, passphrase string, replace bool) (string, error) {
	id, err := identity.FromBackup(backup, passphrase)
	switch {
	case errors.Is(err, identity.ErrBadPassphrase):
		return "", fmt.Errorf("passphrase: %w", err)
	case errors.Is(err, identity.ErrNotBackup):
		return "", fmt.Errorf("format: %w", err)
	case err != nil:
		return "", err
	}
	if _, err := node.RestoreIdentity(dataDir, id, replace); err != nil {
		return "", err
	}
	return id.Hash().String(), nil
}

// Hash is this identity's destination hash (hex).
func (c *Core) Hash() string { return c.n.Identity().Hash().String() }

func toJSON(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// Status returns a node.Status as JSON (links, stored packets, router
// counters, attached LoRa nodes).
func (c *Core) Status() string {
	s, _ := toJSON(c.n.Status(), nil)
	return s
}

// Chats returns [{"contact":{hash,name,uri},"last":{message},"unread":n}], newest first.
func (c *Core) Chats() (string, error) {
	cs, err := c.n.Chats()
	if cs == nil {
		cs = []node.Chat{}
	}
	return toJSON(cs, err)
}

// MarkRead clears a chat's unread count.
func (c *Core) MarkRead(contact string) error { return c.n.MarkRead(contact) }

// AddContact saves a contact from a meshmsg: link; name overrides the link's.
func (c *Core) AddContact(uri, name string) (string, error) {
	return toJSON(c.n.AddContact(uri, name))
}

func (c *Core) RenameContact(contact, name string) error { return c.n.RenameContact(contact, name) }
func (c *Core) RemoveContact(contact string) error       { return c.n.RemoveContact(contact) }

// Send queues a text for a contact (hash, name or hash prefix) and returns
// the stored message as JSON.
func (c *Core) Send(contact, text string) (string, error) {
	return toJSON(c.n.Send(contact, text))
}

// Messages returns up to limit messages of a chat as JSON, oldest first.
func (c *Core) Messages(contact string, limit int) (string, error) {
	ms, err := c.n.Messages(contact, limit)
	if ms == nil {
		ms = []node.Message{}
	}
	return toJSON(ms, err)
}

// ---- groups and public chats ----
// Send, Messages and MarkRead take a group's hash too; Chats lists groups as
// {"group":{hash,name,title,kind,uri,owner,is_owner,removed,members},"last":…,"unread":n}.
// Outgoing private-group messages carry "recipients" and "delivered_to".

// Groups returns the joined groups as JSON [{hash,name,title,kind,…,members}].
func (c *Core) Groups() (string, error) {
	gs, err := c.n.Groups()
	if gs == nil {
		gs = []node.Group{}
	}
	return toJSON(gs, err)
}

// ---- media: photos, voice and video messages, files ----
// Messages JSON carries "media":{id,mime,name,size,state,…}; state is ready |
// remote (tap to download) | downloading | missing. "media" events report
// changes.

// SendMedia sends the file at path to a contact or private group. important:
// also through relays (reaches offline receivers, a few per day); the
// message's media "note" says when it went as a normal file instead.
func (c *Core) SendMedia(peer, path, mime, name, caption string, important bool) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > media.MaxFileBytes {
		return "", fmt.Errorf("file larger than %d MB", media.MaxFileBytes>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return toJSON(c.n.SendMedia(peer, b, mime, name, caption, important))
}

// FetchMedia downloads a received file in the background.
func (c *Core) FetchMedia(id string) error { return c.n.FetchMedia(id) }

// MediaPath decrypts a downloaded file into the app's cache and returns its
// path (the same path on later calls).
func (c *Core) MediaPath(id string) (string, error) {
	mi, ok := c.n.MediaInfo(id)
	if !ok {
		return "", fmt.Errorf("no file %q", id)
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 32 {
			return '_'
		}
		return r
	}, mi.Name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	dir := filepath.Join(c.dir, "media-cache", mi.ID)
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	file, _, err := c.n.MediaFile(mi.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, file, 0o600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// KnownRelays returns the relays found automatically (official list, relays'
// lists, nearby devices) as JSON [{url,name,region,source,state,active,config,…}].
func (c *Core) KnownRelays() string {
	ks := c.n.KnownRelays()
	if ks == nil {
		ks = []node.KnownRelay{}
	}
	s, _ := toJSON(ks, nil)
	return s
}

// Folders returns the user's chat folders as JSON [{name, chats:[hash]}].
func (c *Core) Folders() (string, error) { return toJSON(c.n.Folders(), nil) }

// SetFolders replaces the chat folders (JSON as Folders returns).
func (c *Core) SetFolders(js string) error {
	var fs []node.Folder
	if err := json.Unmarshal([]byte(js), &fs); err != nil {
		return err
	}
	return c.n.SetFolders(fs)
}

// StoredFiles lists the files kept on this device (JSON StorageFilter, ""
// for all), newest first.
func (c *Core) StoredFiles(filter string) (string, error) {
	var f node.StorageFilter
	if filter != "" {
		if err := json.Unmarshal([]byte(filter), &f); err != nil {
			return "", err
		}
	}
	return toJSON(c.n.StoredFiles(f))
}

// DeleteFiles deletes the files a JSON StorageFilter picks; it returns
// {"files": n, "bytes": freed}.
func (c *Core) DeleteFiles(filter string) (string, error) {
	var f node.StorageFilter
	if err := json.Unmarshal([]byte(filter), &f); err != nil {
		return "", err
	}
	k, b, err := c.n.DeleteFiles(f)
	return toJSON(map[string]int64{"files": int64(k), "bytes": b}, err)
}

// SetAvatar sets this account's profile picture (JPEG/PNG/WebP, ≤ 64 KB);
// empty removes it. Contacts fetch it over direct links when they ask.
func (c *Core) SetAvatar(img []byte) error { return c.n.SetAvatar(img) }

// AvatarPath is the file with hash's picture ("me" for this account), or "".
func (c *Core) AvatarPath(hash string) string {
	if hash == "me" {
		hash = c.n.Identity().Hash().String()
	}
	return c.n.AvatarPath(hash)
}

// RequestAvatar asks for a contact's picture over a direct link (opening one
// if needed); an "avatar" event tells when it arrived.
func (c *Core) RequestAvatar(hash string) { c.n.RequestAvatar(hash) }

// ForgetRelay stops using a discovered relay and never picks it up again.
func (c *Core) ForgetRelay(ref string) error { return c.n.ForgetRelay(ref) }

// MergeContact moves the chat with oldRef (an account the contact lost)
// into the chat with newRef and removes oldRef; it returns how many
// messages moved.
func (c *Core) MergeContact(oldRef, newRef string) (int, error) {
	return c.n.MergeContact(oldRef, newRef)
}

// ReadReceipts reports whether read receipts are sent (relays only).
func (c *Core) ReadReceipts() bool { return c.n.ReadReceipts() }

// SetReadReceipts turns sending read receipts on or off.
func (c *Core) SetReadReceipts(on bool) error { return c.n.SetReadReceipts(on) }

// CreateGroup makes a private group with contacts (a JSON array of hashes or
// names) and returns it as JSON. Each message goes to every member separately.
func (c *Core) CreateGroup(name, membersJSON string) (string, error) {
	var ms []string
	if membersJSON != "" {
		if err := json.Unmarshal([]byte(membersJSON), &ms); err != nil {
			return "", err
		}
	}
	return toJSON(c.n.CreateGroup(name, ms))
}

// AddMembers adds contacts (JSON array) to a private group we own.
func (c *Core) AddMembers(group, membersJSON string) (string, error) {
	var ms []string
	if err := json.Unmarshal([]byte(membersJSON), &ms); err != nil {
		return "", err
	}
	return toJSON(c.n.AddMembers(group, ms))
}

// RemoveMember removes a member (hash) from a private group we own.
func (c *Core) RemoveMember(group, member string) (string, error) {
	return toJSON(c.n.RemoveMember(group, member))
}

// JoinGroup joins a public chat ("global", "#tehran", meshgroup:#tehran) and
// returns it as JSON.
func (c *Core) JoinGroup(link string) (string, error) { return toJSON(c.n.JoinGroup(link)) }

// LeaveGroup leaves a group (hash, name or title).
func (c *Core) LeaveGroup(group string) error { return c.n.LeaveGroup(group) }

// Regions returns suggested regional chat names as a JSON array.
func Regions() string {
	s, _ := toJSON(node.Regions, nil)
	return s
}

// ---- point-to-point transports (BLE mesh) ----

// AddTransport registers a point-to-point link. chunk is the largest chunk
// the transport carries (e.g. GATT MTU − 3); packets are split to fit.
func (c *Core) AddTransport(name string, chunk int, t Transport) error {
	if t == nil || chunk < 20 {
		return errors.New("meshcore: transport needs chunks of at least 20 bytes")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.links[name]; dup {
		return fmt.Errorf("meshcore: transport %q already added", name)
	}
	l := &chunkLink{name: name, chunk: chunk, t: t, rx: map[string][]byte{}}
	c.links[name] = l
	c.n.Router().AddLink(l)
	c.n.SetLinkStatus(name, "no peers")
	return nil
}

// RemoveTransport unregisters a transport (Bluetooth turned off, stealth mode).
func (c *Core) RemoveTransport(name string) {
	c.mu.Lock()
	l := c.links[name]
	delete(c.links, name)
	c.mu.Unlock()
	if l != nil {
		c.n.Router().RemoveLink(name)
		c.n.SetLinkStatus(name, "off")
	}
}

func (c *Core) link(name string) (*chunkLink, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l := c.links[name]; l != nil {
		return l, nil
	}
	return nil, fmt.Errorf("meshcore: no transport %q", name)
}

// PeerUp tells the router a peer is connected: it then receives everything
// stored that it may be able to carry or deliver.
func (c *Core) PeerUp(transport, peer string) error {
	l, err := c.link(transport)
	if err != nil {
		return err
	}
	count := l.up(peer)
	c.n.SetLinkStatus(transport, peerCount(count))
	c.n.Router().PeerUp(transport, router.PeerID(peer))
	c.n.RetryMedia() // a neighbour may hold files we wait for
	return nil
}

// PeerDown tells the router the peer is gone.
func (c *Core) PeerDown(transport, peer string) error {
	l, err := c.link(transport)
	if err != nil {
		return err
	}
	count := l.down(peer)
	c.n.Router().PeerDown(transport, router.PeerID(peer))
	c.n.SetLinkStatus(transport, peerCount(count))
	return nil
}

func peerCount(n int) string {
	switch n {
	case 0:
		return "no peers"
	case 1:
		return "1 peer"
	}
	return fmt.Sprintf("%d peers", n)
}

// Receive feeds one chunk received from a peer. An error means the stream is
// corrupt: drop the connection (PeerDown) and reconnect.
func (c *Core) Receive(transport, peer string, chunk []byte) error {
	l, err := c.link(transport)
	if err != nil {
		return err
	}
	pkts, err := l.feed(peer, chunk)
	for _, p := range pkts {
		c.n.Inbound(transport, router.PeerID(peer), p) // packets and media frames
	}
	return err
}

// chunkLink frames packets as len(3, big endian) || packet and cuts the
// stream into chunks. The transport is ordered and reliable per peer (GATT
// writes with response / indications), so there is no checksum: packets are
// authenticated end to end and a desynchronised stream fails the length check.
type chunkLink struct {
	name  string
	chunk int
	t     Transport

	mu sync.Mutex // also serialises Send, keeping a packet's chunks together
	rx map[string][]byte
}

const frameHeader = 3

func (l *chunkLink) Name() string          { return l.name }
func (l *chunkLink) Kind() router.LinkKind { return router.PointToPoint }
func (l *chunkLink) MTU() int              { return packet.MaxSize }

func (l *chunkLink) Send(peer router.PeerID, raw []byte) {
	if len(raw) == 0 || len(raw) > packet.MaxSize {
		return
	}
	buf := make([]byte, 0, frameHeader+len(raw))
	buf = append(buf, byte(len(raw)>>16), byte(len(raw)>>8), byte(len(raw)))
	buf = append(buf, raw...)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.rx[string(peer)]; !ok {
		return // peer left
	}
	for len(buf) > 0 {
		k := min(l.chunk, len(buf))
		l.t.Send(string(peer), buf[:k:k])
		buf = buf[k:]
	}
}

func (l *chunkLink) up(peer string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rx[peer] = []byte{}
	return len(l.rx)
}

func (l *chunkLink) down(peer string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.rx, peer)
	return len(l.rx)
}

func (l *chunkLink) feed(peer string, chunk []byte) ([][]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	buf, ok := l.rx[peer]
	if !ok {
		return nil, fmt.Errorf("meshcore: chunk from unknown peer %q", peer)
	}
	buf = append(buf, chunk...)
	var out [][]byte
	for len(buf) >= frameHeader {
		n := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
		if n == 0 || n > packet.MaxSize {
			l.rx[peer] = []byte{}
			return out, fmt.Errorf("meshcore: bad frame length %d from %q", n, peer)
		}
		if len(buf) < frameHeader+n {
			break
		}
		out = append(out, append([]byte(nil), buf[frameHeader:frameHeader+n]...))
		buf = buf[frameHeader+n:]
	}
	l.rx[peer] = append([]byte(nil), buf...)
	return out, nil
}

// ---- LoRa node ----

// AttachLoRa starts a session with a LoRa node over the stream from opener
// (reopened after failures) and returns at once. Detach with DetachLoRa.
func (c *Core) AttachLoRa(name string, opener StreamOpener) error {
	if opener == nil {
		return errors.New("meshcore: nil opener")
	}
	c.mu.Lock()
	if _, dup := c.loras[name]; dup {
		c.mu.Unlock()
		return fmt.Errorf("meshcore: LoRa node %q already attached", name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.loras[name] = cancel
	c.mu.Unlock()
	go c.n.AttachLoRa(ctx, name, func() (io.ReadWriteCloser, error) {
		s, err := opener.Open()
		if err != nil {
			return nil, err
		}
		return &streamRW{s: s}, nil
	})
	return nil
}

// DetachLoRa ends the session with a node.
func (c *Core) DetachLoRa(name string) {
	c.mu.Lock()
	cancel := c.loras[name]
	delete(c.loras, name)
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// LoRaConfigure changes node settings; patchJSON holds any of role, region,
// preset, freq_hz, tx_dbm, name, announce_min.
func (c *Core) LoRaConfigure(name, patchJSON string) error {
	var p node.LoRaPatch
	if err := json.Unmarshal([]byte(patchJSON), &p); err != nil {
		return err
	}
	return c.n.LoRaConfigure(name, p)
}

// streamRW adapts a Stream (whole chunks per Read) to io.ReadWriteCloser.
type streamRW struct {
	s    Stream
	rest []byte
}

func (r *streamRW) Read(p []byte) (int, error) {
	for len(r.rest) == 0 {
		b, err := r.s.Read()
		if err != nil {
			return 0, err
		}
		r.rest = b
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

func (r *streamRW) Write(p []byte) (int, error) {
	if err := r.s.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (r *streamRW) Close() error { return r.s.Close() }

// ---- text size (for the composer's counter) ----

// TextBytes is the size of text as it travels (Persian is stored compactly).
func TextBytes(text string) int { return message.TextBytes(text) }

// OnePacketBytes is how many text bytes fit in one LoRa packet; shorter
// messages need a single transmission per hop.
func OnePacketBytes() int { return message.OnePacketTextBytes }

// MaxTextBytes is the longest text Send accepts.
func MaxTextBytes() int { return message.MaxTextBytes }

// ---- calls (docs/calls.md) ----
//
// codecs: the audio codecs this phone can encode and decode, best first,
// as ids: "1,2" (Opus, AMR-WB) or "2". Call state arrives as "call" events.

func parseCodecs(s string) []byte {
	var out []byte
	for _, f := range strings.Split(s, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && v > 0 && v < 256 {
			out = append(out, byte(v))
		}
	}
	return out
}

// StartCall calls a contact; returns the call as JSON.
func (c *Core) StartCall(contact, codecs string) (string, error) {
	return toJSON(c.n.StartCall(contact, parseCodecs(codecs)...))
}

// AnswerCall accepts a ringing call.
func (c *Core) AnswerCall(id, codecs string) error { return c.n.AnswerCall(id, parseCodecs(codecs)...) }

// StartVideoCall calls a contact offering video too (codec lists like
// "17,16"); cameraOn: a video call, the other phone shows it so.
func (c *Core) StartVideoCall(contact, audioCodecs, videoCodecs string, cameraOn bool) (string, error) {
	return toJSON(c.n.StartCallWith(contact, parseCodecs(audioCodecs), parseCodecs(videoCodecs), cameraOn))
}

// AnswerCallVideo accepts a ringing call, with video if both sides have a
// common video codec.
func (c *Core) AnswerCallVideo(id, audioCodecs, videoCodecs string) error {
	return c.n.AnswerCallWith(id, parseCodecs(audioCodecs), parseCodecs(videoCodecs))
}

// SendCallVideo sends one encoded video frame.
func (c *Core) SendCallVideo(id string, frame []byte, keyframe bool) error {
	return c.n.CallVideo(id, frame, keyframe)
}

// PullCallVideo waits up to timeoutMs for the next video frame: flags(1:
// bit 0 keyframe) | timestamp ms(4) | frame, or empty when none came.
func (c *Core) PullCallVideo(id string, timeoutMs int) []byte {
	f, ok := c.n.PullCallVideo(id, time.Duration(timeoutMs)*time.Millisecond)
	if !ok {
		return nil
	}
	b := make([]byte, 5, 5+len(f.Data))
	if f.Keyframe {
		b[0] = 1
	}
	binary.BigEndian.PutUint32(b[1:], f.TS)
	return append(b, f.Data...)
}

// CallVideoNeeds is the bitrate to encode at in kbps times 2, plus 1 when
// the other side needs a keyframe now.
func (c *Core) CallVideoNeeds(id string) int {
	key, kbps := c.n.CallVideoNeeds(id)
	v := kbps * 2
	if key {
		v++
	}
	return v
}

// EndCall hangs up, cancels or declines.
func (c *Core) EndCall(id string) error { return c.n.EndCall(id) }

// MuteCall tells the other side the microphone is muted.
func (c *Core) MuteCall(id string, muted bool) error { return c.n.MuteCall(id, muted) }

// CallAudio sends one encoded 20 ms frame.
func (c *Core) CallAudio(id string, frame []byte) error { return c.n.CallAudio(id, frame) }

// PullCallAudio returns the next frame to play every 20 ms, as one byte of
// result (0 frame, 1 lost: conceal, 2 nothing: silence) followed by the
// frame.
func (c *Core) PullCallAudio(id string) []byte {
	p, r := c.n.PullCallAudio(id)
	return append([]byte{byte(r)}, p...)
}

// CurrentCall is the call in progress as JSON, or "" when there is none.
func (c *Core) CurrentCall() string {
	in, ok := c.n.CurrentCall()
	if !ok {
		return ""
	}
	s, _ := toJSON(in, nil)
	return s
}

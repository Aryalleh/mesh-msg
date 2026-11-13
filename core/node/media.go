package node

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/group"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/media"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/router"
	"github.com/Aryalleh/mesh-msg/network"
)

// Media messages (photos, files, voice and video messages).
//
// The chat message carries only a core/media reference; the encrypted file
// moves outside the packet mesh, so it never floods LoRa or delays chats:
//   - directly between neighbours (LAN, Bluetooth): the receiver pulls the
//     blob in small windows (frames below), interleaved with packets;
//   - only files the sender marks important also go through relays that
//     accept media (relay info media_max_mb), within the account's daily
//     allowance there; a relay deletes it once every recipient fetched it or
//     after a week. Normal files never touch a relay: the receiver waits for
//     a direct path to the sender (or anyone who has it).
//
// Every node keeps the blobs it sent or fetched (data dir media/) and serves
// them to neighbours that ask, so a file also spreads hop by hop in a group.

// MediaConfig controls media.
type MediaConfig struct {
	// AutoDownloadMB: files up to this size are fetched without asking
	// (default 2; negative: never).
	AutoDownloadMB int `toml:"auto_download_mb" json:"auto_download_mb,omitempty"`
}

// MediaInfo describes a message's file.
type MediaInfo struct {
	ID    string `json:"id"`
	Mime  string `json:"mime"`
	Name  string `json:"name"`
	Size  int    `json:"size"`  // file bytes
	State string `json:"state"` // ready | remote | downloading | missing
	// Downloading: blob bytes so far / total.
	Have  int64 `json:"have,omitempty"`
	Total int   `json:"total,omitempty"`
	// Outgoing: relays it is uploaded to / chosen.
	Uploaded int `json:"uploaded,omitempty"`
	Relays   int `json:"relays,omitempty"`
	// Important: it goes through relays too (reaches offline receivers).
	Important bool `json:"important,omitempty"`
	// Note: set on a just-sent file that was asked to be important but went
	// as a normal one (allowance used up, no relay takes media).
	Note string `json:"note,omitempty"`
}

// Kind is image | audio | video | round | file, from the type ("round": a
// round video message, shown as a circle that plays in the chat).
func (m MediaInfo) Kind() string {
	if media.IsRound(m.Mime, m.Name) {
		return "round"
	}
	for _, k := range []string{"image", "audio", "video"} {
		if strings.HasPrefix(m.Mime, k+"/") {
			return k
		}
	}
	return "file"
}

const mediaSchema = `
CREATE TABLE IF NOT EXISTS media (
	id       TEXT PRIMARY KEY,           -- blob id (hex)
	key      BLOB NOT NULL,
	size     INTEGER NOT NULL,           -- blob bytes
	mime     TEXT NOT NULL DEFAULT '',
	name     TEXT NOT NULL DEFAULT '',
	urls     TEXT NOT NULL DEFAULT '',   -- relays that hold it (newline separated)
	pending  TEXT NOT NULL DEFAULT '',   -- outgoing: relays still to upload to
	uploaded INTEGER NOT NULL DEFAULT 0,
	readers  INTEGER NOT NULL DEFAULT 1,
	state    TEXT NOT NULL,              -- ready | remote | downloading | missing
	wanted   INTEGER NOT NULL DEFAULT 0, -- the user asked for it (or auto)
	created  INTEGER NOT NULL,
	last_try INTEGER NOT NULL DEFAULT 0,
	important INTEGER NOT NULL DEFAULT 0 -- goes through relays (protocol.md §6b)
);`

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

type mediaRow struct {
	id                string
	key               [32]byte
	size              int
	mime, name        string
	urls, pending     []string
	uploaded, readers int
	state             string
	wanted            bool
	created, lastTry  int64
	important         bool
}

func (d *appDB) mediaRow(id string) (mediaRow, bool) {
	var r mediaRow
	var key []byte
	var urls, pending string
	err := d.db.QueryRow(`SELECT id,key,size,mime,name,urls,pending,uploaded,readers,state,wanted,created,last_try,important FROM media WHERE id=?`, id).
		Scan(&r.id, &key, &r.size, &r.mime, &r.name, &urls, &pending, &r.uploaded, &r.readers, &r.state, &r.wanted, &r.created, &r.lastTry, &r.important)
	if err != nil || len(key) != 32 {
		return r, false
	}
	copy(r.key[:], key)
	r.urls, r.pending = lines(urls), lines(pending)
	return r, true
}

func (d *appDB) putMediaRow(r mediaRow) error {
	_, err := d.db.Exec(`INSERT OR IGNORE INTO media(id,key,size,mime,name,urls,pending,readers,state,wanted,created,important) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.id, r.key[:], r.size, r.mime, r.name, strings.Join(r.urls, "\n"), strings.Join(r.pending, "\n"), r.readers, r.state, r.wanted, time.Now().Unix(), r.important)
	return err
}

func (d *appDB) mediaInfo(id string) (MediaInfo, bool) {
	r, ok := d.mediaRow(id)
	if !ok {
		return MediaInfo{}, false
	}
	return MediaInfo{
		ID: r.id, Mime: r.mime, Name: r.name, Size: media.PlainSize(r.size), State: r.state, Total: r.size,
		Uploaded: r.uploaded, Relays: len(r.urls), Important: r.important,
	}, true
}

// ---- files on disk ----

func (n *Node) mediaDir() string { return filepath.Join(n.cfg.DataDir, "media") }

func (n *Node) blobPath(id string) string { return filepath.Join(n.mediaDir(), id) }

func (n *Node) haveBlob(id string) (int64, bool) {
	st, err := os.Stat(n.blobPath(id))
	if err != nil {
		return 0, false
	}
	return st.Size(), true
}

// ---- sending ----

// SendMedia sends a file (photo, voice or video message, any file) to a
// contact or private group; caption may be empty. A normal file moves only
// over direct paths (both sides online); an important one is also uploaded
// to relays and reaches receivers who are offline, a few per day (relay
// allowance). When no relay takes it (allowance used up, no media relay) it
// goes as a normal file and the returned Media.Note says so.
func (n *Node) SendMedia(ref string, file []byte, mime, name, caption string, important bool) (Message, error) {
	c, g, err := n.resolveChat(ref)
	if err != nil {
		return Message{}, err
	}
	if g != nil && g.g != nil {
		return Message{}, errors.New("files can't be sent to public chats")
	}
	if g != nil && g.Removed {
		return Message{}, errors.New("you were removed from this group")
	}
	if len(file) == 0 {
		return Message{}, errors.New("empty file")
	}
	blob, key, id, err := media.Seal(file)
	if err != nil {
		return Message{}, err
	}
	if err := os.MkdirAll(n.mediaDir(), 0o700); err != nil {
		return Message{}, err
	}
	if err := os.WriteFile(n.blobPath(id.String()), blob, 0o600); err != nil {
		return Message{}, err
	}
	readers := 1
	if g != nil {
		readers = max(1, len(g.Members)-1)
	}
	var urls []string
	note := ""
	if important {
		if urls, note = n.mediaRelays(len(blob)); len(urls) == 0 {
			important = false
		}
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	row := mediaRow{id: id.String(), key: key, size: len(blob), mime: mime, name: filepath.Base(name), urls: urls, pending: urls, readers: readers, state: "ready", wanted: true, important: important}
	if err := n.db.putMediaRow(row); err != nil {
		return Message{}, err
	}
	r := media.Ref{ID: id, Key: key, Size: len(blob), Mime: mime, Name: row.name, URLs: urls, Caption: caption}
	content := r.Encode()
	if len(content) > message.MaxContentBytes-group.IDSize-1 {
		return Message{}, errors.New("caption too long")
	}
	now := time.Now()
	m := Message{Outgoing: true, Text: caption, Sent: now, Received: now}
	if c != nil {
		pid, err := n.router.SendContent(n.id, c.pub, e2e.ContentMedia, content, n.heardFrom(c.Hash), false)
		if err != nil {
			return Message{}, err
		}
		m.ID, m.Peer = pid.String(), c.Hash
	} else {
		m.ID, m.Peer, m.Group, m.From, m.FromName = packet.NewID().String(), g.Hash, true, n.id.Hash().String(), n.cfg.Name
		gc := group.EncodeMedia(g.id, content)
		for _, mem := range g.Members {
			if mem.You {
				continue
			}
			pid, err := n.router.SendContent(n.id, mem.pub, e2e.ContentGroup, gc, false, false)
			if err != nil {
				return Message{}, err
			}
			n.db.db.Exec(`INSERT INTO group_sends(packet,msg,member) VALUES(?,?,?)`, pid.String(), m.ID, hashBytes(mem.pub.Hash()))
			m.Recipients++
		}
	}
	mi, _ := n.db.mediaInfo(row.id)
	m.Media = &mi
	if err := n.db.putMessage(m); err != nil {
		return m, err
	}
	mi.Note = note
	n.mediaPoke()
	return m, nil
}

// mediaRelays picks up to two relays (configured first) for an important
// file of this size: they take media that big and, when they can be asked,
// still have allowance for this account today. note explains an empty list.
func (n *Node) mediaRelays(size int) (urls []string, note string) {
	media, used := 0, 0
	for _, l := range n.allRelayLinks() {
		max := l.c.KnownMediaMaxBytes()
		if max == 0 && l.isUp() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			max = l.c.MediaMaxBytes(ctx)
			cancel()
		}
		if max <= 0 || size > max+max/1000+64 {
			continue
		}
		media++
		if l.isUp() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			q, err := l.c.MediaQuota(ctx, n.id)
			cancel()
			if err == nil && q.Left <= 0 {
				used++
				continue // allowance used up (or not allowed) here
			}
		}
		urls = append(urls, l.c.URL())
		if len(urls) == 2 {
			break
		}
	}
	switch {
	case len(urls) > 0:
	case used > 0:
		note = "Today's allowance of important files is used up: sent as a normal file (it arrives when you are both online on a direct path)."
	case media == 0:
		note = "No relay takes files this big: sent as a normal file (it arrives when you are both online on a direct path)."
	}
	return urls, note
}

// ---- receiving ----

// onMedia stores a received file reference (one-to-one or private group).
func (n *Node) onMedia(m *Message, refBytes []byte) {
	r, err := media.DecodeRef(refBytes)
	if err != nil {
		n.cfg.Logf("node: media reference: %v", err)
		return
	}
	var urls []string
	for _, u := range r.URLs {
		if v, err := network.CheckURL(u, n.cfg.Discovery.AllowPrivate); err == nil {
			urls = append(urls, v)
		}
	}
	state := "remote"
	if _, ok := n.haveBlob(r.ID.String()); ok {
		state = "ready"
	}
	auto := n.cfg.Media.AutoDownloadMB
	if auto == 0 {
		auto = 2
	}
	limit := auto << 20
	if media.IsRound(r.Mime, r.Name) {
		limit = max(limit, 8<<20) // round video messages play in the chat like voice
	}
	wanted := auto > 0 && r.FileSize() <= limit
	n.db.putMediaRow(mediaRow{id: r.ID.String(), key: r.Key, size: r.Size, mime: r.Mime, name: r.Name, urls: urls, state: state, wanted: wanted, important: len(r.URLs) > 0})
	m.Text = r.Caption
	mi, _ := n.db.mediaInfo(r.ID.String())
	m.Media = &mi
	if err := n.db.putMessage(*m); err != nil {
		n.cfg.Logf("node: store message: %v", err)
	}
	n.nameGroupSender(m)
	n.emit(Event{Type: "message", Message: m})
	if wanted {
		n.mediaPoke()
	}
}

// FetchMedia asks for a file to be downloaded (it happens in the background;
// "media" events report progress).
func (n *Node) FetchMedia(id string) error {
	id = n.resolveMedia(id)
	r, ok := n.db.mediaRow(id)
	if !ok {
		return fmt.Errorf("no file %q", id)
	}
	if r.state == "ready" {
		return nil
	}
	n.db.db.Exec(`UPDATE media SET wanted=1, last_try=0, state=CASE WHEN state='missing' THEN 'remote' ELSE state END WHERE id=?`, id)
	n.emit(Event{Type: "media", Info: id})
	n.mediaPoke()
	return nil
}

// MediaFile returns a file's decrypted bytes.
func (n *Node) MediaFile(id string) ([]byte, MediaInfo, error) {
	id = n.resolveMedia(id)
	r, ok := n.db.mediaRow(id)
	if !ok {
		return nil, MediaInfo{}, fmt.Errorf("no file %q", id)
	}
	mi, _ := n.db.mediaInfo(id)
	blob, err := os.ReadFile(n.blobPath(id))
	if err != nil {
		return nil, mi, errors.New("not downloaded yet")
	}
	var bid media.ID
	if bid, err = packet.ParseHash(id); err != nil {
		return nil, mi, err
	}
	file, err := media.Open(blob, r.key, bid)
	return file, mi, err
}

// MediaInfo returns a file's state (with download progress).
func (n *Node) MediaInfo(id string) (MediaInfo, bool) {
	id = n.resolveMedia(id)
	mi, ok := n.db.mediaInfo(id)
	if ok && mi.State == "downloading" {
		if st, err := os.Stat(n.blobPath(id) + ".part"); err == nil {
			mi.Have = st.Size()
		}
	}
	return mi, ok
}

// resolveMedia accepts a unique id prefix (8+ characters).
func (n *Node) resolveMedia(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if len(ref) == 32 || len(ref) < 8 || strings.ContainsAny(ref, "%_") {
		return ref
	}
	rows, err := n.db.db.Query(`SELECT id FROM media WHERE id LIKE ? LIMIT 2`, ref+"%")
	if err != nil {
		return ref
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 1 {
		return ids[0]
	}
	return ref
}

// ---- background work: uploads and downloads ----

type mediaWorker struct {
	kick  chan struct{}
	retry atomic.Bool // a link came up: retry everything now
	mu    sync.Mutex
	busy  map[string]bool // downloads in progress
	quick map[string]int  // quick retries used per download (after a neighbour failed)

	// waiters for neighbours' answers (p2p): link|peer|id → frames
	wmu     sync.Mutex
	waiters map[string]chan []byte
	serving map[string]bool // link|peer being served
}

// RetryMedia retries waiting uploads and downloads now (a link came up).
func (n *Node) RetryMedia() { n.mediaLinkUp() }

// mediaLinkUp retries waiting transfers at once: a path just appeared.
// mediaLinkUp is called on link callbacks (Bluetooth, LAN, relays): it only
// sets a flag, the worker does the database work.
func (n *Node) mediaLinkUp() {
	n.mw.retry.Store(true)
	n.mediaPoke()
}

func (n *Node) mediaPoke() {
	select {
	case n.mw.kick <- struct{}{}:
	default:
	}
}

const (
	mediaRetry   = 2 * time.Minute
	mediaGiveUp  = 7 * 24 * time.Hour // a normal file waits this long for a direct path
	parallelGets = 2

	quickRetries    = 3
	quickRetryAfter = 10 * time.Second
)

func (n *Node) runMedia(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if n.mw.retry.Swap(false) {
			n.db.db.Exec(`UPDATE media SET last_try=0 WHERE (wanted=1 AND state='remote') OR pending<>''`)
		}
		n.mediaUploads(ctx)
		n.mediaDownloads(ctx)
		select {
		case <-ctx.Done():
			return
		case <-n.mw.kick:
		case <-t.C:
		}
	}
}

func (n *Node) mediaUploads(ctx context.Context) {
	rows, err := n.db.db.Query(`SELECT id FROM media WHERE pending<>'' AND last_try < ?`, time.Now().Add(-mediaRetry).Unix())
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		r, ok := n.db.mediaRow(id)
		if !ok {
			continue
		}
		blob, err := os.ReadFile(n.blobPath(id))
		if err != nil {
			n.db.db.Exec(`UPDATE media SET pending='' WHERE id=?`, id)
			continue
		}
		var left []string
		for _, u := range r.pending {
			l := n.relayLinkByURL(u)
			if l == nil {
				left = append(left, u)
				continue
			}
			err := l.c.PutMedia(ctx, id, blob, r.readers, n.id)
			var se *relayclient.StatusError
			switch {
			case err == nil:
				n.db.db.Exec(`UPDATE media SET uploaded=uploaded+1 WHERE id=?`, id)
			case relayclient.IsAllowanceUsed(err) || (errors.As(err, &se) && se.Code/100 == 4 && se.Code != 429):
				n.cfg.Logf("media: %s refused %s: %v", u, id, err) // too large, allowance used etc.: don't retry
			default:
				left = append(left, u)
			}
		}
		if time.Since(time.Unix(r.created, 0)) > mediaGiveUp {
			left = nil
		}
		n.db.db.Exec(`UPDATE media SET pending=?, last_try=? WHERE id=?`, strings.Join(left, "\n"), time.Now().Unix(), id)
		n.emit(Event{Type: "media", Info: id})
	}
}

func (n *Node) relayLinkByURL(u string) *relayLink {
	for _, l := range n.allRelayLinks() {
		if a, err := network.CheckURL(l.c.URL(), true); err == nil && a == u {
			return l
		}
	}
	return nil
}

func (n *Node) mediaDownloads(ctx context.Context) {
	rows, err := n.db.db.Query(`SELECT id FROM media WHERE wanted=1 AND state IN ('remote','downloading') AND last_try < ? ORDER BY created`,
		time.Now().Add(-mediaRetry).Unix())
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		n.mw.mu.Lock()
		if n.mw.busy[id] || len(n.mw.busy) >= parallelGets {
			n.mw.mu.Unlock()
			continue
		}
		n.mw.busy[id] = true
		n.mw.mu.Unlock()
		n.db.db.Exec(`UPDATE media SET last_try=? WHERE id=?`, time.Now().Unix(), id)
		go func() {
			defer func() {
				n.mw.mu.Lock()
				delete(n.mw.busy, id)
				n.mw.mu.Unlock()
				n.mediaPoke()
			}()
			n.download(ctx, id)
		}()
	}
}

// download fetches a blob from neighbours, then relays, resuming a partial file.
func (n *Node) download(ctx context.Context, id string) {
	r, ok := n.db.mediaRow(id)
	if !ok {
		return
	}
	bid, _ := packet.ParseHash(id)
	part := n.blobPath(id) + ".part"
	os.MkdirAll(n.mediaDir(), 0o700)
	n.setMediaState(id, "downloading")

	try := func(fetch func(f *os.File, from int64) error) bool {
		f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return false
		}
		from, _ := f.Seek(0, io.SeekEnd)
		if from > int64(r.size) {
			f.Truncate(0)
			from, _ = f.Seek(0, io.SeekStart)
		}
		err = fetch(f, from)
		f.Close()
		if err != nil {
			return false
		}
		blob, err := os.ReadFile(part)
		if err != nil || len(blob) != r.size {
			return false
		}
		if _, err := media.Open(blob, r.key, bid); err != nil {
			os.Remove(part) // wrong or corrupt: start over
			return false
		}
		return os.Rename(part, n.blobPath(id)) == nil
	}

	ok = false
	// Normal media comes only over direct paths: open one to the sender
	// (the attempt runs in the background; a new link retries at once).
	if len(r.urls) == 0 && n.direct != nil {
		if pub, found := n.mediaSender(id); found && !n.direct.connected(pub.Hash().String()) {
			n.direct.ensure(pub)
		}
	}
	// Neighbours first (LAN before Bluetooth): fast, no internet needed.
	nbs := n.router.Neighbours()
	for _, pass := range []bool{true, false} {
		for _, nb := range nbs {
			if ok || ctx.Err() != nil || (nb.Link == "lan") != pass {
				continue
			}
			ok = try(func(f *os.File, from int64) error { return n.fetchP2P(ctx, nb, id, r.size, f, from) })
		}
		if pass && !ok {
			// Then relays.
			for _, u := range r.urls {
				if ok || ctx.Err() != nil {
					break
				}
				ok = try(func(f *os.File, from int64) error { return n.fetchRelay(ctx, u, id, f, from) })
			}
		}
	}
	if ok {
		n.setMediaState(id, "ready")
		return
	}
	if time.Since(time.Unix(r.created, 0)) > mediaGiveUp {
		n.setMediaState(id, "missing")
		return
	}
	n.setMediaState(id, "remote") // retried later, and when a link comes up
	// A neighbour that was there but didn't answer (it may not have
	// registered us yet, or was busy): try again soon instead of after
	// mediaRetry, a few times.
	if len(nbs) > 0 {
		n.mw.mu.Lock()
		k := n.mw.quick[id]
		if k < quickRetries {
			n.mw.quick[id] = k + 1
		}
		n.mw.mu.Unlock()
		if k < quickRetries {
			time.AfterFunc(quickRetryAfter, func() {
				n.db.db.Exec(`UPDATE media SET last_try=0 WHERE id=? AND wanted=1 AND state='remote'`, id)
				n.mediaPoke()
			})
		}
	}
}

func (n *Node) setMediaState(id, state string) {
	n.db.db.Exec(`UPDATE media SET state=? WHERE id=?`, state, id)
	n.emit(Event{Type: "media", Info: id})
}

func (n *Node) fetchRelay(ctx context.Context, u, id string, f *os.File, from int64) error {
	c := (*relayclient.Client)(nil)
	if l := n.relayLinkByURL(u); l != nil {
		c = l.c // its path (Tor, proxy) and cached info
	} else {
		var err error
		if c, err = relayclient.New(u, relayclient.Options{}); err != nil {
			return err
		}
	}
	_, err := c.GetMedia(ctx, id, from, f)
	return err
}

// ---- neighbour transfers ----
//
// Frames on point-to-point links, besides packets (packet version bits 11,
// which older nodes drop as invalid packets):
//
//	REQ  0xC1 | id(16) | offset(4) | length(4)
//	DATA 0xC2 | id(16) | offset(4) | total(4) | bytes   (total 0: "don't have it")
//
// The receiver asks for one window at a time, so at most a window is ever
// queued on a link and chat packets never wait long behind a file.

const (
	frameReq  = 0xC1
	frameData = 0xC2
	reqLen    = 1 + 16 + 4 + 4
	dataHead  = 1 + 16 + 4 + 4
)

// window and frame sizes: a LAN moves megabytes per second, Bluetooth tens
// of kilobytes.
func p2pSizes(link string) (window, frame int) {
	switch link {
	case "lan":
		return 256 << 10, 32 << 10
	case "direct": // UDP datagrams: one frame per datagram
		return 64 << 10, 1100
	}
	return 8 << 10, 2 << 10
}

// Inbound takes raw bytes from a point-to-point link: media frames are
// handled here, everything else goes to the router.
func (n *Node) Inbound(link string, peer router.PeerID, raw []byte) {
	if len(raw) > 0 && raw[0]>>6 == 3 {
		n.onMediaFrame(link, peer, raw)
		return
	}
	n.router.Inbound(link, peer, raw)
}

func waitKey(link string, peer router.PeerID, id string) string {
	return link + "|" + string(peer) + "|" + id
}

func (n *Node) onMediaFrame(link string, peer router.PeerID, raw []byte) {
	switch {
	case raw[0] == frameReq && len(raw) == reqLen:
		var id packet.Hash
		copy(id[:], raw[1:])
		off := int64(binary.BigEndian.Uint32(raw[17:]))
		ln := int64(binary.BigEndian.Uint32(raw[21:]))
		n.serveP2P(link, peer, id.String(), off, ln)
	case raw[0] == frameData && len(raw) >= dataHead:
		var id packet.Hash
		copy(id[:], raw[1:])
		n.mw.wmu.Lock()
		ch := n.mw.waiters[waitKey(link, peer, id.String())]
		n.mw.wmu.Unlock()
		if ch != nil {
			select {
			case ch <- append([]byte(nil), raw...):
			default:
			}
		}
	}
}

func reqFrame(id packet.Hash, off, ln int64) []byte {
	f := make([]byte, 0, reqLen)
	f = append(f, frameReq)
	f = append(f, id[:]...)
	f = binary.BigEndian.AppendUint32(f, uint32(off))
	return binary.BigEndian.AppendUint32(f, uint32(ln))
}

func dataFrame(id packet.Hash, off, total int64, b []byte) []byte {
	f := make([]byte, 0, dataHead+len(b))
	f = append(f, frameData)
	f = append(f, id[:]...)
	f = binary.BigEndian.AppendUint32(f, uint32(off))
	f = binary.BigEndian.AppendUint32(f, uint32(total))
	return append(f, b...)
}

// serveP2P answers a window request for a blob we hold (one at a time per
// neighbour).
func (n *Node) serveP2P(link string, peer router.PeerID, id string, off, ln int64) {
	key := link + "|" + string(peer)
	n.mw.wmu.Lock()
	if n.mw.serving[key] {
		n.mw.wmu.Unlock()
		return // the asker repeats after a timeout
	}
	n.mw.serving[key] = true
	n.mw.wmu.Unlock()
	go func() {
		defer func() {
			n.mw.wmu.Lock()
			delete(n.mw.serving, key)
			n.mw.wmu.Unlock()
		}()
		bid, err := packet.ParseHash(id)
		if err != nil {
			return
		}
		window, frame := p2pSizes(link)
		ln = min(ln, int64(window))
		path := n.blobPath(id)
		if bid == avatarID(n.id.Hash().String()) {
			// Our profile picture: only to a contact over a direct link.
			path = n.myAvatarPath()
			if link != "direct" {
				path = ""
			}
		}
		f, err := os.Open(path)
		if err != nil {
			n.router.SendRaw(link, peer, dataFrame(bid, off, 0, nil))
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		total := st.Size()
		if off >= total || ln <= 0 {
			n.router.SendRaw(link, peer, dataFrame(bid, off, total, nil))
			return
		}
		buf := make([]byte, frame)
		for sent := int64(0); sent < ln && off < total; {
			k, err := f.ReadAt(buf[:min(int64(frame), ln-sent, total-off)], off)
			if k == 0 || (err != nil && err != io.EOF) {
				return
			}
			if !n.router.SendRaw(link, peer, dataFrame(bid, off, total, buf[:k])) {
				return
			}
			off += int64(k)
			sent += int64(k)
		}
	}()
}

var errNoBlob = errors.New("neighbour does not have it")

// fetchP2P pulls a blob from a neighbour window by window.
// maxStalls: how often a direct-link download re-asks for a window that
// stopped arriving before it gives up (and waits for the next retry).
const maxStalls = 5

func (n *Node) fetchP2P(ctx context.Context, nb router.Neighbour, id string, size int, f *os.File, from int64) error {
	bid, err := packet.ParseHash(id)
	if err != nil {
		return err
	}
	key := waitKey(nb.Link, nb.Peer, id)
	ch := make(chan []byte, 64)
	n.mw.wmu.Lock()
	n.mw.waiters[key] = ch
	n.mw.wmu.Unlock()
	defer func() {
		n.mw.wmu.Lock()
		delete(n.mw.waiters, key)
		n.mw.wmu.Unlock()
	}()
	window, _ := p2pSizes(nb.Link)
	off := from
	lastEvent := time.Now()
	stalls := 0 // direct links: windows re-asked in a row without progress
	for off < int64(size) {
		req := make([]byte, 0, reqLen)
		req = append(req, frameReq)
		req = append(req, bid[:]...)
		req = binary.BigEndian.AppendUint32(req, uint32(off))
		req = binary.BigEndian.AppendUint32(req, uint32(window))
		if !n.router.SendRaw(nb.Link, nb.Peer, req) {
			return errors.New("neighbour gone")
		}
		end := min(off+int64(window), int64(size))
		wait := 15 * time.Second
		if nb.Link == "direct" {
			wait = 3 * time.Second // a lost datagram: ask for the window again
		}
		timeout := time.NewTimer(wait)
	window:
		for off < end {
			select {
			case <-ctx.Done():
				timeout.Stop()
				return ctx.Err()
			case <-timeout.C:
				// Over a direct (UDP) link a datagram was lost: ask again from
				// where the data stopped. Other links are reliable: give up.
				if nb.Link == "direct" && stalls < maxStalls {
					stalls++
					break window
				}
				return errors.New("neighbour timed out")
			case fr := <-ch:
				o := int64(binary.BigEndian.Uint32(fr[17:]))
				total := int64(binary.BigEndian.Uint32(fr[21:]))
				data := fr[dataHead:]
				if total == 0 {
					timeout.Stop()
					return errNoBlob
				}
				if total != int64(size) || o != off {
					continue // stale or out of order: ignore
				}
				if len(data) == 0 {
					break window
				}
				if _, err := f.WriteAt(data, off); err != nil {
					timeout.Stop()
					return err
				}
				off += int64(len(data))
				stalls = 0
				timeout.Reset(wait)
			}
		}
		timeout.Stop()
		if time.Since(lastEvent) > time.Second {
			lastEvent = time.Now()
			n.emit(Event{Type: "media", Info: id})
		}
	}
	return nil
}

// mediaSender: the contact who sent the file (for a group file, the member).
func (n *Node) mediaSender(id string) (identity.Public, bool) {
	var peer, sender []byte
	if n.db.db.QueryRow(`SELECT peer, sender FROM messages WHERE media=? AND outgoing=0 LIMIT 1`, id).Scan(&peer, &sender) != nil {
		return identity.Public{}, false
	}
	h := peer
	if len(sender) == len(packet.Hash{}) {
		h = sender
	}
	for _, k := range n.contactKeys() {
		kh := k.Hash()
		if string(kh[:]) == string(h) {
			return k, true
		}
	}
	return identity.Public{}, false
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/node"
)

// mediaLabel describes a message's file in one line, e.g.
// "[photo dinner.jpg, 293 KB, ready · mesh media get 4a0552a2]".
func mediaLabel(m node.Message) string {
	mi := m.Media
	if mi == nil {
		return ""
	}
	kind := map[string]string{"image": "photo", "audio": "voice", "video": "video", "round": "round video"}[mi.Kind()]
	if kind == "" {
		kind = "file"
	}
	state := map[string]string{"remote": "not downloaded", "downloading": "downloading", "missing": "unavailable"}[mi.State]
	s := fmt.Sprintf("[%s %s, %s", kind, mi.Name, humanSize(mi.Size))
	if !m.Outgoing && !mi.Important && mi.State == "remote" {
		state = "waits for a direct connection"
	}
	if mi.Important {
		s += ", important"
	}
	if m.Outgoing && mi.Relays > 0 && mi.Uploaded < mi.Relays {
		state = "uploading"
	}
	if state != "" {
		s += ", " + state
	}
	return s + " · mesh media get " + mi.ID[:8] + "]"
}

// msgText is a message's text with its file, if any.
func msgText(m node.Message) string {
	l := mediaLabel(m)
	switch {
	case l == "":
		return m.Text
	case m.Text == "":
		return l
	}
	return l + " " + m.Text
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// importantFlag removes -i / --important from args and tells if it was there.
func importantFlag(args []string) ([]string, bool) {
	var out []string
	imp := false
	for _, a := range args {
		if a == "-i" || a == "--important" {
			imp = true
			continue
		}
		out = append(out, a)
	}
	return out, imp
}

// sendFile sends a file through meshd; important also sends it through
// relays (reaches someone offline, a few per day).
func sendFile(to, path, caption string, important bool) (node.Message, error) {
	var m node.Message
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	q := url.Values{"to": {to}, "name": {filepath.Base(path)}, "caption": {caption}}
	if important {
		q.Set("important", "1")
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); t != "" {
		q.Set("mime", strings.SplitN(t, ";", 2)[0])
	}
	req, _ := http.NewRequest("POST", "http://meshd/send-media?"+q.Encode(), f)
	resp, err := client.Do(req)
	if err != nil {
		return m, notRunning(err)
	}
	defer resp.Body.Close()
	return m, decodeResp(resp, &m)
}

func decodeResp(resp *http.Response, out any) error {
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// getMedia downloads a file (waiting for it) and saves it; it returns the path.
func getMedia(id, out string, progress func(node.MediaInfo)) (string, error) {
	var mi node.MediaInfo
	if err := call("GET", "/media/"+url.PathEscape(id)+"/info", nil, &mi); err != nil {
		return "", err
	}
	if mi.State != "ready" {
		if err := call("POST", "/media/"+mi.ID+"/fetch", nil, nil); err != nil {
			return "", err
		}
		deadline := time.Now().Add(30 * time.Minute)
		for mi.State != "ready" {
			if mi.State == "missing" {
				return "", errors.New("no one has this file any more (relays delete files after a few hours)")
			}
			if time.Now().After(deadline) {
				return "", errors.New("still downloading: try again later (it continues in the background)")
			}
			time.Sleep(time.Second)
			if err := call("GET", "/media/"+mi.ID+"/info", nil, &mi); err != nil {
				return "", err
			}
			if progress != nil {
				progress(mi)
			}
		}
	}
	resp, err := client.Get("http://meshd/media/" + mi.ID + "?download=1")
	if err != nil {
		return "", notRunning(err)
	}
	defer resp.Body.Close()
	if err := decodeResp(resp, nil); err != nil {
		return "", err
	}
	if out == "" {
		out = mi.Name
		if out == "" {
			out = mi.ID
		}
	}
	if st, err := os.Stat(out); err == nil && st.IsDir() {
		out = filepath.Join(out, mi.Name)
	}
	base, ext := strings.TrimSuffix(out, filepath.Ext(out)), filepath.Ext(out)
	for i := 1; ; i++ { // never overwrite
		if _, err := os.Stat(out); errors.Is(err, os.ErrNotExist) {
			break
		}
		out = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return out, err
}

func mediaCmd(args []string) error {
	const usage = "usage: mesh media get <id> [file or folder] | info <id>"
	if len(args) < 2 {
		return errors.New(usage)
	}
	switch args[0] {
	case "get", "save":
		dest := ""
		if len(args) > 2 {
			dest = args[2]
		}
		path, err := getMedia(args[1], dest, func(mi node.MediaInfo) {
			if mi.State == "downloading" && mi.Total > 0 {
				fmt.Fprintf(os.Stderr, "\r%d%%", mi.Have*100/int64(mi.Total))
			}
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "saved %s\n", path)
	case "info":
		var mi node.MediaInfo
		if err := call("GET", "/media/"+url.PathEscape(args[1])+"/info", nil, &mi); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s  %s  %s  %s  %s\n", mi.ID, mi.Name, mi.Mime, humanSize(mi.Size), mi.State)
	default:
		return errors.New(usage)
	}
	return nil
}

package node

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Folder is a user-made list of chats (Telegram-style folders). The built-in
// views (all, personal, groups, public, unread) are filtered by the apps;
// these are the user's own. Kept on this device only.
type Folder struct {
	Name  string   `json:"name"`
	Chats []string `json:"chats"` // chat hashes (contacts and groups)
}

const maxFolders = 20

// Folders returns the user's chat folders in display order.
func (n *Node) Folders() []Folder {
	out := []Folder{}
	if v, ok := n.db.get("folders"); ok {
		json.Unmarshal([]byte(v), &out)
	}
	return out
}

// SetFolders replaces the user's folders (names must be unique and non-empty).
func (n *Node) SetFolders(fs []Folder) error {
	if len(fs) > maxFolders {
		return errors.New("too many folders (max 20)")
	}
	seen := map[string]bool{}
	clean := make([]Folder, 0, len(fs))
	for _, f := range fs {
		f.Name = strings.TrimSpace(f.Name)
		if f.Name == "" || utf8.RuneCountInString(f.Name) > 24 {
			return errors.New("folder name must be 1–24 characters")
		}
		if seen[strings.ToLower(f.Name)] {
			return errors.New("two folders named " + f.Name)
		}
		seen[strings.ToLower(f.Name)] = true
		chats := []string{}
		in := map[string]bool{}
		for _, h := range f.Chats {
			if !in[h] && len(h) == 32 {
				in[h] = true
				chats = append(chats, h)
			}
		}
		f.Chats = chats
		clean = append(clean, f)
	}
	b, _ := json.Marshal(clean)
	if err := n.db.set("folders", string(b)); err != nil {
		return err
	}
	n.emit(Event{Type: "folders"})
	return nil
}

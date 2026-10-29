package bot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/fsutil"
	"xbot/internal/link"
)

// appsFile is the owner-level app list under the root. Every bot reads it, so
// one install reaches all of them. It holds link names only, never secrets.
type appsFile struct {
	Links []string `json:"links"`
}

// AppsPath is the owner-level list of installed native links.
func AppsPath(root string) string { return filepath.Join(root, "apps.json") }

// GlobalMCPPath is the owner-level MCP server list shared by all bots.
func GlobalMCPPath(root string) string { return filepath.Join(root, "mcp.json") }

// GlobalLinks reads the owner-level link names. A missing file is empty.
func GlobalLinks(root string) ([]string, error) {
	raw, err := os.ReadFile(AppsPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f appsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return f.Links, nil
}

// GrantGlobal adds a link for every bot. Repeating it changes nothing.
func GrantGlobal(root, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("bot: empty link")
	}
	if _, organ := link.LookupOrgan(name); organ {
		return nil
	}
	return fsutil.WithLock(AppsPath(root)+".lock", func() error {
		cur, err := GlobalLinks(root)
		if err != nil {
			return err
		}
		next := addUnique(cur, name)
		if len(next) == len(cur) {
			return nil
		}
		return writeApps(root, next)
	})
}

// RevokeGlobal removes a link from every bot. A name that is not there is left alone.
func RevokeGlobal(root, name string) error {
	name = strings.TrimSpace(name)
	return fsutil.WithLock(AppsPath(root)+".lock", func() error {
		cur, err := GlobalLinks(root)
		if err != nil {
			return err
		}
		next := make([]string, 0, len(cur))
		for _, n := range cur {
			if n != name {
				next = append(next, n)
			}
		}
		if len(next) == len(cur) {
			return nil
		}
		return writeApps(root, next)
	})
}

func writeApps(root string, links []string) error {
	data, err := fsutil.Marshal(appsFile{Links: links})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(AppsPath(root), data, 0o600)
}

// EffectiveLinks is this bot's own grants plus the owner-level ones.
// A damaged owner list counts as empty so a bot still starts.
func (b *Bot) EffectiveLinks() []string {
	var out []string
	global, _ := GlobalLinks(b.Root)
	for _, n := range append(append([]string(nil), b.cfg.Links...), global...) {
		if _, organ := link.LookupOrgan(n); !organ {
			out = addUnique(out, n)
		}
	}
	return out
}

// GlobalMCPPath is the shared MCP list for this bot's root.
func (b *Bot) GlobalMCPPath() string { return GlobalMCPPath(b.Root) }

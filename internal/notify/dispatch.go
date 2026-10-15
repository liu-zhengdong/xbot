package notify

import (
	"path/filepath"
	"strings"
	"sync"
)

var (
	hookMu sync.Mutex
	hooks  = map[string]func(botName string, n Note){}
)

// Bind registers fn for notes appended under root. A nil fn removes it.
// The web server uses this to send a phone push after a note is written.
func Bind(root string, fn func(botName string, n Note)) {
	root = filepath.Clean(root)
	hookMu.Lock()
	defer hookMu.Unlock()
	if fn == nil {
		delete(hooks, root)
		return
	}
	hooks[root] = fn
}

func emitNote(path string, n Note) {
	hookMu.Lock()
	type call struct {
		fn  func(string, Note)
		bot string
	}
	var calls []call
	for root, fn := range hooks {
		bot, ok := botFromOutbox(root, path)
		if ok && fn != nil {
			calls = append(calls, call{fn: fn, bot: bot})
		}
	}
	hookMu.Unlock()
	for _, c := range calls {
		c.fn(c.bot, n)
	}
}

func botFromOutbox(root, path string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[0] != "bots" || parts[2] != "outbox.jsonl" {
		return "", false
	}
	if parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", false
	}
	return parts[1], true
}

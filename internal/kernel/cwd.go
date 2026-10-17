package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

// cwdState is the bot's persistent working directory. It lives in memory and
// in bots/<name>/cwd so a new process for the same bot continues where the
// last foreground command stopped.
type cwdState struct {
	mu     sync.Mutex
	loaded bool
	dir    string
}

func (k *Kernel) cwdPath() string {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return ""
	}
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "cwd")
}

func (k *Kernel) loadCwd() string {
	if k == nil {
		return ""
	}
	k.cwd.mu.Lock()
	defer k.cwd.mu.Unlock()
	if k.cwd.loaded {
		return k.cwd.dir
	}
	k.cwd.loaded = true
	path := k.cwdPath()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	k.cwd.dir = strings.TrimSpace(string(b))
	return k.cwd.dir
}

func (k *Kernel) storeCwd(dir string) {
	if k == nil {
		return
	}
	k.cwd.mu.Lock()
	defer k.cwd.mu.Unlock()
	k.cwd.loaded = true
	if k.cwd.dir == dir {
		return
	}
	k.cwd.dir = dir
	path := k.cwdPath()
	if path == "" || dir == "" {
		return
	}
	_ = fsutil.WriteAtomic(path, []byte(dir+"\n"), 0600)
}

// allowedRoots are the workspace and the shared directory. A shell cwd and a
// project-instruction file must stay inside one of them.
func (k *Kernel) allowedRoots() []string {
	var in []string
	if k != nil {
		if k.WorkDir != "" {
			in = append(in, k.WorkDir)
		}
		if k.SharedDir != "" {
			in = append(in, k.SharedDir)
		}
	}
	return cleanRoots(in)
}

// workspace is the directory a call falls back to. It is the bot workspace
// when one is set, otherwise the process working directory.
func (k *Kernel) workspace() string {
	if k != nil && strings.TrimSpace(k.WorkDir) != "" {
		if dir, ok := cleanDir(k.WorkDir); ok {
			return dir
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if dir, ok := cleanDir(wd); ok {
		return dir
	}
	return filepath.Clean(wd)
}

func cleanDir(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), true
}

func cleanRoots(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range in {
		abs, ok := cleanDir(r)
		if !ok || seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

func insideRoots(path string, roots []string) bool {
	abs, ok := cleanDir(path)
	if !ok || len(roots) == 0 {
		return false
	}
	for _, root := range roots {
		if abs == root {
			return true
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// usableDir reports path when it exists, is a directory, and sits inside roots.
func usableDir(path string, roots []string) (string, bool) {
	abs, ok := cleanDir(path)
	if !ok || !insideRoots(abs, roots) {
		return "", false
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return abs, true
}

// lookupCwd reports where a shell call would start. It does not store the
// fallback: project-instruction loading and the review path also ask, and
// storing here would swallow the notice the shell result is supposed to show.
func (k *Kernel) lookupCwd() (string, string) {
	roots := k.allowedRoots()
	fb := k.workspace()
	if len(roots) == 0 && fb != "" {
		roots = []string{fb}
	}
	want := k.loadCwd()
	if want == "" {
		want = fb
	}
	if dir, ok := usableDir(want, roots); ok {
		return dir, ""
	}
	if dir, ok := usableDir(fb, roots); ok {
		if want != "" && want != dir {
			return dir, fmt.Sprintf("working directory %s is unavailable; using %s", want, dir)
		}
		return dir, ""
	}
	if want != "" {
		return "", fmt.Sprintf("working directory %s is unavailable", want)
	}
	return "", ""
}

// shellStartDir is the directory for this shell call. A saved directory that
// is missing or outside the allowed roots falls back to the workspace.
func (k *Kernel) shellStartDir() (string, string) {
	dir, notice := k.lookupCwd()
	if notice != "" && dir != "" {
		k.storeCwd(dir)
	}
	return dir, notice
}

// shellAfterDir accepts the directory a foreground command finished in.
// A path outside the allowed roots is not stored; the workspace stays current.
func (k *Kernel) shellAfterDir(dir string) (string, string) {
	roots := k.allowedRoots()
	fb := k.workspace()
	if len(roots) == 0 && fb != "" {
		roots = []string{fb}
	}
	if kept, ok := usableDir(dir, roots); ok {
		k.storeCwd(kept)
		return kept, ""
	}
	if kept, ok := usableDir(fb, roots); ok {
		k.storeCwd(kept)
		return kept, fmt.Sprintf("working directory %s is outside the workspace; staying in %s", dir, kept)
	}
	return "", fmt.Sprintf("working directory %s is outside the workspace", dir)
}

// shellCwd is the directory a shell command will actually start in.
func (k *Kernel) shellCwd() string {
	dir, _ := k.lookupCwd()
	if dir != "" {
		return dir
	}
	if k != nil {
		return k.WorkDir
	}
	return ""
}

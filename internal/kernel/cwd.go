package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

// cwdState is the working directory of each execution context. The foreground
// value is the only one written to bots/<name>/cwd, so a new process continues
// where the last foreground command stopped. Background branches and subagents
// keep their own directory in extra; those are not persisted.
type cwdState struct {
	mu     sync.Mutex
	loaded bool
	dir    string
	extra  map[string]string
}

// cwdScopeKey pins a context to a cwd that is not the foreground and not a
// branch id. Subagent tools close over their scope instead of reading this,
// because a subagent call does not carry the parent's turn.
type cwdScopeKey struct{}

// shellMarkerDir is where a foreground command records the directory it
// finished in. It lives under the bot data directory, not the process temp dir.
func (k *Kernel) shellMarkerDir() string {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return ""
	}
	dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "cwd-markers")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}
	return dir
}

func (k *Kernel) cwdPath() string {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return ""
	}
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "cwd")
}

// scopeOf is the cwd key for ctx. An explicit scope wins, then a background
// branch id, then "" for the foreground. It must run before cwd.mu is taken:
// BranchOf locks the turn, and a fork holds that lock while it snapshots cwd.
func scopeOf(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if s, ok := ctx.Value(cwdScopeKey{}).(string); ok && s != "" {
		return s
	}
	return BranchOf(ctx)
}

func (k *Kernel) ensureLoadedLocked() {
	if k == nil || k.cwd.loaded {
		return
	}
	k.cwd.loaded = true
	path := k.cwdPath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	k.cwd.dir = strings.TrimSpace(string(b))
}

func (k *Kernel) readScope(scope string) string {
	if k == nil {
		return ""
	}
	k.cwd.mu.Lock()
	defer k.cwd.mu.Unlock()
	k.ensureLoadedLocked()
	if scope == "" {
		return k.cwd.dir
	}
	if k.cwd.extra == nil {
		return ""
	}
	return k.cwd.extra[scope]
}

// noteScope records dir for scope when that scope has no directory yet.
// The foreground is not stored here; only storeScope writes bots/<name>/cwd.
func (k *Kernel) noteScope(scope, dir string) {
	if k == nil || scope == "" || dir == "" {
		return
	}
	k.cwd.mu.Lock()
	defer k.cwd.mu.Unlock()
	k.ensureLoadedLocked()
	if k.cwd.extra == nil {
		k.cwd.extra = map[string]string{}
	}
	if _, ok := k.cwd.extra[scope]; ok {
		return
	}
	k.cwd.extra[scope] = dir
}

// adoptScope copies the cwd of ctx into scope, once. A later cd in the parent
// does not move the child, and a later cd in the child does not move the parent.
func (k *Kernel) adoptScope(ctx context.Context, scope string) {
	if k == nil || scope == "" {
		return
	}
	parent := scopeOf(ctx)
	if parent == scope {
		return
	}
	dir, _ := k.lookupScope(parent)
	if dir == "" {
		dir = k.workspace()
	}
	k.noteScope(scope, dir)
}

// adoptForeground copies the foreground cwd into scope. tryFork calls it while
// holding the turn lock, so it must not call BranchOf.
func (k *Kernel) adoptForeground(scope string) {
	k.adoptScope(context.Background(), scope)
}

func (k *Kernel) storeCwd(dir string) {
	k.storeScope("", dir)
}

func (k *Kernel) storeScope(scope, dir string) {
	if k == nil {
		return
	}
	k.cwd.mu.Lock()
	defer k.cwd.mu.Unlock()
	k.ensureLoadedLocked()
	if scope != "" {
		if k.cwd.extra == nil {
			k.cwd.extra = map[string]string{}
		}
		k.cwd.extra[scope] = dir
		return
	}
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

// lookupScope reports where a call in scope would start. It does not store a
// fallback: project loading and the review path also ask, and storing here
// would swallow the notice the shell result is supposed to show. The first
// lookup of an unknown scope copies the foreground directory into that scope.
func (k *Kernel) lookupScope(scope string) (string, string) {
	roots := k.allowedRoots()
	fb := k.workspace()
	if len(roots) == 0 && fb != "" {
		roots = []string{fb}
	}
	want := k.readScope(scope)
	if want == "" && scope != "" {
		want = k.readScope("")
		if want == "" {
			want = fb
		}
		if dir, ok := usableDir(want, roots); ok {
			k.noteScope(scope, dir)
			return dir, ""
		}
	}
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

func (k *Kernel) shellStartDir(ctx context.Context) (string, string) {
	return k.shellStartScope(scopeOf(ctx))
}

func (k *Kernel) shellStartScope(scope string) (string, string) {
	dir, notice := k.lookupScope(scope)
	if notice != "" && dir != "" {
		k.storeScope(scope, dir)
	}
	return dir, notice
}

func (k *Kernel) shellAfterDir(ctx context.Context, dir string) (string, string) {
	return k.shellAfterScope(scopeOf(ctx), dir)
}

// shellAfterScope accepts the directory a command in scope finished in.
// A path outside the allowed roots is not stored; the scope stays in the workspace.
// Only the foreground scope is written to bots/<name>/cwd.
func (k *Kernel) shellAfterScope(scope, dir string) (string, string) {
	roots := k.allowedRoots()
	fb := k.workspace()
	if len(roots) == 0 && fb != "" {
		roots = []string{fb}
	}
	if kept, ok := usableDir(dir, roots); ok {
		k.storeScope(scope, kept)
		return kept, ""
	}
	if kept, ok := usableDir(fb, roots); ok {
		k.storeScope(scope, kept)
		return kept, fmt.Sprintf("working directory %s is outside the workspace; staying in %s", dir, kept)
	}
	return "", fmt.Sprintf("working directory %s is outside the workspace", dir)
}

// shellCwd is the foreground directory a shell command starts in when the
// caller has no branch and no subagent scope.
func (k *Kernel) shellCwd() string {
	return k.cwdFor(context.Background())
}

// cwdFor is the directory of the calling context: the foreground, the
// background branch in ctx, or an explicit scope.
func (k *Kernel) cwdFor(ctx context.Context) string {
	return k.cwdScope(scopeOf(ctx))
}

func (k *Kernel) cwdScope(scope string) string {
	dir, _ := k.lookupScope(scope)
	if dir != "" {
		return dir
	}
	if k != nil {
		return k.WorkDir
	}
	return ""
}

package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/memory"
	"xbot/internal/tools"
)

// memoryFolder classifies an absolute path without resolving its symlinks.
func (k *Kernel) memoryFolder(path string) (dir string, scope memory.Scope, own bool) {
	if k == nil || k.Bot == nil || path == "" {
		return
	}
	root, err := filepath.Abs(k.Bot.Root)
	if err != nil {
		return
	}
	resolved, _ := tools.ResolveRoot("", root)
	var parts []string
	for _, candidate := range []string{root, resolved} {
		rel, err := filepath.Rel(candidate, path)
		if err != nil {
			continue
		}
		parts = strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 3 && parts[2] == "memory" && memory.ValidID(parts[1]) && (parts[0] == "bots" || parts[0] == "people") {
			root = candidate
			break
		}
		parts = nil
	}
	if parts == nil {
		return
	}
	switch parts[0] {
	case "people":
		scope, own = memory.ScopePerson, true
	case "bots":
		scope, own = memory.ScopeBot, parts[1] == k.Bot.Name()
	default:
		return
	}
	return filepath.Join(root, parts[0], parts[1], "memory"), scope, own
}

// memoryCall uses both identities so symlinks cannot erase memory protection.
func (k *Kernel) memoryCall(ctx context.Context, tool tools.Tool, args json.RawMessage) (path, dir string, scope memory.Scope, own bool) {
	if logical, ok := tool.(interface {
		LogicalPathContext(context.Context, json.RawMessage) string
	}); ok {
		path = logical.LogicalPathContext(ctx, args)
		dir, scope, own = k.memoryFolder(path)
		if dir != "" {
			return
		}
	}
	path = callPath(ctx, tool, args)
	dir, scope, own = k.memoryFolder(path)
	return
}

func memorySymlinks(dir, path string) error {
	for cur := path; ; cur = filepath.Dir(cur) {
		st, err := os.Lstat(cur)
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("memory: symlinks are not allowed in memory folders")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if cur == dir {
			if path == dir {
				return errors.New("memory: cannot write the memory folder itself")
			}
			return nil
		}
	}
}

// runMemoryGuard surrounds the actual tool execution, after review and queuing.
func (t reviewedTool) runMemoryGuard(ctx context.Context, args json.RawMessage) (out string, err error) {
	path, dir, scope, own := t.k.memoryCall(ctx, t.Tool, args)
	if !isFileEdit(t.Name()) || dir == "" {
		return t.Tool.Run(ctx, args)
	}
	if err := memorySymlinks(dir, path); err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(dir, path)
	tracked := rel == "memory.md" || rel == "log" || strings.HasPrefix(rel, "log"+string(filepath.Separator))
	if own && tracked {
		done := t.k.beginOwnWrite(ctx, scope, filepath.Base(filepath.Dir(dir)))
		// Other processes can still write between these snapshots and the folder lock.
		defer func() { done(err == nil) }()
	}
	err = fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		if err := memorySymlinks(dir, path); err != nil {
			return err
		}
		if kernelManaged(rel) {
			return errors.New("memory: kernel-managed file")
		}
		if !tracked {
			var err error
			out, err = t.Tool.Run(ctx, args)
			return err
		}
		old, err := os.ReadFile(path)
		existed := !errors.Is(err, os.ErrNotExist)
		if err != nil && existed {
			return err
		}
		mode := os.FileMode(0600)
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		var runErr error
		out, runErr = t.Tool.Run(ctx, args)
		next, readErr := os.ReadFile(path)
		ruleErr := readErr
		if readErr == nil {
			ruleErr = validateMemoryWrite(rel, old, next, t.k.now())
		}
		if ruleErr != nil {
			var restoreErr error
			if existed {
				restoreErr = fsutil.WriteAtomic(path, old, mode)
			} else {
				restoreErr = os.Remove(path)
				if errors.Is(restoreErr, os.ErrNotExist) {
					restoreErr = nil
				}
			}
			out = ""
			if restoreErr != nil {
				restoreErr = fmt.Errorf("memory: restore failed; %s may hold the rejected content: %w", modelPath(t.Tool, path), restoreErr)
			}
			return errors.Join(ruleErr, restoreErr, runErr)
		}
		return runErr
	})
	return out, err
}

// kernelManaged reports a file at the folder root that only the store writes:
// the lock, indexes, the person sync journal, and the legacy log.md,
// log.overflow.md and profile.md names, whose presence makes every load wait
// for a migration that runs once per process.
func kernelManaged(rel string) bool {
	low := strings.ToLower(rel)
	switch low {
	case ".lock", "sync.jsonl", "log.md", "log.overflow.md", "profile.md":
		return true
	}
	return !strings.ContainsRune(low, filepath.Separator) && strings.HasSuffix(low, ".index.json")
}

func validateMemoryWrite(rel string, old, next []byte, now time.Time) error {
	if rel == "memory.md" && len(next) > memory.ProfileBytes && len(next) >= len(old) {
		return fmt.Errorf("memory: memory.md exceeds %d bytes; consolidate: merge entries, delete stale ones, move details to the log or a free file", memory.ProfileBytes)
	}
	if rel == "memory.md" {
		return memoryEntryLines(old, next)
	}
	if rel != "log" && !strings.HasPrefix(rel, "log"+string(filepath.Separator)) {
		return nil
	}
	if rel != filepath.Join("log", now.In(time.Local).Format("2006-01")+".md") {
		return errors.New("memory: only the current month's log may change")
	}
	if len(next) > 64<<10 {
		return errors.New("memory: monthly log exceeds 64 KiB")
	}
	if !bytes.HasPrefix(next, old) {
		return errors.New("memory: log is append only; append corrections as a new line")
	}
	if len(next) > len(old) && len(old) > 0 && old[len(old)-1] != '\n' && next[len(old)] != '\n' {
		return errors.New("memory: append must start on a new log line")
	}
	for _, line := range strings.Split(string(next[len(old):]), "\n") {
		if strings.TrimSuffix(line, "\r") != "" && !memory.ValidLogLine(line) {
			return errors.New("memory: expected - YYYY-MM-DD HH:MM [source] text")
		}
	}
	return nil
}

// memoryEntryLines rejects new memory.md lines the store would drop: outside
// the 索引 section, only blank lines, its own three headings and
// - (YYYY-MM-DD) text entries survive its next rewrite. Lines already in the
// file are left alone.
func memoryEntryLines(old, next []byte) error {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(old), "\n") {
		seen[strings.TrimSuffix(line, "\r")] = true
	}
	inIndex, indexes := false, 0
	for _, line := range strings.Split(string(next), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "## ") {
			inIndex = line == "## 索引"
			if inIndex {
				indexes++
			}
		}
		if indexes > 1 {
			return errors.New("memory: memory.md has one ## 索引 section")
		}
		// The store writes back only these headings and the first index section.
		heading := line == "# memory" || line == "## 索引" || line == "## 条目"
		if inIndex || heading || seen[line] || strings.TrimSpace(line) == "" || memory.ValidMemoryLine(line) {
			continue
		}
		if r := []rune(line); len(r) > 60 {
			line = string(r[:60]) + "…"
		}
		return fmt.Errorf("memory: memory.md entries are one line each: - (YYYY-MM-DD) text; got %q", line)
	}
	return nil
}

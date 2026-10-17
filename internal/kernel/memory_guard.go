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

// memoryFolder matches canonical targets, including aliases into the data root.
func (k *Kernel) memoryFolder(path string) (dir string, scope memory.Scope, own bool) {
	if k == nil || k.Bot == nil || path == "" {
		return
	}
	root, err := tools.ResolveRoot("", k.Bot.Root)
	if err != nil {
		return
	}
	path, err = tools.ResolveRoot("", path)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 || parts[2] != "memory" || !memory.ValidID(parts[1]) {
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

// runMemoryGuard surrounds the actual tool execution, after review and queuing.
func (t reviewedTool) runMemoryGuard(ctx context.Context, args json.RawMessage) (string, error) {
	path := callPath(ctx, t.Tool, args)
	dir, scope, own := t.k.memoryFolder(path)
	if !isFileEdit(t.Name()) || dir == "" {
		return t.Tool.Run(ctx, args)
	}
	if own {
		done := t.k.beginOwnWrite(ctx, scope, filepath.Base(filepath.Dir(dir)))
		defer done() // Snapshot reads take the folder lock too; finish after releasing it.
	}
	var out string
	err := fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		rel, _ := filepath.Rel(dir, path)
		if rel == ".lock" || rel == "memory.index.json" {
			return errors.New("memory: kernel-managed file")
		}
		if rel != "memory.md" && rel != "log" && !strings.HasPrefix(rel, "log"+string(filepath.Separator)) {
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
			return errors.Join(ruleErr, restoreErr, runErr)
		}
		return runErr
	})
	return out, err
}

func validateMemoryWrite(rel string, old, next []byte, now time.Time) error {
	if rel == "memory.md" && len(next) > memory.ProfileBytes && len(next) >= len(old) {
		return fmt.Errorf("memory: memory.md exceeds %d bytes; consolidate: merge entries, delete stale ones, move details to the log or a free file", memory.ProfileBytes)
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

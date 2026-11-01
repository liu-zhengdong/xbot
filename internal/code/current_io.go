package code

import (
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/work"
)

// UseCurrentIO binds this real Link to the process-owned Bot route. Bind before
// exposing tools. Record I/O, cards and terminal posts resolve inside the short
// callback; backends and terminal observers run outside it.
func (l *Link) UseCurrentIO(fn func(func(*bot.Bot, *inbox.Inbox) error) error) {
	if l == nil {
		return
	}
	l.pathMu.Lock()
	defer l.pathMu.Unlock()
	l.rec.mu.Lock()
	defer l.rec.mu.Unlock()
	l.currentIO = fn
	if fn == nil {
		l.rec.withDirectoryIO = nil
		return
	}
	l.rec.withDirectoryIO = func(io func(string) error) error {
		return fn(func(b *bot.Bot, _ *inbox.Inbox) error { return io(filepath.Join(bot.Dir(b.Root, b.Name()), "code")) })
	}
}

// Record.mu is held. A resolved directory never escapes to business I/O.
func (r *Record) directoryIO(fn func() error) error {
	if r.withDirectoryIO == nil {
		return fn()
	}
	return r.withDirectoryIO(func(dir string) error { r.Dir = dir; return fn() })
}

// A blocking flock cannot hold route.RLock: Move owns the same flock before
// taking route.Lock. Open/try under route, wait on the open inode without route,
// then resolve again. This keeps the original file lock and never creates a
// stale path while waiting for another Link/process to finish.
func (r *Record) lockFile() (*os.File, error) {
	for {
		var f *os.File
		busy := false
		err := r.directoryIO(func() error {
			if err := os.MkdirAll(r.dir(), 0700); err != nil {
				return err
			}
			var err error
			f, err = os.OpenFile(filepath.Join(r.dir(), ".lock"), os.O_RDWR|os.O_CREATE, 0600)
			if err != nil {
				return err
			}
			err = fsutil.LockFile(f, true, false)
			if fsutil.IsWouldBlock(err) {
				busy = true
				return nil
			}
			return err
		})
		if err != nil {
			if f != nil {
				_ = f.Close()
			}
			return nil, err
		}
		if !busy {
			return f, nil
		}
		err = fsutil.LockFile(f, true, true)
		if err == nil {
			err = fsutil.UnlockFile(f)
		}
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
}

// Only this Bot's own record/workspace prefixes follow a rename. External
// projects, repositories and caller text keep their original identity.
func rebaseCodePath(path, oldCode, newCode string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	oldBot, newBot := filepath.Dir(oldCode), filepath.Dir(newCode)
	oldWork := work.BotDir(filepath.Dir(filepath.Dir(oldBot)), filepath.Base(oldBot))
	newWork := work.BotDir(filepath.Dir(filepath.Dir(newBot)), filepath.Base(newBot))
	for _, pair := range [][2]string{{oldBot, newBot}, {oldWork, newWork}} {
		rel, err := filepath.Rel(pair[0], path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(pair[1], rel)
		}
	}
	return path
}

// fn contains only local file I/O or process Start; backends/Wait run outside.
func (l *Link) taskIO(id string, fn func(Task) error) error {
	return l.rec.lock(func() error {
		task, err := l.rec.read(id)
		if err != nil {
			return err
		}
		return fn(task)
	})
}

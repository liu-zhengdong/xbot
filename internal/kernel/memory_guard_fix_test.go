package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/tools"
)

func memoryGuardFixture(t *testing.T) (*Kernel, string) {
	t.Helper()
	b := testBot(t, link.NameTerminal)
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return &Kernel{Bot: b, Memory: memory.Open(b.Root)}, dir
}

func TestMemorySymlinkRejected(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	outside := filepath.Join(k.Bot.Root, "outside")
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	tool := &tools.WriteFile{Root: filepath.Dir(dir)}
	reject := func(path, reason string) {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"path": path, "content": "bad"})
		need, _, err := k.reviewCall(context.Background(), tool, args, "")
		if err != nil || !need {
			t.Fatalf("card for %s: %v %v", path, need, err)
		}
		_, err = (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	reject("memory/notes.txt", "symlinks are not allowed")
	if _, err := os.Stat(filepath.Join(outside, "notes.txt")); !os.IsNotExist(err) {
		t.Fatal("outside target touched")
	}
	// The same policy applies to a protected file symlink within a real folder.
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "profile.txt"), filepath.Join(dir, "memory.md")); err != nil {
		t.Fatal(err)
	}
	reject(filepath.Join(dir, "memory.md"), "symlinks are not allowed")
	dir = filepath.Join(filepath.Dir(filepath.Dir(dir)), "u2", "memory")
	reject(dir, "folder itself")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("folder target created")
	}
}

type memoryWriteHook struct {
	*tools.WriteFile
	run func() (string, error)
}

func (t memoryWriteHook) Run(context.Context, json.RawMessage) (string, error) { return t.run() }

func TestMemoryFreeWriteDoesNotRecordOutsideChange(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "notes.txt"), "content": "note"})
	tool := memoryWriteHook{WriteFile: &tools.WriteFile{}, run: func() (string, error) {
		return "", os.WriteFile(filepath.Join(dir, "memory.md"), []byte("- (2026-10-18) outside change\n"), 0600)
	}}
	if _, err := (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(k.memOwn) != 0 {
		t.Fatal("free write swallowed an outside memory change")
	}
}

func TestMemoryRejectedWriteRestore(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	path := filepath.Join(dir, "memory.md")
	log := filepath.Join(dir, "log", time.Now().Format("2006-01")+".md")
	if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": log, "content": "bad"})
	failRestore := false
	tool := memoryWriteHook{WriteFile: &tools.WriteFile{}, run: func() (string, error) {
		if err := os.WriteFile(path, []byte("- (2026-10-18) outside change\n"), 0600); err != nil {
			return "", err
		}
		if failRestore {
			if err := os.Mkdir(log, 0700); err != nil {
				return "", err
			}
			return "", os.WriteFile(filepath.Join(log, "rejected"), []byte("bad"), 0600)
		}
		return "", os.WriteFile(log, []byte("bad"), 0600)
	}}
	for _, fail := range []bool{false, true} {
		failRestore = fail
		_, err := (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
		if err == nil {
			t.Fatal("bad log accepted")
		}
		if fail {
			if !strings.Contains(err.Error(), "may hold the rejected content") || !strings.Contains(err.Error(), log) {
				t.Fatalf("missing restore warning: %v", err)
			}
		} else if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatal("rejected log not removed")
		}
		if len(k.memOwn) != 0 {
			t.Fatal("rejected write swallowed an outside memory change")
		}
	}
}

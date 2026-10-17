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

func TestMemorySymlinkRejected(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	dir := filepath.Join(b.Root, "people", "u1", "memory")
	outside := filepath.Join(b.Root, "outside")
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	tool := &tools.WriteFile{Root: filepath.Dir(dir)}
	args, _ := json.Marshal(map[string]string{"path": "memory/notes.txt", "content": "bad"})
	need, _, err := k.reviewCall(context.Background(), tool, args, "")
	if err != nil || !need {
		t.Fatalf("card: %v %v", need, err)
	}
	_, err = (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("write: %v", err)
	}
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
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(dir, "memory.md"), "content": "bad"})
	_, err = (reviewedTool{Tool: &tools.WriteFile{}, k: k}).Run(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("file link: %v", err)
	}
}

type memoryWriteHook struct {
	*tools.WriteFile
	run func() (string, error)
}

func (t memoryWriteHook) Run(context.Context, json.RawMessage) (string, error) { return t.run() }

func TestMemoryFreeWriteDoesNotRecordOutsideChange(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
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

	// Rejected protected writes must not swallow a simultaneous outside change either.
	log := filepath.Join(dir, "log", time.Now().Format("2006-01")+".md")
	if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
		t.Fatal(err)
	}
	args, _ = json.Marshal(map[string]string{"path": log, "content": "bad"})
	tool.run = func() (string, error) {
		if err := os.WriteFile(filepath.Join(dir, "memory.md"), []byte("- (2026-10-18) another outside change\n"), 0600); err != nil {
			return "", err
		}
		return "", os.WriteFile(log, []byte("bad"), 0600)
	}
	if _, err := (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args); err == nil {
		t.Fatal("bad log accepted")
	}
	if len(k.memOwn) != 0 {
		t.Fatal("rejected write swallowed an outside memory change")
	}
}

func TestMemoryRestoreFailureWarning(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "content": "bad"})
	tool := memoryWriteHook{WriteFile: &tools.WriteFile{}, run: func() (string, error) {
		if err := os.Mkdir(path, 0700); err != nil {
			return "", err
		}
		return "", os.WriteFile(filepath.Join(path, "rejected"), []byte("bad"), 0600)
	}}
	_, err := (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "may hold the rejected content") || !strings.Contains(err.Error(), path) {
		t.Fatalf("missing restore warning: %v", err)
	}
	if len(k.memOwn) != 0 {
		t.Fatal("failed restoration recorded as own change")
	}
}

func TestMemoryFolderTargetRejected(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	path := filepath.Join(b.Root, "people", "u1", "memory")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "bad"})
	tool := &tools.WriteFile{}
	need, _, err := k.reviewCall(context.Background(), tool, args, "")
	if err != nil || !need {
		t.Fatalf("folder card: %v %v", need, err)
	}
	_, err = (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "folder itself") {
		t.Fatalf("folder write: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("folder target created")
	}
}

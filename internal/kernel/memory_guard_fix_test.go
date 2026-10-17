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

// A file tool writing a legacy log.md used to be accepted as a free file, after
// which every load failed with "legacy log awaits migration" until restart.
func TestMemoryKernelManagedNamesRejected(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	person := filepath.Join(k.Bot.Root, "people", "owner", "memory")
	for _, path := range []string{
		filepath.Join(dir, "log.md"), filepath.Join(dir, "log.overflow.md"), filepath.Join(dir, "Profile.md"),
		filepath.Join(dir, "log.index.json"), filepath.Join(person, "sync.jsonl"), filepath.Join(person, "profile.md"),
	} {
		if err := guardWrite(t, k, path, "notes\n"); err == nil || !strings.Contains(err.Error(), "kernel-managed") {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was written", path)
		}
	}
	if _, err := k.Memory.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: k.Bot.Name(), Tier: memory.TierLog}); err != nil {
		t.Fatalf("load after rejected writes: %v", err)
	}
	if err := guardWrite(t, k, filepath.Join(dir, "notes", "log.md"), "free\n"); err != nil {
		t.Fatalf("free file in a subfolder: %v", err)
	}
}

// An undated memory.md line was accepted, never reached the snapshot, and was
// deleted by the store's next rewrite of the file.
func TestMemoryMDRejectsLinesTheStoreDrops(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	path := filepath.Join(dir, "memory.md")
	if err := guardWrite(t, k, path, "# memory\n\n- always run the tests\n"); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("undated entry: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected file left behind")
	}
	if err := guardWrite(t, k, path, "# memory\n\n## 偏好：主人喜欢茶\n"); err == nil {
		t.Fatal("a heading the store drops was accepted")
	}
	ok := "# memory\n\n## 索引\nnotes/ - free notes\n\n## 条目\n- (2026-10-18) always run the tests\n"
	if err := guardWrite(t, k, path, ok); err != nil {
		t.Fatal(err)
	}
	facts, err := k.Memory.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: k.Bot.Name(), Tier: memory.TierProfile})
	if err != nil || len(facts) != 1 {
		t.Fatalf("recall %v %v", facts, err)
	}
}

// memorySymlinks used to spin forever when the path never reached dir, as on
// Windows when the path's case differs from the data root.
func TestMemorySymlinksStopsAtVolumeRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	other := filepath.Join(t.TempDir(), "Memory", "memory.md")
	done := make(chan error, 1)
	go func() { done <- memorySymlinks(dir, other) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a path outside dir passed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("memorySymlinks did not return")
	}
}

// Subagent file tools had no review wrapper, so they skipped the memory guard.
func TestSubagentFileToolsUseMemoryGuard(t *testing.T) {
	k, dir := memoryGuardFixture(t)
	old := filepath.Join(dir, "log", "2025-01.md")
	for _, tool := range k.toolsForScope(k.allocScope(context.Background())) {
		if tool.Name() != "write_file" {
			continue
		}
		args, _ := json.Marshal(map[string]string{"path": old, "content": "- 2025-01-02 10:00 [对话] backdated\n"})
		if _, err := tool.Run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "current month") {
			t.Fatalf("subagent backdated log write: %v", err)
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("old month written")
		}
		// A valid subagent write is not booked as the foreground's own change.
		args, _ = json.Marshal(map[string]string{"path": filepath.Join(dir, "memory.md"), "content": "- (2026-10-18) from a subagent\n"})
		if _, err := tool.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if len(k.memOwn) != 0 {
			t.Fatalf("subagent write recorded as own: %v", k.memOwn)
		}
		return
	}
	t.Fatal("no write_file in subagent tools")
}

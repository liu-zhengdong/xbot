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
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func guardWrite(t *testing.T, k *Kernel, path, body string) error {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"path": path, "content": body})
	tool := &tools.WriteFile{}
	if _, err := os.Stat(path); err == nil {
		read := &tools.ReadFile{}
		raw, _ := json.Marshal(map[string]string{"path": path})
		if _, err := (reviewedTool{Tool: read, k: k}).Run(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	_, err := (reviewedTool{Tool: tool, k: k}).Run(context.Background(), args)
	return err
}

func TestMemoryFileProfileGuard(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	if err := guardWrite(t, k, path, "- (2026-10-18) old\n"); err != nil {
		t.Fatal(err)
	}
	if err := guardWrite(t, k, path, "- (2026-10-18) "+strings.Repeat("x", memory.ProfileBytes+1)); err == nil {
		t.Fatal("over-cap write accepted")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "- (2026-10-18) old\n" {
		t.Fatal("file was not restored")
	}
	if err := os.WriteFile(path, []byte("- (2026-10-18) "+strings.Repeat("x", memory.ProfileBytes+10)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := guardWrite(t, k, path, "- (2026-10-18) "+strings.Repeat("x", memory.ProfileBytes+1)); err != nil {
		t.Fatalf("shrinking: %v", err)
	}
}

func TestMemoryFileLogGuard(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Now()
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root), Now: func() time.Time { return now }}
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	path := filepath.Join(dir, "log", now.Format("2006-01")+".md")
	line := "- " + now.Format("2006-01-02 15:04") + " [对话] first\n"
	if err := guardWrite(t, k, path, line); err != nil {
		t.Fatal(err)
	}
	if err := guardWrite(t, k, path, line+strings.Replace(line, "first", "second", 1)); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(path)
	if err := guardWrite(t, k, path, strings.Replace(string(old), "first", "changed", 1)); err == nil {
		t.Fatal("edit accepted")
	}
	if err := guardWrite(t, k, path, string(old)+"bad line\n"); err == nil {
		t.Fatal("bad format accepted")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != string(old) {
		t.Fatal("log not restored")
	}
	past := filepath.Join(dir, "log", now.AddDate(0, -1, 0).Format("2006-01")+".md")
	if err := guardWrite(t, k, past, line); err == nil {
		t.Fatal("past month accepted")
	}
	if _, err := os.Stat(past); !os.IsNotExist(err) {
		t.Fatal("rejected new file retained")
	}
}

func TestMemoryFileUntrustedReview(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, turn: &turn{}}
	path := filepath.Join(b.Root, "people", "u1", "memory", "notes.txt")
	// A symlink alias and .. still resolve into a memory folder.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(b.Root, "alias")
	if err := os.Symlink(filepath.Dir(path), alias); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(alias, "child", "..", "notes.txt"), "content": "note"})
	tool := &tools.WriteFile{}
	need, summary, err := k.reviewCall(context.Background(), tool, args, "write")
	if err != nil || !need || !strings.Contains(summary, "untrusted") {
		t.Fatalf("untrusted: %v %q %v", need, summary, err)
	}
	k.turn.turnTrusted = true
	need, _, err = k.reviewCall(context.Background(), tool, args, "write")
	if err != nil || need {
		t.Fatalf("trusted: %v %v", need, err)
	}
}

func TestMemoryFileOwnEditNoDiff(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- (2026-10-18) original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "old_string": "original", "new_string": "own edited fact"})
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "one"}, {Role: provider.RoleAssistant, Content: "two"}}}
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root), Provider: p, MaxSteps: 2}
	memDiffRun(t, k, 2, func(i int) {
		if i != 0 {
			return
		}
		read := &tools.ReadFile{}
		raw, _ := json.Marshal(map[string]string{"path": path})
		if _, err := (reviewedTool{Tool: read, k: k}).Run(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
		if _, err := (reviewedTool{Tool: &tools.EditFile{}, k: k}).Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	})
	for _, up := range envUpdates(t, b) {
		if strings.Contains(up, "<memory_diff>") {
			t.Fatalf("own edit reported: %s", up)
		}
	}
}

func TestMemoryFileCaseFoldProfileCap(t *testing.T) {
	previous := memCaseFold
	memCaseFold = true
	t.Cleanup(func() { memCaseFold = previous })

	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "MEMORY.md")
	err := guardWrite(t, k, path, strings.Repeat("x", memory.ProfileBytes+1))
	if err == nil {
		t.Fatal("case-variant over-cap write accepted")
	}
	if !strings.Contains(err.Error(), "memory.md exceeds 4096 bytes") {
		t.Fatalf("expected profile cap rejection, got: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected new file retained: %v", err)
	}
}

package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestSubagentCwdDoesNotMoveParent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "other-task", "README"), "x\n")
	k, _, now := workBot(t, dir)
	before := k.shellCwd()

	var sub *tools.Shell
	for _, tl := range k.subTools() {
		if s, ok := tl.(*tools.Shell); ok {
			sub = s
		}
	}
	if sub == nil {
		t.Fatal("subagent tool set has no shell")
	}
	args, _ := json.Marshal(map[string]string{"description": "Verify the scenario behavior", "command": "cd other-task"})
	if _, err := sub.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	after := k.shellCwd()
	if after != before {
		t.Errorf("a subagent's cd moved the parent bot's cwd: %s -> %s", before, after)
	}

	runTurn(t, k, now, "write the notes",
		toolCall("c1", "write_file", `{"path":"notes.md","content":"parent notes\n"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")})
	if _, err := os.Stat(filepath.Join(dir, "other-task", "notes.md")); err == nil {
		t.Errorf("parent's write_file(notes.md) landed in the subagent's directory other-task/")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Errorf("parent write_file(notes.md) missing at the foreground cwd: %v", err)
	}
}

func TestBackgroundCwdDoesNotMoveForeground(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "other", "README"), "x\n")
	k, _, now := workBot(t, dir)
	before := k.shellCwd()
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	var sh tools.Tool
	for _, tl := range list {
		if tl.Name() == "sh" {
			sh = tl
		}
	}
	if sh == nil {
		t.Fatal("model tools have no shell")
	}
	const branch = "bg_other"
	k.adoptForeground(branch)
	bg := &turn{branch: branch}
	ctx := context.WithValue(context.Background(), turnKey{}, bg)
	args, _ := json.Marshal(map[string]string{"description": "Verify the scenario behavior", "command": "cd other"})
	if _, err := sh.Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	if got := k.shellCwd(); got != before {
		t.Errorf("background cd moved the foreground cwd: %s -> %s", before, got)
	}
	if got := k.cwdFor(ctx); got == before || !filepath.IsAbs(got) {
		t.Fatalf("background cwd = %s, foreground = %s", got, before)
	}
	cwdFile := filepath.Join(k.Bot.Root, "bots", k.Bot.Name(), "cwd")
	if b, err := os.ReadFile(cwdFile); err == nil && filepath.Clean(string(b)) != "" {
		// Only the foreground is persisted. A background cd must not appear there
		// unless the foreground itself is already that directory.
		if filepath.Clean(string(trimNL(b))) == k.cwdFor(ctx) && k.cwdFor(ctx) != before {
			t.Errorf("background cwd was persisted to bots/<name>/cwd: %s", b)
		}
	}

	runTurn(t, k, now, "write the notes",
		toolCall("c1", "write_file", `{"path":"notes.md","content":"foreground\n"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")})
	if _, err := os.Stat(filepath.Join(dir, "other", "notes.md")); err == nil {
		t.Errorf("foreground write_file(notes.md) landed in the background directory other/")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Errorf("foreground write_file(notes.md) missing at the workspace root: %v", err)
	}
}

func TestReviewFileConflictUsesCallerCwd(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkg, 0755); err != nil {
		t.Fatal(err)
	}
	k, _, _ := workBot(t, dir)
	pkgAbs := physicalDir(t, pkg)
	k.storeCwd(pkgAbs)
	held := "file:" + filepath.Clean(filepath.Join(pkgAbs, "notes.md"))
	bg := &turn{branch: "bg_hold"}
	bg.addResource(held)
	k.bgs = map[string]*turn{bg.branch: bg}
	write := &tools.WriteFile{Root: dir}
	write.BindRoot(k.cwdFor)
	rt := reviewedTool{Tool: write, k: k}
	ctx := context.WithValue(context.Background(), turnKey{}, &turn{})
	prepared, err := rt.PrepareRun(ctx, json.RawMessage(`{"path":"notes.md","content":"x\n"}`))
	out, ran := prepared.Text, prepared.Run != nil
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatalf("relative write was not queued behind the background holding %s: %s", held, out)
	}
	if _, err := os.Stat(filepath.Join(pkg, "notes.md")); err == nil {
		t.Fatal("queued write still created the file")
	}
}

func trimNL(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

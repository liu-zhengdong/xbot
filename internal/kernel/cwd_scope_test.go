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

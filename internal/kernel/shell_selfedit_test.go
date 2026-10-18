package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
)

// A file the bot read and then rewrote with its own shell command is not
// reported as someone else's change; a later outside edit still is (#19).
func TestShellSelfEditIsNotAnExternalChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, p, now := freshBot(t)
	k.Links = []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")}
	k.WorkDir = dir
	k.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	runTurn(t, k, now, "edit",
		toolCall("r1", "read_file", mustArgs(t, map[string]string{"path": path})),
		toolCall("s1", "shell", mustArgs(t, map[string]string{"command": "printf 'new\\n' > note.txt"})),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	var shellOut string
	for _, m := range p.reqs[len(p.reqs)-1].Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == "s1" {
			shellOut = m.Content
		}
	}
	if !strings.Contains(shellOut, "changed by this command") || !strings.Contains(shellOut, "note.txt") {
		t.Fatalf("shell result: %q", shellOut)
	}
	advance(now, fileChangeCooldown+time.Second)
	runTurn(t, k, now, "again", provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if tip := newestReq(t, p); strings.Contains(tip, "<file_change>") {
		t.Fatalf("own shell edit reported as external: %q", tip)
	}
	if err := os.WriteFile(path, []byte("outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	advance(now, fileChangeCooldown+time.Second)
	runTurn(t, k, now, "third", provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if tip := newestReq(t, p); !strings.Contains(tip, "<file_change>") || !strings.Contains(tip, "note.txt changed") {
		t.Fatalf("outside edit not reported: %q", tip)
	}
}

func newestReq(t *testing.T, p *scripted) string {
	t.Helper()
	msgs := p.reqs[len(p.reqs)-1].Messages
	return msgs[len(msgs)-1].Content
}

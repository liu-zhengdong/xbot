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

func TestModelOverlayAppendsAfterRulesAndShowsInEnv(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetBinding("deepseek", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "prompts", "models", "deepseek", "deepseek-chat.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const note = "Keep tool calls short."
	if err := os.WriteFile(path, []byte(note+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	sys := p.reqs[0].Messages[0].Content
	rulesAt := strings.Index(sys, "Answer in the owner's language")
	noteAt := strings.Index(sys, "## Model\n"+note)
	if rulesAt < 0 || noteAt < 0 || rulesAt > noteAt {
		t.Fatalf("overlay placement\n%s", sys)
	}
	if linksAt := strings.Index(sys, "## Links"); linksAt >= 0 && linksAt < noteAt {
		t.Fatalf("overlay after links\n%s", sys)
	}
	updates := envUpdates(t, b)
	if len(updates) == 0 || !strings.Contains(strings.Join(updates, "\n"), "overlay: deepseek/deepseek-chat ") || !strings.Contains(strings.Join(updates, "\n"), note) {
		t.Fatalf("env %v", updates)
	}
}

func TestUnboundBotShowsNoOverlay(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.reqs[0].Messages[0].Content, "## Model") {
		t.Fatal(p.reqs[0].Messages[0].Content)
	}
	updates := envUpdates(t, b)
	if len(updates) == 0 || strings.Contains(strings.Join(updates, "\n"), "<model_overlay>") {
		t.Fatalf("env %v", updates)
	}
}

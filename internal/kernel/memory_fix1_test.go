package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestMemoryWriteRuleWithSavedRules(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory)
	if err := b.WriteRules("Saved custom rules from before memory v2."); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root), Provider: p, MaxSteps: 2}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	msgs := p.reqs[0].Messages
	if !strings.Contains(msgs[0].Content, "Saved custom rules") || !strings.Contains(msgs[0].Content, MemoryWriteRule) {
		t.Fatal("saved rules bot did not receive the write rule in its system prompt")
	}
	for _, msg := range msgs[1:] {
		if strings.Contains(msg.Content, MemoryWriteRule) {
			t.Fatal("write rule must not sit inside the memory data block")
		}
	}
}

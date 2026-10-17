package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestMemoryWriteRuleWithSavedRules(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory)
	if err := b.WriteRules("Saved custom rules from before memory v2."); err != nil {
		t.Fatal(err)
	}
	old := "## Memory\n### Bot\n- old snapshot\n"
	if err := b.SaveState(bot.State{MemRender: 2, MemSnapSet: true, MemSnap: old, MemSeen: old}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root), Provider: p, MaxSteps: 2}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	msgs := p.reqs[0].Messages
	if !strings.Contains(msgs[0].Content, "Saved custom rules") || !strings.Contains(msgs[1].Content, MemoryRule+"\n"+MemoryWriteRule+"\n") {
		t.Fatal("saved rules bot did not receive the write rule in its memory block")
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "<memory_diff>") {
			t.Fatal("render upgrade emitted a diff instead of replacing the snapshot")
		}
	}
	st, err := b.LoadState()
	if err != nil || st.MemRender != 3 {
		t.Fatalf("render version = %d: %v", st.MemRender, err)
	}
}

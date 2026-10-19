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

// TestEnvironmentUpdateIsMarkedInternal is the regression for the owner seeing
// a note the kernel wrote as if they had said it. The environment update is the
// most frequent such line. The interface decides what to show by the origin
// field and never by the text, so the kernel has to write it.
func TestEnvironmentUpdateIsMarkedInternal(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "好"}}}
	now := time.Unix(1_700_000_000, 0).UTC()
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Now: func() time.Time { return now }}
	ev := link.TerminalEvent("u1", "你好", now)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var env, owner int
	for _, e := range entries {
		switch {
		case strings.Contains(e.Message.Content, "你好"):
			owner++
			if e.IsInternal() {
				t.Fatal("the owner's own message was marked internal")
			}
		case strings.HasPrefix(e.Message.Content, "<environment_"):
			env++
			if e.Origin != bot.OriginInternal {
				t.Fatalf("an environment update was written without the internal origin: %q", e.Message.Content)
			}
		}
	}
	if env == 0 {
		t.Fatal("the turn wrote no environment update")
	}
	if owner != 1 {
		t.Fatalf("owner lines %d", owner)
	}
}

// TestOldNoteWithoutTheFieldIsStillHidden covers transcripts written before the
// origin field: their rows carry no marker, so the reader falls back to the
// opening tag. An owner line that merely mentions a tag stays visible, and a
// row that carries the field is decided by the field alone.
func TestOldNoteWithoutTheFieldIsStillHidden(t *testing.T) {
	old := bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: "<environment_update>\ntime: 2026-10-19\n</environment_update>"}}
	if !old.IsInternal() {
		t.Fatal("a pre-field environment line would show as an owner bubble")
	}
	for _, content := range []string{"[继续] 上一段用完了步数上限，接着做。", "[主人的补充] 用中文写"} {
		legacy := bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: content}}
		if !legacy.IsInternal() {
			t.Fatalf("a pre-field kernel note would show as an owner bubble: %q", content)
		}
	}
	owner := bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: "解释一下 <environment_update> 是什么"}}
	if owner.IsInternal() {
		t.Fatal("an owner line that mentions a tag was hidden")
	}
	// The field wins over the text: a note whose text looks ordinary is hidden.
	marked := bot.Entry{Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: "顺便说一句"}}
	if !marked.IsInternal() {
		t.Fatal("the origin field was ignored")
	}
	// And the owner stays visible even when what they typed opens like a note.
	// The prefix list exists for old rows only; a new owner line says so.
	for _, content := range []string{"[继续] 请接着处理", "[排队] 我改主意了", "<environment_update> 是什么"} {
		own := bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: content}}
		if own.IsInternal() {
			t.Fatalf("the owner's message was hidden by its text: %q", content)
		}
	}
}

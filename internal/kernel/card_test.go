package kernel

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/provider"
)

func TestCodingCardStaysOutOfModel(t *testing.T) {
	msgs, _ := Assemble(Input{
		Persona: "p",
		Rules:   "r",
		Entries: []bot.Entry{
			{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(code.Card("ca_aa", "running", "diff --git"))}},
			{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hello")}},
		},
	})
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Text())
		b.WriteByte('\n')
	}
	text := b.String()
	if strings.Contains(text, "coding_card") || strings.Contains(text, "diff --git") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "hello") {
		t.Fatal(text)
	}
}

func TestApprovalCardStaysOutOfModel(t *testing.T) {
	msgs, _ := Assemble(Input{
		Persona: "p",
		Rules:   "r",
		Entries: []bot.Entry{
			{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(bot.ApprovalCardText("ab12", "sh", "", "", "mkdir approved-dir", bot.ApprovalApproved))}},
			{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hello")}},
		},
	})
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Text())
		b.WriteByte('\n')
	}
	text := b.String()
	if strings.Contains(text, "approval_card") || strings.Contains(text, "mkdir approved-dir") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "hello") {
		t.Fatal(text)
	}
}

// recordCompactor keeps the turns a compaction asked the model to summarize.
type recordCompactor struct{ last []provider.Message }

func (r *recordCompactor) Compact(_ context.Context, _ string, turns []provider.Message) (string, error) {
	r.last = turns
	return "summary", nil
}

func TestCardsStayOutOfCompaction(t *testing.T) {
	rec := &recordCompactor{}
	k := &Kernel{Compactor: rec}
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(bot.ApprovalCardText("ab12", "sh", "", "", "mkdir x", bot.ApprovalApproved))}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(code.Card("ca_aa", "done", "diff --git"))}},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("real turn")}},
	}
	if _, err := k.summarize(context.Background(), entries, nil, 1, len(entries), "focus"); err != nil {
		t.Fatal(err)
	}
	if len(rec.last) != 1 || rec.last[0].Text() != "real turn" {
		t.Fatalf("compactor saw %+v", rec.last)
	}
}

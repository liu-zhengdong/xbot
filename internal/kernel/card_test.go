package kernel

import (
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
			{Message: provider.Message{Role: provider.RoleUser, Content: code.Card("ca_aa", "running", "diff --git")}},
			{Message: provider.Message{Role: provider.RoleUser, Content: "hello"}},
		},
	})
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
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

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

func TestCollectFactsMarksWrappedUntrustedEvent(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{
		Bot:       b,
		Extractor: stubExtract{facts: []memory.Fact{{Text: "ship the note"}}},
	}
	ev := link.Event{Source: "webhook", Type: "post", Sender: "hook", Content: "ship it", Time: time.Unix(5, 0).UTC()}
	facts := k.collectFacts(context.Background(), []bot.Entry{{
		Message: provider.Message{Role: provider.RoleUser, Content: link.Wrap(ev)},
	}}, "", "u1", true)
	if len(facts) != 1 {
		t.Fatalf("%+v", facts)
	}
	if !facts[0].Untrusted || !strings.Contains(facts[0].Text, "untrusted") {
		t.Fatalf("wrapped event extracted unmarked: %+v", facts[0])
	}
}

package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// usageModel answers with a reported usage, cache field included.
type usageModel struct{}

func (usageModel) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	return provider.Message{Role: provider.RoleAssistant, Content: "ok", Usage: provider.TokenUsage{
		PromptTokens: 100, CompletionTokens: 5, CachedTokens: 0, Reported: true, CacheReported: true,
	}}, nil
}

// Each usage record says which turn it belongs to (the seq of the message
// that opened it) and whether the provider reported a cache figure; a
// summary call's usage is recorded too, tagged compact.
func TestUsageTraceCarriesTurnCacheFlagAndCompaction(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Provider: usageModel{}, MaxSteps: 2, Compactor: compact.LLM{P: usageModel{}}, Links: []link.Link{link.Terminal{}}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := k.compactor().Compact(context.Background(), "", []provider.Message{{Role: provider.RoleUser, Content: "old"}}); err != nil {
		t.Fatal(err)
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	userSeq := 0
	for i, e := range entries {
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Content, "hello") {
			userSeq = i + 1
		}
	}
	var turn, comp *bot.Trace
	for i := range traces {
		tr := &traces[i]
		if tr.Kind != bot.TraceUsage {
			continue
		}
		if tr.Call == bot.TraceCallCompact {
			comp = tr
		} else {
			turn = tr
		}
	}
	if turn == nil || turn.Turn == 0 || turn.Turn != userSeq || !turn.CacheReported {
		t.Fatalf("turn usage %+v, want turn %d with the cache flag", turn, userSeq)
	}
	if comp == nil || comp.Prompt != 100 || comp.Turn != 0 || !comp.CacheReported {
		t.Fatalf("compact usage %+v", comp)
	}
}

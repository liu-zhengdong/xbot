package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// usageModel answers with a reported usage, cache field included.
type usageModel struct{}

func (usageModel) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	return provider.Message{Role: provider.RoleAssistant, Usage: provider.TokenUsage{
		PromptTokens: 100, CompletionTokens: 5, CachedTokens: 0, Reported: true, CacheReported: true,
	}, Content: provider.TextContent("ok")}, nil
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
	if _, err := k.compactor(turnSeqOf(context.Background())).Compact(context.Background(), "", []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("old")}}); err != nil {
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
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Text(), "hello") {
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

// A summary written inside a turn keeps that turn's opening seq, so a cost
// count can tell in-turn compaction from a background job, which stays 0.
func TestSummaryUsageKeepsTheTurnItRanIn(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Provider: usageModel{}, Links: []link.Link{link.Terminal{}}, Compactor: compact.LLM{P: usageModel{}}}
	inTurn := context.WithValue(context.Background(), turnKey{}, &turn{fromSeq: 7})
	if _, err := k.compactor(turnSeqOf(inTurn)).Compact(inTurn, "", []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("old")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.compactor(turnSeqOf(context.Background())).Compact(context.Background(), "", []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("old")}}); err != nil {
		t.Fatal(err)
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	var in, bg int
	for _, tr := range traces {
		if tr.Kind != bot.TraceUsage || tr.Call != bot.TraceCallCompact {
			continue
		}
		if tr.Turn != 0 {
			in = tr.Turn
		} else {
			bg++
		}
	}
	if in != 7 || bg != 1 {
		t.Fatalf("summary turns = %d in-turn, %d background; want 7 and one 0", in, bg)
	}
}

// A turn that opens over the hard ceiling compacts inside Handle, before the
// turn value is installed. That summary call now carries the
// opening message's seq instead of 0, so an in-turn compaction can be told
// from a background job by its cost.
func TestOpeningCompactionKeepsTheTurnsSeq(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedMarked(t, b, 6, 4000)
	k := &Kernel{
		Bot: b, Provider: usageModel{}, Memory: memory.NopStore{}, MaxSteps: 2,
		Links:     []link.Link{link.Terminal{}},
		Compactor: compact.LLM{P: usageModel{}},
		Budget:    compact.Budget{Ceiling: 1, KeepTail: 1},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	for i, e := range entries {
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Text(), "hello") {
			opened = i + 1
		}
	}
	if opened == 0 {
		t.Fatal("the opening message is not on the transcript")
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	var comp, ext *bot.Trace
	for i := range traces {
		if traces[i].Kind != bot.TraceUsage {
			continue
		}
		switch traces[i].Call {
		case bot.TraceCallCompact:
			comp = &traces[i]
		case bot.TraceCallExtract:
			ext = &traces[i]
		}
	}
	if comp == nil || ext != nil {
		t.Fatalf("opening compaction traces: compact %+v, extract %+v; want only compact", comp, ext)
	}
	if comp.Turn != opened {
		t.Fatalf("opening compaction ran as turn %d, want the opening seq %d", comp.Turn, opened)
	}
}

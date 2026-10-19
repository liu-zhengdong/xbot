package kernel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// TestSummaryRange pins the parser against the exact wrapper summaryMessage
// writes. The panel reads the range to say which lines a block covers, so a
// silent zero would mislabel every block.
func TestSummaryRange(t *testing.T) {
	cases := []struct {
		in       string
		from, to int
	}{
		{summaryMessage(bot.SummaryBlock{From: 1, To: 2, Text: "x"}).Content, 1, 2},
		{summaryMessage(bot.SummaryBlock{From: 39, To: 71, Text: "y"}).Content, 39, 71},
		// A malformed wrapper is not guessed at.
		{"<summary from=\"5\">z", 5, 0},
		{"<summary>", 0, 0},
		{"not a summary", 0, 0},
	}
	for _, c := range cases {
		from, to := summaryRange(c.in)
		if from != c.from || to != c.to {
			t.Errorf("summaryRange(%q) = %d,%d want %d,%d", c.in, from, to, c.from, c.to)
		}
	}
}

// TestSplitLayersBands checks the band assignment and that the block keeps its
// own row instead of being folded into history.
func TestSplitLayersBands(t *testing.T) {
	block := summaryMessage(bot.SummaryBlock{From: 3, To: 9, Text: "summarised"})
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "persona and rules"},
		{Role: provider.RoleUser, Content: "<memory>\nbot facts\n</memory>"},
		block,
		{Role: provider.RoleUser, Content: "<environment_update>\nnow: x\n</environment_update>"},
		{Role: provider.RoleUser, Content: "owner said something"},
		{Role: provider.RoleAssistant, Content: "bot said something"},
	}
	layers, entries, more := splitLayers(msgs)
	if more != 0 {
		t.Fatalf("more %d, want 0", more)
	}
	want := []string{keySys, keyMem, keyBlock, keyEnv, keyHist}
	if len(layers) != len(want) {
		t.Fatalf("layers %+v, want keys %v", layers, want)
	}
	for i, key := range want {
		if layers[i].Key != key {
			t.Fatalf("layer %d key %q, want %q (all: %+v)", i, layers[i].Key, key, layers)
		}
	}
	if layers[2].From != 3 || layers[2].To != 9 {
		t.Fatalf("block range %d-%d, want 3-9", layers[2].From, layers[2].To)
	}
	// Two history messages share one band.
	if layers[4].Tokens <= 0 {
		t.Fatal("history band has no estimate")
	}
	if len(entries) != len(msgs) {
		t.Fatalf("entries %d, want %d", len(entries), len(msgs))
	}
}

// TestContextEntryClipsAndRedacts covers the two things a preview must do:
// stay short, and never carry a secret past the vault replacer.
func TestContextEntryClipsAndRedacts(t *testing.T) {
	restore := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "sk-secret", "[x]") })
	defer restore()
	long := strings.Repeat("a", contextPreviewRunes*2)
	e := contextEntry(provider.Message{Role: provider.RoleTool, Name: "read_file", Content: "sk-secret " + long}, kindHist, 999)
	if !e.Cut {
		t.Fatal("a long content was not marked cut")
	}
	if strings.Contains(e.Preview, "sk-secret") {
		t.Fatal("the secret survived redaction")
	}
	if e.Tool != "read_file" {
		t.Fatalf("tool %q, want read_file", e.Tool)
	}
	if e.Chars == 0 {
		t.Fatal("chars is 0, so a clipped preview cannot show the real size")
	}
	if e.Tokens != 999 {
		t.Fatalf("tokens %d, want the estimate passed in", e.Tokens)
	}
	short := contextEntry(provider.Message{Role: provider.RoleUser, Content: "hi"}, kindHist, 1)
	if short.Cut {
		t.Fatal("a short content was marked cut")
	}
}

// TestEstimateToolsCountsParameters guards the band's purpose: it exists because
// tool definitions are sent but left out of the reported prompt count.
func TestEstimateToolsCountsParameters(t *testing.T) {
	defs := []provider.ToolDef{{
		Name:        "shell",
		Description: "run a command",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
	}}
	if got := estimateTools(defs); got <= 0 {
		t.Fatalf("estimate %d, want a positive count", got)
	}
	if got := estimateTools(nil); got != 0 {
		t.Fatalf("empty defs estimate %d, want 0", got)
	}
}

// TestContextSnapshotKeptWhenTheCallFails pins the reason the snapshot is taken
// before the request goes out: a provider that errors or is cancelled still
// sent a context, and that is exactly the case an audit wants to read.
func TestContextSnapshotKeptWhenTheCallFails(t *testing.T) {
	failing := &failModel{}
	k := &Kernel{Bot: nil}
	// The ring does not need a bot to record; only the snapshot labels one.
	t0 := &turn{}
	rp := &replyProvider{
		inner:  failing,
		onSend: func(req provider.Request) { k.noteContext(t0, req) },
	}
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "persona"},
		{Role: provider.RoleUser, Content: "hello"},
	}}
	if _, err := rp.Chat(context.Background(), req, nil); err == nil {
		t.Fatal("the fake provider should have failed")
	}
	k.context.mu.Lock()
	kept := len(k.context.reqs)
	k.context.mu.Unlock()
	if kept != 1 {
		t.Fatalf("kept %d requests, want 1: a failed call must still be inspectable", kept)
	}
}

// failModel always returns an error, standing in for a timeout or a provider
// rejection.
type failModel struct{}

func (failModel) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	return provider.Message{}, errors.New("boom")
}

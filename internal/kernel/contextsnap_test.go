package kernel

import (
	"context"
	"errors"
	"strconv"
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

// TestSplitLayersBands checks the band assignment, that the block keeps its own
// row instead of being folded into history, and that the current turn starts
// at the last message that woke the bot.
func TestSplitLayersBands(t *testing.T) {
	block := summaryMessage(bot.SummaryBlock{From: 3, To: 9, Text: "summarised"})
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "persona and rules"},
		{Role: provider.RoleUser, Content: "<memory>\nbot facts\n</memory>"},
		block,
		{Role: provider.RoleUser, Content: "earlier ask"},
		{Role: provider.RoleAssistant, Content: "earlier answer"},
		{Role: provider.RoleUser, Content: "<environment_update>\nnow: x\n</environment_update>"},
		{Role: provider.RoleUser, Content: "owner said something"},
		{Role: provider.RoleAssistant, Content: "bot said something"},
	}
	layers, detail := splitLayers(msgs)
	want := []string{keySys, keyMem, keyBlock, keyHist, keyEnv, keyTurn}
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
	if len(detail.Blocks) != 1 || detail.Blocks[0].From != 3 || strings.HasPrefix(detail.Blocks[0].Preview, "<summary") {
		t.Fatalf("blocks %+v, want one 3-9 block without its wrapper", detail.Blocks)
	}
	if len(detail.Turns) != 1 || detail.Turns[0].Ask != "earlier ask" {
		t.Fatalf("turns %+v, want the one earlier turn", detail.Turns)
	}
	// The opening message counts toward the turn band but is not a step.
	if len(detail.Steps) != 1 || detail.Steps[0].What != "bot said something" {
		t.Fatalf("steps %+v, want the reply as the one step", detail.Steps)
	}
}

// TestTurnStepsPairCallsAndResults covers the row the panel shows for a tool
// call: parallel calls each get their own result by call id, and a failed
// result is marked.
func TestTurnStepsPairCallsAndResults(t *testing.T) {
	restore := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "sk-secret", "[x]") })
	defer restore()
	call := func(id, name, args string) provider.ToolCall {
		return provider.ToolCall{ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: args}}
	}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "p"},
		{Role: provider.RoleUser, Content: "fix the stamps"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			call("a", "read_file", `{"path":"docs/atlas/kernel.md","lines":"1-8"}`),
			call("b", "shell", `{"command":"go test ./... sk-secret"}`),
		}},
		{Role: provider.RoleTool, ToolCallID: "b", Name: "shell", Content: "exit 1\n--- FAIL: TestX"},
		{Role: provider.RoleTool, ToolCallID: "a", Name: "read_file", Content: "verified: 2026-10-17"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call("c", "web_search", `{"query":"q"}`)}},
		{Role: provider.RoleTool, ToolCallID: "c", Name: "web_search", Content: "error: web_search: blocked"},
	}
	_, detail := splitLayers(msgs)
	if len(detail.Steps) != 3 {
		t.Fatalf("steps %+v, want 3", detail.Steps)
	}
	read, sh, web := detail.Steps[0], detail.Steps[1], detail.Steps[2]
	if read.Tool != "read_file" || read.What != "docs/atlas/kernel.md" || read.Fail || !strings.Contains(read.Preview, "verified") {
		t.Fatalf("read step %+v", read)
	}
	if sh.Tool != "shell" || !sh.Fail || !strings.Contains(sh.Preview, "FAIL") {
		t.Fatalf("shell step %+v: want its own failed result", sh)
	}
	if strings.Contains(sh.What, "sk-secret") {
		t.Fatalf("the secret survived redaction: %q", sh.What)
	}
	if !web.Fail {
		t.Fatalf("web step %+v: an error result is a failure", web)
	}
	if read.Tokens <= 0 || sh.Tokens <= 0 {
		t.Fatal("a step has no estimate")
	}
}

// TestShortPathsKeepsTheFileName pins the row text for a step that acts on a
// long absolute path: the shared prefix goes, the last two segments stay.
func TestShortPathsKeepsTheFileName(t *testing.T) {
	cases := map[string]string{
		`{"path":"/Users/me/.xbot/work/ada/docs/summary.txt"}`:    "…/docs/summary.txt",
		`{"command":"ls -1 '/Users/me/.xbot/work/ada/notes'"}`:    "ls -1 '…/ada/notes'",
		`{"path":"docs/atlas/kernel.md"}`:                         "docs/atlas/kernel.md",
		`{"command":"go test ./internal/web/... -run TestAtlas"}`: "go test ./internal/web/... -run TestAtlas",
	}
	for args, want := range cases {
		if got := whatOf(args); got != want {
			t.Errorf("whatOf(%s) = %q, want %q", args, got, want)
		}
	}
}

// TestLongHistoryKeepsCurrentTurn is the regression the old entry cap had: the
// first 120 messages were kept, so in a long session the current turn, which
// is at the end, was the part that got cut.
func TestLongHistoryKeepsCurrentTurn(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "p"}}
	for i := 0; i < 200; i++ {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "ask " + strconv.Itoa(i)},
			provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "now"})
	for i := 0; i < contextStepCap+5; i++ {
		id := strconv.Itoa(i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"step ` + id + `"}`}}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "shell", Content: "exit 0"})
	}
	req, detail := buildContextRequest(provider.Request{Messages: msgs}, 1, "")
	if len(detail.Steps) != contextStepCap || detail.SkippedSteps != 5 {
		t.Fatalf("steps %d skipped %d, want %d and 5", len(detail.Steps), detail.SkippedSteps, contextStepCap)
	}
	if last := detail.Steps[len(detail.Steps)-1]; last.What != "step "+strconv.Itoa(contextStepCap+4) {
		t.Fatalf("last step %+v, want the newest", last)
	}
	if req.TurnSteps != contextStepCap+5 {
		t.Fatalf("turn steps %d, want the whole turn counted", req.TurnSteps)
	}
	if len(detail.Turns) != contextTurnCap || detail.EarlierTurns != 200-contextTurnCap || detail.EarlierTokens <= 0 {
		t.Fatalf("turns %d earlier %d (%d tokens)", len(detail.Turns), detail.EarlierTurns, detail.EarlierTokens)
	}
	if detail.Turns[len(detail.Turns)-1].Ask != "ask 199" {
		t.Fatalf("last earlier turn %+v, want ask 199", detail.Turns[len(detail.Turns)-1])
	}
}

// TestContextRingKeepsTenAndNewestDetail pins what the panel's request picker
// can offer: the last ten requests, with rows only for the newest.
func TestContextRingKeepsTenAndNewestDetail(t *testing.T) {
	k := &Kernel{}
	t0 := &turn{}
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "p"}, {Role: provider.RoleUser, Content: "go"}}
	for i := 0; i < 13; i++ {
		id := strconv.Itoa(i)
		k.noteContext(t0, provider.Request{Messages: msgs})
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"c"}`}}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "shell", Content: "exit 0"})
	}
	k.context.mu.Lock()
	reqs, detail := k.context.reqs, k.context.detail
	k.context.mu.Unlock()
	if len(reqs) != contextSnapCap || reqs[0].Step != 4 || reqs[len(reqs)-1].Step != 13 {
		t.Fatalf("kept steps %d..%d (%d), want 4..13", reqs[0].Step, reqs[len(reqs)-1].Step, len(reqs))
	}
	if reqs[0].TurnSteps != 3 || reqs[len(reqs)-1].TurnSteps != 12 {
		t.Fatalf("turn steps %d and %d, want 3 and 12", reqs[0].TurnSteps, reqs[len(reqs)-1].TurnSteps)
	}
	if detail == nil || len(detail.Steps) != 12 {
		t.Fatalf("detail %+v, want the newest request's 12 steps", detail)
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

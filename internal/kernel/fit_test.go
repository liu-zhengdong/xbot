package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// bigTool returns a large result that differs per call, like a file read or a
// test run.
type bigTool struct{ size int }

func (bigTool) Name() string        { return "big" }
func (bigTool) Description() string { return "big" }
func (bigTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"n": map[string]any{"type": "string"},
	}}
}
func (bigTool) Confirm(json.RawMessage) (bool, string, error) { return false, "big", nil }
func (t bigTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct{ N string }
	_ = json.Unmarshal(args, &in)
	unit := "result " + in.N + " "
	return strings.Repeat(unit, t.size/len(unit)+1)[:t.size], nil
}

type bigLink struct{ size int }

func (bigLink) Name() string          { return "big" }
func (l bigLink) Tools() []tools.Tool { return []tools.Tool{bigTool{size: l.size}} }

// longTurn scripts steps tool calls, each a different call, then a reply.
// Each step's own text is talk bytes long, like a model that explains every
// step or writes a file through the call's arguments.
func longTurn(steps, talk int) []provider.Message {
	out := make([]provider.Message, 0, steps+1)
	for i := 0; i < steps; i++ {
		text := fmt.Sprintf("step %d ", i)
		text += strings.Repeat("z", talk)
		out = append(out, assistantCall(fmt.Sprintf("c%d", i), text, "big", fmt.Sprintf(`{"n":"%d"}`, i)))
	}
	return append(out, provider.Message{Role: provider.RoleAssistant, Content: "done"})
}

func fitKernel(t *testing.T, p provider.Provider, comp compact.Compactor, b compact.Budget, size int) (*Kernel, *bot.Bot) {
	t.Helper()
	bt := testBot(t, link.NameTerminal, "big")
	if err := bt.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{
		Bot: bt, Provider: p, Memory: memory.NopStore{}, Compactor: comp,
		Budget: b, MaxSteps: 60,
		Links: []link.Link{link.Terminal{}, bigLink{size: size}},
	}
	return k, bt
}

func fitTraces(t *testing.T, b *bot.Bot) map[string]int {
	t.Helper()
	trs, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, tr := range trs {
		if tr.Kind == bot.TraceFit {
			out[tr.Reason]++
		}
	}
	return out
}

// A turn of forty 2K-token results, each step explaining itself in 500 tokens,
// would reach about 100K tokens. Compaction between turns cannot touch it, so
// before fit the requests of the turn grew past the ceiling. Leaving out the
// results is not enough on its own here: the steps' text adds up to more than
// the ceiling, so the earlier steps are summarized as well.
func TestLongTurnStaysUnderTheCeiling(t *testing.T) {
	const steps = 40
	p := &scripted{resps: longTurn(steps, 2000)}
	comp := &recCompactor{}
	k, b := fitKernel(t, p, comp, compact.Budget{Ceiling: 12_000, KeepTail: 1}, 8000)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != steps+1 {
		t.Fatalf("requests %d, want %d", len(p.reqs), steps+1)
	}
	for i, req := range p.reqs {
		if n := compact.EstimateMessages(req.Messages); n > 12_000 {
			t.Fatalf("request %d is %d tokens, over the 12000 ceiling", i, n)
		}
	}
	last := p.reqs[len(p.reqs)-1].Messages
	if got := last[len(last)-1].Content; !strings.HasPrefix(got, "result 39 ") {
		t.Fatalf("latest result was cut: %.40q", got)
	}
	tr := fitTraces(t, b)
	if tr["omit"] == 0 || tr["fold_turn"] == 0 {
		t.Fatalf("traces %v, want omit and fold_turn", tr)
	}
	// Each fold is a model call and a cache miss from the turn's start, so
	// they must be rare: the cut leaves room for several steps.
	if tr["fold_turn"] > steps/4 {
		t.Fatalf("%d folds in %d steps", tr["fold_turn"], steps)
	}
	raw, err := b.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "result 0 result 0") {
		t.Fatal("the transcript lost a result that was only left out of a request")
	}
}

// Once a result is left out it stays out, so the bytes before the newest step
// match the request before and the provider's prefix cache keeps hitting.
func TestLeftOutResultsStayOut(t *testing.T) {
	const steps = 12
	p := &scripted{resps: longTurn(steps, 0)}
	// No compactor: only the first step runs, which is the one that must be
	// stable across requests.
	k, _ := fitKernel(t, p, nil, compact.Budget{Ceiling: 12_000, KeepTail: 1}, 4000)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	shown := map[string]bool{}
	for i, req := range p.reqs {
		for _, m := range req.Messages {
			if m.Role != provider.RoleTool {
				continue
			}
			out := strings.HasPrefix(m.Content, "[result left out")
			if !out && shown[m.ToolCallID] {
				t.Fatalf("request %d shows %s again after leaving it out", i, m.ToolCallID)
			}
			if out {
				shown[m.ToolCallID] = true
			}
		}
	}
	if len(shown) == 0 {
		t.Fatal("nothing was left out")
	}
}

// overflowProvider rejects a request longer than limit with the error an
// OpenAI-compatible API gives, the way a real model does when the estimate
// undercounts (Chinese text, for one).
type overflowProvider struct {
	scripted
	limit    int
	rejected int
}

func (o *overflowProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	if n := compact.EstimateMessages(req.Messages); n > o.limit {
		o.rejected++
		return provider.Message{}, provider.NewStatusError(400, fmt.Sprintf(`{"error":{"message":"This model's maximum context length is %d tokens. However, you requested %d tokens.","code":"context_length_exceeded"}}`, o.limit, n))
	}
	return o.scripted.Chat(ctx, req, onDelta)
}

// The lines say the request fits, the model says it does not. Before, the turn
// ended with the provider's error.
func TestOverflowIsCutAndSentAgain(t *testing.T) {
	p := &overflowProvider{scripted: scripted{resps: longTurn(4, 0)}, limit: 6000}
	k, b := fitKernel(t, p, &recCompactor{}, compact.Budget{Ceiling: 100_000, KeepTail: 1}, 8000)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if p.rejected == 0 {
		t.Fatal("the provider never rejected a request; the test does not reach overflow")
	}
	if p.i != len(p.resps) {
		t.Fatalf("the turn stopped after %d of %d replies", p.i, len(p.resps))
	}
	last := p.reqs[len(p.reqs)-1].Messages
	if got := last[len(last)-1].Content; !strings.HasPrefix(got, "result 3 ") {
		t.Fatalf("latest result was cut: %.40q", got)
	}
	if tr := fitTraces(t, b); tr["omit"]+tr["fold_turn"]+tr["omit_earlier"] == 0 {
		t.Fatalf("traces %v", tr)
	}
	if k.scaled(100) <= 100 {
		t.Fatal("an overflow did not raise the estimate's ratio")
	}
}

// A request rejected for another reason is not retried.
func TestOtherErrorsAreNotRetriedAsOverflow(t *testing.T) {
	for _, err := range []error{
		provider.NewStatusError(400, `{"error":{"message":"invalid tool schema"}}`),
		provider.NewStatusError(401, `{"error":{"message":"maximum context length"}}`),
		errors.New("provider: http 400: context length"),
	} {
		if provider.IsContextOverflow(err) {
			t.Fatalf("%v read as overflow", err)
		}
	}
	if !provider.IsContextOverflow(provider.NewStatusError(413, "too large")) {
		t.Fatal("413 is overflow")
	}
}

// Reported usage calibrates the estimate. Mostly Chinese text is counted near
// 0.75 tokens a character by the estimate and about one by the model.
func TestReportedUsageCalibratesTheEstimate(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	text := strings.Repeat("上下文", 1000)
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: text}}}
	est := compact.EstimateMessages(req.Messages)
	k.noteUsage(nil, req, provider.Message{Role: provider.RoleAssistant, Content: "ok", Usage: provider.TokenUsage{Reported: true, PromptTokens: est * 4 / 3}})
	if got := k.scaled(est); got < est*13/10 || got > est*4/3+1 {
		t.Fatalf("scaled %d from %d", got, est)
	}
	// An estimate above the real count is not trusted downwards.
	k.noteUsage(nil, req, provider.Message{Role: provider.RoleAssistant, Content: "ok", Usage: provider.TokenUsage{Reported: true, PromptTokens: est / 2}})
	if got := k.scaled(est); got != est {
		t.Fatalf("scaled %d, want %d", got, est)
	}
	if calibrate(10*est, est) != ratioMax {
		t.Fatal("ratio not capped")
	}
}

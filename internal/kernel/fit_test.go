package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
	var latest string
	for _, m := range last {
		if m.Role == provider.RoleTool {
			latest = m.Content
		}
	}
	if got := latest; !strings.HasPrefix(got, "result 39 ") {
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
	var latest string
	for _, m := range last {
		if m.Role == provider.RoleTool {
			latest = m.Content
		}
	}
	if got := latest; !strings.HasPrefix(got, "result 3 ") {
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

// countTool is bigTool counting how often each call runs.
type countTool struct {
	bigTool
	runs map[string]int
}

func (t countTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct{ N string }
	_ = json.Unmarshal(args, &in)
	t.runs[in.N]++
	return t.bigTool.Run(ctx, args)
}

type countLink struct{ t countTool }

func (countLink) Name() string          { return "big" }
func (l countLink) Tools() []tools.Tool { return []tools.Tool{l.t} }

// readerModel calls big for steps calls, then, once the first result has been
// left out, follows the note to expand and answers.
type readerModel struct {
	steps    int
	i        int
	expanded string
	reqs     []provider.Request
}

var leftOutSeq = regexp.MustCompile(`transcript seq (\d+)\. Call expand with from=(\d+) to=(\d+)`)

func (m *readerModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	m.reqs = append(m.reqs, snapshotReq(req))
	last := req.Messages[len(req.Messages)-1]
	if last.Role == provider.RoleTool && last.Name == "expand" {
		m.expanded = last.Content
		return provider.Message{Role: provider.RoleAssistant, Content: "done"}, nil
	}
	if m.i < m.steps {
		m.i++
		return assistantCall(fmt.Sprintf("c%d", m.i), fmt.Sprintf("reading part %d", m.i), "big", fmt.Sprintf(`{"n":"%d"}`, m.i-1)), nil
	}
	for _, msg := range req.Messages {
		if msg.ToolCallID != "c1" {
			continue
		}
		g := leftOutSeq.FindStringSubmatch(msg.Content)
		if g == nil {
			return provider.Message{Role: provider.RoleAssistant, Content: "the first result is not left out with a seq: " + msg.Content}, nil
		}
		return assistantCall("x1", "", "expand", fmt.Sprintf(`{"from":%s,"to":%s}`, g[2], g[3])), nil
	}
	return provider.Message{}, errors.New("no c1 in request")
}

// A result left out of the request names its transcript line, and expand reads
// it back as it was. Before, the note said to run the call again.
func TestLeftOutResultIsReadBackWithExpand(t *testing.T) {
	m := &readerModel{steps: 6}
	ct := countTool{bigTool: bigTool{size: 8000}, runs: map[string]int{}}
	bt := testBot(t, link.NameTerminal, link.NameContext, "big")
	if err := bt.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{
		Bot: bt, Provider: m, Memory: memory.NopStore{}, Compactor: &recCompactor{},
		Budget: compact.Budget{Ceiling: 12_000, KeepTail: 1}, MaxSteps: 20,
		Links: []link.Link{link.Terminal{}, link.Context{}, countLink{t: ct}},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.expanded, "result 0 result 0") {
		t.Fatalf("expand did not return the left-out result: %.200q", m.expanded)
	}
	if ct.runs["0"] != 1 {
		t.Fatalf("call 0 ran %d times", ct.runs["0"])
	}
}

// The turn's own summary says which transcript range it covers, so expand can
// read those steps back.
func TestTurnSummaryNamesItsRange(t *testing.T) {
	p := &scripted{resps: longTurn(40, 2000)}
	k, b := fitKernel(t, p, &recCompactor{}, compact.Budget{Ceiling: 12_000, KeepTail: 1}, 8000)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`<turn_progress from="(\d+)" to="(\d+)">`)
	var from, to int
	for _, req := range p.reqs {
		for _, msg := range req.Messages {
			if g := re.FindStringSubmatch(msg.Content); g != nil {
				from, _ = strconv.Atoi(g[1])
				to, _ = strconv.Atoi(g[2])
			}
		}
	}
	if from == 0 || to < from {
		t.Fatalf("no range on the turn summary (from %d, to %d)", from, to)
	}
	got, err := b.Slice(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Message.Role != provider.RoleAssistant || !strings.HasPrefix(got[0].Message.Content, "step 0 ") {
		t.Fatalf("range starts at %+v, want the turn's first step", got[0].Message.Role)
	}
	if last := got[len(got)-1]; last.Message.Role != provider.RoleTool && !last.IsInternal() {
		t.Fatalf("range ends on %s, want a tool result", last.Message.Role)
	}
}

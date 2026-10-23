package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
)

// ctrlText is the control the tests below send while the background is still
// working on its first step.
const ctrlText = "改用中文写注释"

// bgControlProvider asks for one wait call and then answers, so the background
// reaches its end with only the control left to decide another run.
type bgControlProvider struct {
	calls   int
	reqs    []provider.Request
	steer   func()
	steered bool
	// steerOnSummary sends the control during the closing summary, which runs
	// with no tools after the last step. That is the only window where a control
	// is still unread once agent.Run has returned, which is what the budget
	// check has to catch.
	steerOnSummary bool
}

func (p *bgControlProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	p.reqs = append(p.reqs, req)
	if p.steer != nil && !p.steered && (!p.steerOnSummary || len(req.Tools) == 0) {
		p.steered = true
		p.steer()
	}
	if p.calls > 1 || len(req.Tools) == 0 {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("这一段做完")}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: append(provider.TextContent("step "+strings.Repeat("x", 100)), provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function",
		Function: provider.FunctionCall{Name: "wait", Arguments: `{"n":1}`},
	}})...)}, nil
}

// blockingTool blocks until it is released, which is what keeps the turn on the
// main line long enough for the control to be sent.
type blockingTool struct{ release chan struct{} }

func (*blockingTool) Name() string                                  { return "wait" }
func (*blockingTool) Description() string                           { return "wait" }
func (*blockingTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (*blockingTool) Confirm(json.RawMessage) (bool, string, error) { return false, "wait", nil }
func (h *blockingTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-h.release:
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// controlKernel is forkKernel with a provider whose first step blocks on a tool
// so the caller can steer it while it works.
func controlKernel(t *testing.T) (*Kernel, chan struct{}, *bgControlProvider) {
	t.Helper()
	k, g, _ := forkKernel(t)
	rel := make(chan struct{})
	k.Links = []link.Link{link.Terminal{}, oneToolLink{&blockingTool{release: rel}}}
	p := &bgControlProvider{}
	k.Provider = p
	k.MaxSteps = 4
	k.ForegroundMaxTools = 1
	k.BackgroundMaxTime = -1
	g.release = rel
	return k, rel, p
}

// requestsWithControl lists the model calls that were given the control text.
func requestsWithControl(p *bgControlProvider) []int {
	var idx []int
	for i, r := range p.reqs {
		for _, m := range r.Messages {
			if m.Role == provider.RoleUser && strings.Contains(m.Text(), ctrlText) {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

// runControlledBackground posts the owner event, lets the turn fork, steers it,
// releases the first wait call, and waits for the completion event. It returns
// that event's text.
func runControlledBackground(t *testing.T, k *Kernel, rel chan struct{}) string {
	t.Helper()
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := k.Inbox.Post(ev); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundWait)
	defer cancel()
	if err := k.Handle(ctx, ev); err != nil {
		t.Fatalf("turn did not fork: %v", err)
	}
	bgs := k.Backgrounds()
	if len(bgs) != 1 {
		t.Fatalf("backgrounds %d", len(bgs))
	}
	id := bgs[0].ID
	if _, err := k.ControlSubagent(context.Background(), id, "steer", ctrlText); err != nil {
		t.Fatalf("steer %s: %v", id, err)
	}
	close(rel)
	waitNoBackgrounds(t, k, id)
	return waitForkEnd(t, k, id).Content
}

// TestControlEarnsAnotherRun checks a control that arrives while the
// background is still working is read by the run it starts, and is not
// reported unread.
func TestControlEarnsAnotherRun(t *testing.T) {
	k, rel, p := controlKernel(t)
	text := runControlledBackground(t, k, rel)
	if idx := requestsWithControl(p); len(idx) == 0 {
		t.Fatal("the control never reached the model")
	}
	if strings.Contains(text, "没来得及处理的控制") {
		t.Fatalf("a control that did run is reported unread: %q", text)
	}
	if !strings.Contains(text, "done") {
		t.Fatalf("the completion event does not report success: %q", text)
	}
}

// TestControlOnFinishedBackgroundIsRefused checks the other half: once the
// background is gone, a control has nowhere to go and says so.
func TestControlOnFinishedBackgroundIsRefused(t *testing.T) {
	k, rel, _ := controlKernel(t)
	runControlledBackground(t, k, rel)
	if _, err := k.ControlSubagent(context.Background(), "bg_gone", "steer", ctrlText); err == nil {
		t.Fatal("a control on a finished background was accepted")
	}
}

// TestControlIsUnreadWhenTheBudgetIsSpent is the regression for a control that
// starts another run on a background already out of time. The run loop used to
// consume controls before it looked at the clock: an expired background got a
// fresh run with every tool, and only the Guard after the first tool result
// noticed. The clock is now read before the controls are consumed.
//
// The control has to arrive during the closing summary: that is the only window
// where it is still pending once agent.Run has returned, and the summary always
// runs past the budget in this fixture.
func TestControlIsUnreadWhenTheBudgetIsSpent(t *testing.T) {
	k, rel, p := controlKernel(t)
	k.BackgroundMaxTime = time.Nanosecond
	p.steer = func() {
		for _, bg := range k.Backgrounds() {
			if _, err := k.ControlSubagent(context.Background(), bg.ID, "steer", ctrlText); err != nil {
				t.Errorf("steer %s: %v", bg.ID, err)
			}
		}
	}
	p.steerOnSummary = true
	text := runControlledBackground(t, k, rel)
	if calls := p.calls; calls > 2 {
		t.Fatalf("a spent background started another run: %d model calls", calls)
	}
	if !strings.Contains(text, "没来得及处理的控制") || !strings.Contains(text, ctrlText) {
		t.Fatalf("the unread control is not reported: %q", text)
	}
	if !strings.Contains(text, "background_budget") {
		t.Fatalf("the completion event does not say the budget stopped it: %q", text)
	}
}

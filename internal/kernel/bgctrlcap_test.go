package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"xbot/internal/provider"
)

// ctrlText is the control the tests below send while the background is already
// out of budget for another segment.
const ctrlText = "改用中文写注释"

// bgControlProvider answers every step with another wait call and posts a
// control during the closing summary, which runs after the last step of the
// segment. That is the only window where a control can still be unread when the
// segment loop decides whether another segment may run.
type bgControlProvider struct {
	calls int
	reqs  []provider.Request
	steer func()
	// steered keeps the control to the first closing summary. A later summary
	// would send one more control that nothing can run any more, which is what
	// the "no budget left" test wants but this one does not.
	steered bool
}

func (p *bgControlProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	p.reqs = append(p.reqs, req)
	if len(req.Tools) == 0 {
		// The closing summary: tools are disabled here, so this is not a step.
		if p.steer != nil && !p.steered {
			p.steered = true
			p.steer()
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "这一段做完"}, nil
	}
	return provider.Message{
		Role:    provider.RoleAssistant,
		Content: fmt.Sprintf("step %d %s", p.calls, strings.Repeat("x", 2200)),
		ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("c%d", p.calls), Type: "function",
			Function: provider.FunctionCall{Name: "wait", Arguments: fmt.Sprintf(`{"n":%d}`, p.calls)},
		}},
	}, nil
}

// controlAtCapKernel is segmentKernel with a provider that sends a steer to its
// own background during every closing summary.
func controlAtCapKernel(t *testing.T) (*Kernel, chan struct{}, *bgControlProvider) {
	t.Helper()
	k, rel, _ := segmentKernel(t)
	p := &bgControlProvider{}
	p.steer = func() {
		for _, bg := range k.Backgrounds() {
			if _, err := k.ControlSubagent(context.Background(), bg.ID, "steer", ctrlText); err != nil {
				t.Errorf("steer %s: %v", bg.ID, err)
			}
		}
	}
	k.Provider = p
	return k, rel, p
}

// requestsWithControl lists the model calls that were given the control text.
func requestsWithControl(p *bgControlProvider) []int {
	var idx []int
	for i, r := range p.reqs {
		for _, m := range r.Messages {
			if m.Role == provider.RoleUser && strings.Contains(m.Content, ctrlText) {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

// TestControlIsNotDroppedWhenTheBudgetRefusesASegment is the regression for the
// ordering bug: the segment loop consumed pending controls before it asked the
// budget, so a control that arrived when no segment could run any more was
// marked applied, never reached the model, and was not reported as unread
// either. It must stay pending and be reported.
func TestControlIsNotDroppedWhenTheBudgetRefusesASegment(t *testing.T) {
	k, rel, p := controlAtCapKernel(t)
	k.MaxSegments = 1
	id := runBackground(t, k, rel, "一直做")
	text := endEvent(t, k, id)

	if !strings.Contains(text, "没来得及处理的控制") || !strings.Contains(text, ctrlText) {
		t.Fatalf("the completion event does not report the unread control: %q", text)
	}
	if !strings.Contains(text, "段数上限") {
		t.Fatalf("the completion event does not say where it stopped: %q", text)
	}
	if calls := p.calls; calls != 3 {
		t.Fatalf("model calls %d, want 3 (2 steps plus the closing summary, one segment)", calls)
	}
	if idx := requestsWithControl(p); len(idx) != 0 {
		t.Fatalf("the control was given to model calls %v even though no segment ran for it", idx)
	}
	if n := continueNotes(t, k); n != 0 {
		t.Fatalf("continue notes %d, want none", n)
	}
}

// TestControlRunsInTheSegmentItEarns checks the other half: while budget is
// left, a control that arrives at the cap does earn the next segment and
// reaches the model there.
func TestControlRunsInTheSegmentItEarns(t *testing.T) {
	k, rel, p := controlAtCapKernel(t)
	k.MaxSegments = 2
	id := runBackground(t, k, rel, "一直做")
	text := endEvent(t, k, id)

	if idx := requestsWithControl(p); len(idx) == 0 {
		t.Fatal("the control never reached the model in the segment it earned")
	}
	if strings.Contains(text, "没来得及处理的控制") {
		t.Fatalf("a control that did run is reported unread: %q", text)
	}
	if !strings.Contains(text, "2 段") || !strings.Contains(text, "段数上限") {
		t.Fatalf("the completion event does not report the two segments: %q", text)
	}
	if calls := p.calls; calls != 6 {
		t.Fatalf("model calls %d, want 6 (2 segments x (2 steps + 1 summary))", calls)
	}
	// The segment the control earned must not also carry the step-cap note.
	if n := continueNotes(t, k); n != 1 {
		t.Fatalf("continue notes %d, want 1 (only the segment that really hit the cap)", n)
	}
}

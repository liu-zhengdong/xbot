package agent

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/provider"
)

// A completed answer wins when the newer words arrive in the cleanup boundary
// after Chat returned. The caller handles those words afterwards, as the kernel
// does in drive's existing closeInterject path.
func TestCompletedReplySurvivesLatePreemption(t *testing.T) {
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("A completed")}, {Role: provider.RoleAssistant, Content: provider.TextContent("B completed")}}}
	pending := []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("B")}}
	var recorded []provider.Message
	opening := []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("A")}}
	got, err := Run(context.Background(), p, nil, opening, Options{
		Preempt:   func(stop context.CancelFunc) func() { return stop },
		Preempted: func() []provider.Message { out := pending; pending = nil; return out },
		OnMessage: func(m provider.Message) { recorded = append(recorded, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.i != 1 || len(got) != 1 || got[0].Text() != "A completed" || len(recorded) != 1 || len(pending) != 1 {
		t.Fatalf("completed answer discarded: calls=%d messages=%v pending=%v", p.i, got, pending)
	}
	convo := append(append(opening, got...), pending...)
	more, err := Run(context.Background(), p, nil, convo, Options{})
	if err != nil || len(more) != 1 || more[0].Text() != "B completed" {
		t.Fatalf("next input: %v %v", more, err)
	}
	req := p.reqs[1].Messages
	if len(req) != 3 || req[1].Text() != "A completed" || req[2].Text() != "B" {
		t.Fatalf("completed answer/input order: %v", req)
	}
}

type cancelledRequest struct {
	calls int
	t     *testing.T
}

func (p *cancelledRequest) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	if p.calls == 1 {
		if ctx.Err() == nil {
			p.t.Fatal("first request was not cancelled")
		}
		return provider.Message{}, ctx.Err()
	}
	var text strings.Builder
	for _, m := range req.Messages {
		text.WriteString(m.Text() + "\n")
	}
	for _, want := range []string{"opening", "B1", "B2", "B3", "B4", "B5"} {
		if !strings.Contains(text.String(), want) {
			p.t.Fatalf("resend lost %s: %s", want, text.String())
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("all five handled")}, nil
}
func TestCancelledRequestResendsAllPendingInputs(t *testing.T) {
	p := &cancelledRequest{t: t}
	var pending []provider.Message
	for _, s := range []string{"B1", "B2", "B3", "B4", "B5"} {
		pending = append(pending, provider.Message{Role: provider.RoleUser, Content: provider.TextContent(s)})
	}
	var recorded []provider.Message
	got, err := Run(context.Background(), p, nil, []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("opening")}}, Options{
		Preempt: func(stop context.CancelFunc) func() {
			if p.calls == 0 {
				stop()
			}
			return func() {}
		},
		Preempted: func() []provider.Message { out := pending; pending = nil; return out },
		OnMessage: func(m provider.Message) { recorded = append(recorded, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 || len(got) != 6 || len(recorded) != 6 || got[5].Text() != "all five handled" {
		t.Fatalf("calls=%d messages=%v recorded=%v", p.calls, got, recorded)
	}
}

package agent

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/provider"
)

type cancelledSuccess struct {
	calls int
	t     *testing.T
}

func (p *cancelledSuccess) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	if p.calls == 1 {
		if ctx.Err() == nil {
			p.t.Fatal("first request was not cancelled")
		}
		// The provider finished concurrently with cancellation and reports nil error.
		return provider.Message{Role: provider.RoleAssistant, Content: "discarded draft"}, nil
	}
	var text strings.Builder
	for _, m := range req.Messages {
		text.WriteString(m.Content + "\n")
	}
	for _, s := range []string{"opening", "B1", "B2", "B3", "B4", "B5"} {
		if !strings.Contains(text.String(), s) {
			p.t.Fatalf("missing %s in resend: %s", s, text.String())
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "all five handled"}, nil
}
func TestCancelledSuccessfulDraftIsNotCommitted(t *testing.T) {
	p := &cancelledSuccess{t: t}
	pending := []provider.Message{}
	for _, s := range []string{"B1", "B2", "B3", "B4", "B5"} {
		pending = append(pending, provider.Message{Role: provider.RoleUser, Content: s})
	}
	var recorded []provider.Message
	got, err := Run(context.Background(), p, nil, []provider.Message{{Role: provider.RoleUser, Content: "opening"}}, Options{
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
	if p.calls != 2 || len(got) != 6 || len(recorded) != 6 {
		t.Fatalf("calls=%d messages=%v recorded=%v", p.calls, got, recorded)
	}
	for _, m := range recorded {
		if m.Content == "discarded draft" {
			t.Fatal("cancelled draft became an answer")
		}
	}
}

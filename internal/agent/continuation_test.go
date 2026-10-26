package agent

import (
	"context"
	"strings"
	"testing"
	"xbot/internal/provider"
)

type continueProvider func(context.Context, provider.Request, func(string)) (provider.Message, error)

func (f continueProvider) Chat(ctx context.Context, r provider.Request, d func(string)) (provider.Message, error) {
	return f(ctx, r, d)
}

func TestContinuationPreemptionDiscardsPrefix(t *testing.T) {
	calls := 0
	var stop context.CancelFunc
	p := continueProvider(func(ctx context.Context, req provider.Request, d func(string)) (provider.Message, error) {
		calls++
		if calls == 1 {
			d("old prefix")
			return provider.Message{Content: provider.TextContent("old prefix"), StopReason: "length"}, nil
		}
		if calls == 2 {
			stop()
			return provider.Message{}, ctx.Err()
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Text(), "old prefix") {
				t.Fatal("cancelled prefix replayed")
			}
		}
		if req.Messages[len(req.Messages)-1].Text() != "new input" {
			t.Fatal("interjection missing")
		}
		return provider.Message{Content: provider.TextContent("new answer")}, nil
	})
	var visible []string
	got, err := Run(context.Background(), p, nil, []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("opening")}}, Options{
		Preempt: func(cancel context.CancelFunc) func() { stop = cancel; return func() {} },
		Preempted: func() []provider.Message {
			return []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("new input")}}
		},
		OnDelta: func(s string) { visible = append(visible, s) },
	})
	if err != nil || calls != 3 || len(got) != 2 || len(visible) != 1 || visible[0] != "new answer" {
		t.Fatalf("calls=%d produced=%v visible=%q err=%v", calls, got, visible, err)
	}
}

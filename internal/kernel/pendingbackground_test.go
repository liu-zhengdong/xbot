package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestPendingApprovalBackgroundAcceptsNextHandle(t *testing.T) {
	k, _, _ := forkKernel(t)
	w := &confirmWrite{}
	k.Links = append(k.Links, gateLinkOf(w))
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "pending-send", Type: "function", Function: provider.FunctionCall{Name: "send_email", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, Content: "new message answered"},
		{Role: provider.RoleAssistant, Content: "background completed"},
	}}
	k.Provider = p
	k.ForegroundMaxTime = 100 * time.Millisecond
	release := make(chan struct{})
	asked := make(chan struct{})
	k.Approve = func(ctx context.Context, tool, summary string) (bool, error) {
		appendCard(t, k.Bot, "pending-card", tool, summary, bot.ApprovalPending, time.Now())
		close(asked)
		select {
		case <-release:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		waitNoBackgrounds(t, k)
	}()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "send something", time.Unix(10, 0))); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	default:
		t.Fatal("approval was not pending")
	}
	bgs := k.Backgrounds()
	if len(bgs) != 1 {
		t.Fatalf("backgrounds: %+v", bgs)
	}
	id := bgs[0].ID
	next := link.TerminalEvent("u1", "new message while pending", time.Unix(20, 0))
	start := time.Now()
	if err := k.Handle(context.Background(), next); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	t.Logf("foreground Handle while approval pending: %s", time.Since(start))
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.EventID == next.ID && strings.Contains(e.Message.Content, next.Content) && e.Branch == "" {
			found = true
		}
		if e.Message.ToolCallID == "pending-send" {
			t.Fatal("live call was closed before approval")
		}
	}
	if !found {
		t.Fatal("new user message missing from main transcript")
	}
	close(release)
	waitNoBackgrounds(t, k, id)
	if w.ran != 1 {
		t.Fatalf("approved tool ran %d times", w.ran)
	}
	fin := k.FinishedBackgrounds()
	if len(fin) != 1 || fin[0].Status != "done" {
		t.Fatalf("background completion: %+v", fin)
	}
	// An ordinary provider error must also finish its foreground turn.
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "provider now fails", time.Unix(30, 0))); err == nil {
		t.Fatal("expected exhausted provider error")
	}
	if state := k.Status().State; state != StateIdle {
		t.Fatalf("status after Handle error: %s", state)
	}
	for i, req := range p.reqs {
		pending := map[string]bool{}
		for _, m := range req.Messages {
			if m.Role != provider.RoleTool && len(pending) != 0 {
				t.Fatalf("model call %d has intervening %s before results: %v", i, m.Role, pending)
			}
			for _, tc := range m.ToolCalls {
				pending[tc.ID] = true
			}
			if m.Role == provider.RoleTool {
				delete(pending, m.ToolCallID)
			}
		}
		if len(pending) != 0 {
			t.Fatalf("model call %d has unanswered calls: %v", i, pending)
		}
	}
	if len(p.reqs) != 4 {
		t.Fatalf("model calls: %d", len(p.reqs))
	}
	// Exercise the early repair error return, before the agent loop starts.
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "broken", Function: provider.FunctionCall{Name: "send_email", Arguments: "{}"}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: "invalid intervening row"}}); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "repair fails", time.Unix(40, 0))); err == nil || !strings.Contains(err.Error(), "unanswered tool calls are not at the tail") {
		t.Fatalf("repair error: %v", err)
	}
	if state := k.Status().State; state != StateIdle {
		t.Fatalf("status after repair error: %s", state)
	}
}

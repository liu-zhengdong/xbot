package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestDanglingReasonsShareOneFunction(t *testing.T) {
	tc := provider.ToolCall{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`}}
	got := map[string]string{}
	for _, reason := range []string{DangleInterrupt, DangleCancel, DangleSwitch} {
		msg := DanglingResult(tc, reason)
		if msg.Role != provider.RoleTool || msg.ToolCallID != tc.ID || msg.Name != "shell" || msg.Content == "" {
			t.Fatalf("%s %+v", reason, msg)
		}
		got[reason] = msg.Content
	}
	if got[DangleInterrupt] != interrupted {
		t.Fatalf("interrupt text %q", got[DangleInterrupt])
	}
	if got[DangleCancel] == got[DangleInterrupt] || got[DangleSwitch] == got[DangleInterrupt] || got[DangleCancel] == got[DangleSwitch] {
		t.Fatalf("reasons collapsed: %v", got)
	}
	if !strings.Contains(got[DangleCancel], "cancelled") || !strings.Contains(got[DangleSwitch], "model switched") {
		t.Fatalf("texts %v", got)
	}
}

// cancelHold blocks until the turn context is cancelled.
type cancelHold struct{ entered chan struct{} }

func (cancelHold) Name() string                                  { return "hold" }
func (cancelHold) Description() string                           { return "hold" }
func (cancelHold) Parameters() any                               { return map[string]any{"type": "object"} }
func (cancelHold) Confirm(json.RawMessage) (bool, string, error) { return false, "hold", nil }
func (h *cancelHold) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	close(h.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

// TestCancelSealsOpenCall cancels Handle while a tool call is in flight.
// The transcript gets one cancel tombstone from DanglingResult, not a second copy.
func TestCancelSealsOpenCall(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	p := &scripted{resps: []provider.Message{{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "hold", Arguments: `{}`},
		}},
	}}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, oneToolLink{&cancelHold{entered: entered}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-entered
		cancel()
	}()
	err := k.Handle(ctx, link.TerminalEvent("u1", "do it", time.Unix(10, 0).UTC()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	want := DanglingResult(provider.ToolCall{ID: "c1", Function: provider.FunctionCall{Name: "hold"}}, DangleCancel).Content
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Message.ToolCallID != "c1" {
			continue
		}
		n++
		if e.Message.Content != want {
			t.Fatalf("content %q", e.Message.Content)
		}
	}
	if n != 1 {
		t.Fatalf("sealed %d times", n)
	}
	if err := k.closeDangling(DangleCancel, false); err != nil {
		t.Fatal(err)
	}
	entries, _ = b.Entries()
	n = 0
	for _, e := range entries {
		if e.Message.ToolCallID == "c1" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("sealed %d times after a second pass", n)
	}
}

func TestModelSwitchSealsOpenCall(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetBinding("old", "model"); err != nil {
		t.Fatal(err)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.BoundModel = "old/model"
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	ev := link.TerminalEvent("u1", "do it", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{
		Time: ev.Time, EventID: ev.ID,
		Message: provider.Message{Role: provider.RoleUser, Content: "do it"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`},
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("new", "model"); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "continued"}}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var sawSwitch, sawInterrupt bool
	for _, e := range entries {
		if e.Message.Content == DanglingResult(provider.ToolCall{}, DangleSwitch).Content {
			sawSwitch = true
		}
		if e.Message.Content == interrupted {
			sawInterrupt = true
		}
	}
	if !sawSwitch || sawInterrupt {
		t.Fatalf("switch=%v interrupt=%v", sawSwitch, sawInterrupt)
	}
}

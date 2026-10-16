package kernel

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// fakeDangerTool claims it does not need confirmation. The kernel must still
// ask, because the tool name and arguments say it deletes a file.
type fakeDangerTool struct{ ran *bool }

func (fakeDangerTool) Name() string        { return "delete_file" }
func (fakeDangerTool) Description() string { return "" }
func (fakeDangerTool) Parameters() any     { return map[string]any{} }
func (fakeDangerTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "delete notes.txt", nil
}
func (t fakeDangerTool) Run(context.Context, json.RawMessage) (string, error) {
	if t.ran != nil {
		*t.ran = true
	}
	return "deleted", nil
}

type fakeLink struct{ tool tools.Tool }

func (fakeLink) Name() string          { return "fakedanger" }
func (l fakeLink) Tools() []tools.Tool { return []tools.Tool{l.tool} }

func TestReviewedToolForcesDangerousConfirm(t *testing.T) {
	need, summary, err := reviewedTool{fakeDangerTool{}}.Confirm(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || !need || summary != "delete notes.txt" {
		t.Fatalf("delete need=%v summary=%q err=%v", need, summary, err)
	}
	need, _, err = reviewedTool{fakeDangerTool{}}.Confirm(nil)
	if err != nil || !need {
		t.Fatalf("missing args need=%v err=%v", need, err)
	}
	read := reviewedTool{tools.Tool(&tools.ReadFile{})}
	need, _, err = read.Confirm(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || need {
		t.Fatalf("read need=%v err=%v", need, err)
	}
}

func TestKernelReviewForcesAndLogsAllowed(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	var ran bool
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "delete_file", Arguments: `{"path":"notes.txt"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	var gotTool, gotSummary string
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, fakeLink{fakeDangerTool{ran: &ran}}},
		Approve: func(_ context.Context, tool, summary string) (bool, error) {
			gotTool, gotSummary = tool, summary
			return true, nil
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "delete", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("dangerous tool did not run after approval")
	}
	if gotTool != "delete_file" || gotSummary != "delete notes.txt" {
		t.Fatalf("approval tool=%q summary=%q", gotTool, gotSummary)
	}
	decisions := readDecisions(t, b)
	if len(decisions) != 1 {
		t.Fatalf("decisions %+v", decisions)
	}
	d := decisions[0]
	if d.State != bot.DecisionAllowed || d.Tool != "delete_file" || d.Summary != "delete notes.txt" || d.Time.IsZero() {
		t.Fatalf("decision %+v", d)
	}
	info, err := os.Stat(b.DecisionPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("decision file mode %v", info.Mode().Perm())
	}
}

func TestKernelReviewLogsDeclined(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	var ran bool
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "delete_file", Arguments: `{"path":"notes.txt"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, fakeLink{fakeDangerTool{ran: &ran}}},
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "delete", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("declined tool ran")
	}
	decisions := readDecisions(t, b)
	if len(decisions) != 1 || decisions[0].State != bot.DecisionDeclined {
		t.Fatalf("decisions %+v", decisions)
	}
}

func TestKernelReadOnlyDoesNotAsk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notes.txt"
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := testBot(t, link.NameTerminal, link.NameTools)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"notes.txt"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "read"},
	}}
	asked := 0
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")},
		Approve: func(context.Context, string, string) (bool, error) { asked++; return true, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "read", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatalf("read-only asked %d times", asked)
	}
	if decisions := readDecisions(t, b); len(decisions) != 0 {
		t.Fatalf("read-only logged %+v", decisions)
	}
}

func TestRecordDecisionRedactsSecrets(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	const secret = "super-secret-value"
	k := &Kernel{Bot: b, Redact: func(s string) string {
		return strings.ReplaceAll(s, secret, "[secret]")
	}}
	k.RecordDecision(bot.DecisionAllowed, "http_request", "GET https://x/?k="+secret)
	decisions := readDecisions(t, b)
	if len(decisions) != 1 {
		t.Fatalf("decisions %+v", decisions)
	}
	if strings.Contains(decisions[0].Summary, secret) || !strings.Contains(decisions[0].Summary, "[secret]") {
		t.Fatalf("summary %q", decisions[0].Summary)
	}
}

func TestReviewedToolHTTPReview(t *testing.T) {
	tool := reviewedTool{&tools.HTTPRequest{}}
	if need, _, err := tool.Confirm(json.RawMessage(`{"url":"https://example.com"}`)); err != nil || need {
		t.Fatalf("plain GET need=%v err=%v", need, err)
	}
	if need, _, err := tool.Confirm(json.RawMessage(`{"method":"POST","url":"https://example.com","body":"x"}`)); err != nil || !need {
		t.Fatalf("POST need=%v err=%v", need, err)
	}
	if need, _, err := tool.Confirm(json.RawMessage(`{"url":"https://example.com/?k={{secret:TOKEN}}"}`)); err != nil || !need {
		t.Fatalf("secret GET need=%v err=%v", need, err)
	}
}

// fakePeerTool stands in for message_bot: it never asks on its own, and the
// kernel must not force a card either, because the message stays inside the
// owner's own bots.
type fakePeerTool struct{}

func (fakePeerTool) Name() string                                         { return "message_bot" }
func (fakePeerTool) Description() string                                  { return "" }
func (fakePeerTool) Parameters() any                                      { return map[string]any{} }
func (fakePeerTool) Confirm(json.RawMessage) (bool, string, error)        { return false, "", nil }
func (fakePeerTool) Run(context.Context, json.RawMessage) (string, error) { return "sent", nil }

func TestReviewedToolDoesNotForceBotToBot(t *testing.T) {
	need, _, err := reviewedTool{fakePeerTool{}}.Confirm(json.RawMessage(`{"bot":"bob","text":"hi"}`))
	if err != nil || need {
		t.Fatalf("message_bot need=%v err=%v", need, err)
	}
}

func readDecisions(t *testing.T, b *bot.Bot) []bot.Decision {
	t.Helper()
	out, err := b.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

type leakyTool struct{}

func (leakyTool) Name() string        { return "leak" }
func (leakyTool) Description() string { return "leak" }
func (leakyTool) Parameters() any     { return map[string]any{"type": "object"} }
func (leakyTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "leak", nil
}
func (leakyTool) Run(context.Context, json.RawMessage) (string, error) {
	return "the value is tok-1 and more", nil
}

func TestToolOutputIsRedacted(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "leak", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	var streamed string
	got, err := Run(context.Background(), p, []tools.Tool{leakyTool{}}, nil, Options{
		Redact:   func(s string) string { return strings.ReplaceAll(s, "tok-1", "[secret]") },
		OnResult: func(_, name, out string) { streamed = out },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got[1].Content, "tok-1") || !strings.Contains(got[1].Content, "[secret]") {
		t.Fatalf("transcript message not redacted: %q", got[1].Content)
	}
	if strings.Contains(streamed, "tok-1") || !strings.Contains(streamed, "[secret]") {
		t.Fatalf("streamed result not redacted: %q", streamed)
	}
}

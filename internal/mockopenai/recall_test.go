package mockopenai

import (
	"encoding/json"
	"testing"
)

func TestMemoryRecallRuntimePublicMockScript(t *testing.T) {
	const command = `"$XBOT_BIN" memory-recall --self --query tea --scope person`
	name, raw, content := peerMockResponse(t, []map[string]any{{"role": "user", "content": "RECALL_PERSON"}})
	var invocation struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(raw), &invocation); err != nil {
		t.Fatal(err)
	}
	if name != "sh" || content != "" || invocation.Command != command {
		t.Fatalf("wrong actual person recall: tool=%q command=%q content=%q", name, invocation.Command, content)
	}
	for _, tc := range []struct {
		name, tool, args string
		completed        bool
	}{
		{"canonical", "sh", raw, true},
		{"unrelated_shell", "sh", `{"command":"printf tea"}`, false},
		{"wrong_scope", "sh", `{"command":"\"$XBOT_BIN\" memory-recall --self --query tea --scope bot"}`, false},
		{"wrong_query", "sh", `{"command":"\"$XBOT_BIN\" memory-recall --self --query coffee --scope person"}`, false},
		{"legacy_native", "recall", `{"query":"tea","scope":"person"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := []map[string]any{
				{"role": "user", "content": "RECALL_PERSON"},
				{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call_recall", "type": "function", "function": map[string]string{"name": tc.tool, "arguments": tc.args}}}},
				{"role": "tool", "tool_call_id": "call_recall", "content": "person:memory.md:1: Ada likes tea"},
			}
			name, _, content := peerMockResponse(t, messages)
			if tc.completed {
				if name != "" || content != "recalled\n" {
					t.Fatalf("canonical recall did not finish: tool=%q content=%q", name, content)
				}
			} else if name != "sh" || content != "" {
				t.Fatalf("unrelated call advanced recall: tool=%q content=%q", name, content)
			}
		})
	}
}

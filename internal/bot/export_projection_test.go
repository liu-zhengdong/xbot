package bot

import (
	"bytes"
	"encoding/json"
	"testing"
	"xbot/internal/provider"
)

func TestExportProjectionDoesNotMutateInternalMessage(t *testing.T) {
	source := provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{
		{Type: provider.PartReasoning, Text: "private thought", VendorRaw: json.RawMessage(`{"signature":"private-signature"}`)},
		{Type: provider.PartText, Text: "answer", VendorRaw: json.RawMessage(`{"internal":"private metadata"}`)},
		{Type: provider.PartToolCall, ToolCall: &provider.ToolCall{ID: "c1", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}},
	}}
	before, _ := json.Marshal(source)
	for _, tools := range []bool{false, true} {
		first, ok := visibleMessage(source, tools)
		if !ok {
			t.Fatal("answer dropped")
		}
		for _, p := range first.Content {
			if p.Type == provider.PartReasoning || len(p.VendorRaw) > 0 {
				t.Fatal("private payload retained")
			}
		}
		second, ok := visibleMessage(first, tools)
		if !ok {
			t.Fatal("second projection dropped answer")
		}
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(second)
		if !bytes.Equal(a, b) {
			t.Fatal("projection is not idempotent")
		}
	}
	after, _ := json.Marshal(source)
	if !bytes.Equal(before, after) {
		t.Fatal("projection mutated shared content")
	}
}

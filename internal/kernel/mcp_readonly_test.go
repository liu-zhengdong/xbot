package kernel

import (
	"context"
	"encoding/json"
	"testing"
)

type hintTool struct{ ro bool }

func (hintTool) Name() string        { return "mcp_srv_lookup" }
func (hintTool) Description() string { return "" }
func (hintTool) Parameters() any     { return map[string]any{"type": "object"} }
func (hintTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "mcp srv lookup", nil
}
func (hintTool) Run(context.Context, json.RawMessage) (string, error) { return "", nil }
func (t hintTool) ReadOnlyHint() bool                                 { return t.ro }

func TestMCPReadOnlyHintDoesNotAsk(t *testing.T) {
	k := &Kernel{}
	need, _, err := k.reviewCall(hintTool{ro: true}, json.RawMessage(`{}`), "s")
	if err != nil || need {
		t.Fatalf("readOnlyHint mcp: need=%v err=%v", need, err)
	}
	need, _, err = k.reviewCall(hintTool{ro: false}, json.RawMessage(`{}`), "s")
	if err != nil || !need {
		t.Fatalf("plain mcp must ask: need=%v err=%v", need, err)
	}
}

package code

import (
	"encoding/json"
	"testing"
	"xbot/internal/provider"
)

func TestRunnerEndpointKeepsThinkingControls(t *testing.T) {
	ep := endpointOf(&provider.Client{Thinking: provider.ThinkingOn, ReasoningEffort: "high"})
	raw, err := json.Marshal(ep)
	if err != nil {
		t.Fatal(err)
	}
	var back Endpoint
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Thinking != provider.ThinkingOn || back.ReasoningEffort != "high" {
		t.Fatal("runner endpoint lost thinking settings")
	}
}

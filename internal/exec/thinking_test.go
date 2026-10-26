package exec

import (
	"testing"
	"xbot/internal/provider"
)

func TestRunnerProviderKeepsThinkingControls(t *testing.T) {
	p, err := (&Local{}).providerFor(StartRequest{Model: Endpoint{BaseURL: "http://127.0.0.1", APIKey: "fake", Model: "deepseek-flash", Thinking: provider.ThinkingOn, ReasoningEffort: "max"}})
	if err != nil {
		t.Fatal(err)
	}
	ep, ok := provider.EndpointOf(p)
	if !ok || ep.Thinking != provider.ThinkingOn || ep.ReasoningEffort != "max" {
		t.Fatal("runner provider lost thinking settings")
	}
}

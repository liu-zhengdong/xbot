package eval

import (
	"testing"
	"xbot/internal/config"
	"xbot/internal/provider"
)

func TestOpenRealUsesBindingProtocolAndWrappedCapabilities(t *testing.T) {
	t.Setenv("XBOT_EVAL_FAKE_PROTOCOL_KEY", "fake")
	root := t.TempDir()
	if err := config.Add(root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_EVAL_FAKE_PROTOCOL_KEY"}, config.ModelSpec{Name: "gpt-5.5", Context: 1000000}); err != nil {
		t.Fatal(err)
	}
	p, err := openReal(root, "openai/gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if !provider.HasHostedSearch(p) {
		t.Fatalf("eval discarded Responses capability: %T", p)
	}
	if !provider.HasHostedSearch(newActivity(t.TempDir(), p)) {
		t.Fatal("eval meter hid binding capabilities")
	}
}

package config

import "testing"

func TestSetImageGenRoundTrip(t *testing.T) {
	root := t.TempDir()
	off := false
	if err := Add(root, Provider{Name: "openai", Protocol: Responses, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "K"}, ModelSpec{Name: "gpt-5.5", Context: 1000, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	if err := SetImageGen(root, "openai", "gpt-5.5", true, " gpt-image-2.5-flare "); err != nil {
		t.Fatal(err)
	}
	_, m, err := Lookup(root, "openai", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	// Search stays off; generation is its own switch.
	if !m.HasHostedImageGen(Responses) || m.HasHostedSearch(Responses) || m.ImageGenModel != "gpt-image-2.5-flare" {
		t.Fatalf("spec %+v", m)
	}
	// A context update through Add keeps the switch.
	if err := Add(root, Provider{Name: "openai"}, ModelSpec{Name: "gpt-5.5", Context: 2000}); err != nil {
		t.Fatal(err)
	}
	if _, m, _ = Lookup(root, "openai", "gpt-5.5"); !m.HasHostedImageGen(Responses) || m.ImageGenModel != "gpt-image-2.5-flare" {
		t.Fatalf("add dropped image generation: %+v", m)
	}
	if err := SetImageGen(root, "openai", "gpt-5.5", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, m, _ = Lookup(root, "openai", "gpt-5.5"); m.HasHostedImageGen(Responses) || m.ImageGenModel != "" || m.HostedImageGen == nil {
		t.Fatalf("off not saved: %+v", m)
	}
}

func TestSetImageGenRefuses(t *testing.T) {
	root := t.TempDir()
	if err := Add(root, Provider{Name: "deepseek", Protocol: ChatCompletions, BaseURL: "https://api.deepseek.com", APIKeyEnv: "K"}, ModelSpec{Name: "deepseek-flash", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := SetImageGen(root, "deepseek", "deepseek-flash", true, ""); err == nil {
		t.Fatal("completions binding took hosted image generation")
	}
	if err := SetImageGen(root, "deepseek", "missing", false, ""); err == nil {
		t.Fatal("unknown model accepted")
	}
	if err := SetImageGen(root, "deepseek", "deepseek-flash", false, `bad"model`); err == nil {
		t.Fatal("bad image model accepted")
	}
	on := true
	if err := SupportedBinding(ChatCompletions, ModelSpec{Name: "x", HostedImageGen: &on}); err == nil {
		t.Fatal("hand-edited completions binding with image generation accepted")
	}
	if (ModelSpec{HostedImageGen: &on}).HasHostedImageGen(AnthropicMessages) {
		t.Fatal("messages binding mounts image generation")
	}
}

package config

import (
	"testing"
)

func TestProtocolsAndModelCapabilities(t *testing.T) {
	root := t.TempDir()
	for _, p := range []Provider{{Name: "deepseek", BaseURL: "http://localhost/v1", APIKeyEnv: "FAKE_DS"}, {Name: "openai", BaseURL: "http://localhost/v1", APIKeyEnv: "FAKE_GPT"}, {Name: "anthropic", BaseURL: "http://localhost/v1", APIKeyEnv: "FAKE_CLAUDE"}} {
		m := ModelSpec{Name: "deepseek-flash", Context: 1000000}
		if p.Name == "openai" {
			m.Name = "gpt-5.5"
		}
		if p.Name == "anthropic" {
			m.Name = "claude-sonnet-4-5"
		}
		if err := Add(root, p, m); err != nil {
			t.Fatal(err)
		}
	}
	getenv := func(string) string { return "fake" }
	ds, _, err := Resolve(root, "deepseek", "deepseek-flash", getenv)
	if err != nil || ds.Protocol != ChatCompletions || ds.HostedSearch {
		t.Fatalf("deepseek %+v %v", ds, err)
	}
	gpt, _, err := Resolve(root, "openai", "gpt-5.5", getenv)
	if err != nil || gpt.Protocol != Responses || !gpt.HostedSearch || !gpt.Vision {
		t.Fatalf("gpt %+v %v", gpt, err)
	}
	if claude, _, err := Resolve(root, "anthropic", "claude-sonnet-4-5", getenv); err != nil || claude.Protocol != AnthropicMessages || claude.HostedSearch {
		t.Fatalf("messages binding %+v %v", claude, err)
	}
	if err := Add(root, Provider{Name: "bad", Protocol: "wrong", BaseURL: "http://localhost", APIKeyEnv: "FAKE"}, ModelSpec{Name: "fake", Context: 1}); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	off := false
	if err := Add(root, Provider{Name: "openai"}, ModelSpec{Name: "gpt-5.5", Context: 272000, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	gpt, _, err = Resolve(root, "openai", "gpt-5.5", getenv)
	if err != nil || gpt.HostedSearch {
		t.Fatalf("override %+v %v", gpt, err)
	}
}

func TestAnthropicExplicitHostedCapability(t *testing.T) {
	on, off := true, false
	for _, m := range []ModelSpec{{Name: "claude-sonnet-4-5"}, {Name: "claude-sonnet-4-5", HostedSearch: &on}, {Name: "claude-sonnet-4-5", HostedSearch: &off}} {
		if err := SupportedBinding(AnthropicMessages, m); err != nil {
			t.Fatal(err)
		}
		if m.HasHostedSearch(AnthropicMessages) != (m.HostedSearch != nil && *m.HostedSearch) {
			t.Fatalf("implicit Claude hosted capability %+v", m)
		}
	}
	if SupportedBinding(ChatCompletions, ModelSpec{HostedSearch: &on}) == nil {
		t.Fatal("Completions accepted hosted")
	}
}

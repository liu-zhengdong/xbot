package config

import (
	"testing"

	"xbot/internal/provider"
)

func TestOpenAIModelCapabilitiesResolve(t *testing.T) {
	for _, name := range []string{"gpt-5.4", "gpt-5.5", "gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
		"gpt-6", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1", "gpt-6.1-sol"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			p := Provider{Name: "openai", Protocol: Responses, BaseURL: "http://localhost/v1", APIKeyEnv: "FAKE_GPT"}
			on, off := true, false
			for _, flag := range []*bool{nil, &off, &on} {
				if err := Add(root, p, ModelSpec{Name: name, Context: 1000000, Vision: flag, HostedSearch: flag}); err != nil {
					t.Fatal(err)
				}
				endpoint, _, err := Resolve(root, p.Name, name, func(string) string { return "fake" })
				want := flag == nil || *flag
				if err != nil || endpoint.Protocol != Responses || endpoint.Vision != want || endpoint.HostedSearch != want {
					t.Fatalf("resolved capabilities: vision=%t hosted=%t err=%v; want %t", endpoint.Vision, endpoint.HostedSearch, err, want)
				}
				bound, err := provider.New(endpoint)
				if err != nil || provider.HasHostedSearch(bound) != want {
					t.Fatalf("bound hosted search: %v; want %t", err, want)
				}
			}
			for _, protocol := range []string{ChatCompletions, AnthropicMessages} {
				if (ModelSpec{Name: name}).HasHostedSearch(protocol) {
					t.Errorf("implicit hosted search on %s", protocol)
				}
			}
		})
	}
}

func TestHostedSearchDefaultsAndOverrides(t *testing.T) {
	on, off := true, false
	for _, name := range []string{"deepseek-flash", "deepseek-chat", "deepseek-v4-pro", "claude-sonnet-4-5", "claude-opus-4-6",
		"gpt-4o", "gpt-5.60", "gpt-6.10-sol", "gpt-60-sol", "gpt-6.2-sol", "gpt-6-unknown", "other/gpt-6-sol"} {
		for _, protocol := range []string{Responses, AnthropicMessages, ChatCompletions} {
			for _, flag := range []*bool{nil, &off, &on} {
				want := protocol != ChatCompletions && flag != nil && *flag
				if got := (ModelSpec{Name: name, HostedSearch: flag}).HasHostedSearch(protocol); got != want {
					t.Errorf("%s %s explicit=%v: got %t want %t", name, protocol, flag, got, want)
				}
			}
		}
	}
}

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

package config

import (
	"fmt"
	"xbot/internal/provider"
)

const (
	ChatCompletions   = provider.ChatCompletions
	Responses         = provider.ResponsesProtocol
	AnthropicMessages = provider.AnthropicMessages
)

// WireProtocol preserves existing compatible endpoints. Only the known native
// provider names infer a different protocol; base_url never selects a protocol.
func (p Provider) WireProtocol() string {
	if p.Protocol != "" {
		return p.Protocol
	}
	switch p.Name {
	case "openai":
		return Responses
	case "anthropic":
		return AnthropicMessages
	default:
		return ChatCompletions
	}
}

func ValidProtocol(protocol string) error {
	switch protocol {
	case ChatCompletions, Responses, AnthropicMessages:
		return nil
	default:
		return fmt.Errorf("providers: unknown protocol %q", protocol)
	}
}

func (m ModelSpec) HasHostedSearch(protocol string) bool {
	if protocol != Responses && protocol != AnthropicMessages {
		return false
	}
	if m.HostedSearch != nil {
		return *m.HostedSearch
	}
	return protocol == Responses && openAIImageAndSearch(m.Name)
}

// openAIImageAndSearch lists known OpenAI models with image input and Responses
// web_search. Account catalogs supply membership, not these capabilities.
func openAIImageAndSearch(name string) bool {
	switch name {
	case "gpt-5.4", "gpt-5.5", "gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
		"gpt-6", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1", "gpt-6.1-sol":
		return true
	default:
		return false
	}
}

func SupportedBinding(protocol string, m ModelSpec) error {
	if err := ValidProtocol(protocol); err != nil {
		return err
	}
	if m.HostedSearch != nil && *m.HostedSearch && protocol != Responses && protocol != AnthropicMessages {
		return fmt.Errorf("providers: hosted_web_search requires responses or anthropic_messages")
	}
	return nil
}

package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const (
	ChatCompletions   = "chat_completions"
	ResponsesProtocol = "responses"
	AnthropicMessages = "anthropic_messages"
)

type Endpoint struct {
	Protocol     string
	HostedSearch bool
	APIKey       string
	BaseURL      string
	Model        string
	KeyEnv       string
	Thinking     string
	Vision       bool
	HTTPClient   *http.Client
}

// New resolves protocol once at the model boundary, never from base_url.
func New(m Endpoint) (Provider, error) {
	if strings.HasPrefix(m.APIKey, "{{secret:") && m.HTTPClient == nil {
		return nil, fmt.Errorf("provider: OAuth requires the local vault transport; remote runners are not supported")
	}
	c := Client{BaseURL: m.BaseURL, APIKey: m.APIKey, Model: m.Model, Thinking: m.Thinking, HTTPClient: m.HTTPClient}
	switch m.Protocol {
	case "", ChatCompletions:
		return &c, nil
	case ResponsesProtocol:
		return &Responses{Client: c, WebSearch: m.HostedSearch}, nil
	case AnthropicMessages:
		return &Anthropic{Client: c, WebSearch: m.HostedSearch}, nil
	default:
		return nil, fmt.Errorf("provider: unsupported protocol %q", m.Protocol)
	}
}

func HasHostedSearch(p Provider) bool {
	if w, ok := p.(interface{ UnwrapProvider() Provider }); ok {
		return HasHostedSearch(w.UnwrapProvider())
	}
	switch p := p.(type) {
	case *Responses:
		return p.WebSearch
	case *Anthropic:
		return p.WebSearch
	}
	return false
}

// EndpointOf keeps protocol/capabilities on the existing local runner path.
func EndpointOf(p Provider) (Endpoint, bool) {
	var c *Client
	m := Endpoint{Protocol: ChatCompletions}
	switch p := p.(type) {
	case *Client:
		c = p
	case *Responses:
		c = &p.Client
		m.Protocol = ResponsesProtocol
		m.HostedSearch = p.WebSearch
	case *Anthropic:
		c = &p.Client
		m.Protocol = AnthropicMessages
		m.HostedSearch = p.WebSearch
	default:
		return m, false
	}
	if c == nil {
		return m, false
	}
	m.BaseURL, m.APIKey, m.Model, m.Thinking = c.BaseURL, c.APIKey, c.Model, c.Thinking
	m.HTTPClient = c.HTTPClient
	return m, true
}

type partObserverKey struct{}

// WithPartObserver carries ephemeral hosted progress through existing provider
// wrappers. The completed Message alone is persisted in the transcript.
func WithPartObserver(ctx context.Context, f func(ContentPart)) context.Context {
	return context.WithValue(ctx, partObserverKey{}, f)
}

func emitPart(ctx context.Context, p ContentPart) {
	if f, ok := ctx.Value(partObserverKey{}).(func(ContentPart)); ok && f != nil {
		f(p)
	}
}

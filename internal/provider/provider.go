// Package provider is the model boundary. M1 speaks the OpenAI-compatible
// Chat Completions API (tool calls included). The agent depends on Provider,
// not on a vendor SDK.
package provider

import "context"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat message. Tool call arguments are a JSON string.
// ReplyTo is a transcript sequence this message answers. It stays off the
// provider request; the transcript record stores it.
// Usage is the model-reported token count for this completion. It stays off
// the provider request and the transcript; the kernel copies it into the trace.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ReplyTo    int        `json:"-"`
	Usage      TokenUsage `json:"-"`
}

// TokenUsage is one completion's token count. Reported is true only when the
// API sent a usage object. Estimated is set by the kernel when it had to guess.
type TokenUsage struct {
	PromptTokens     int  `json:"prompt_tokens,omitempty"`
	CompletionTokens int  `json:"completion_tokens,omitempty"`
	CachedTokens     int  `json:"cached_tokens,omitempty"`
	Reported         bool `json:"-"`
	Estimated        bool `json:"-"`
}

// ToolCall is a model request to run a tool.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name and raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef is a tool schema offered to the model.
type ToolDef struct {
	Name        string
	Description string
	Parameters  any
}

// Request is one completion.
type Request struct {
	Messages []Message
	Tools    []ToolDef
}

// Provider completes a turn. onDelta receives assistant text as it streams.
// Tool-call arguments are not streamed to onDelta; they are returned on Message.
type Provider interface {
	Chat(ctx context.Context, req Request, onDelta func(string)) (Message, error)
}

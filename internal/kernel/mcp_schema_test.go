package kernel

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/provider"
)

func TestMainLineNeverDeclaresMCPSchemas(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	if err := mcp.AddMCP(b.MCPPath(), mcp.Spec{Name: "external", Command: "does-not-exist-mcp-fixture"}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list {
		if strings.HasPrefix(tool.Name(), "mcp_") {
			t.Fatalf("MCP tool exposed: %s", tool.Name())
		}
	}
	for group := range k.toolRegistry().index() {
		if strings.HasPrefix(group, "mcp:") {
			t.Fatalf("MCP group exposed: %s", group)
		}
	}
}

// Provider schema errors are ordinary errors after MCP model adapter retirement.
// The remaining overflow retry and its fitting hooks still run independently.
func TestModelSchemaErrorsDoNotRetry(t *testing.T) {
	for _, kind := range []string{"main", "legacy-child"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			inner := providerFunc(func(context.Context, provider.Request, func(string)) (provider.Message, error) {
				calls++
				return provider.Message{}, provider.NewStatusError(400, "Invalid schema for function 'native_lookup'")
			})
			var p provider.Provider
			if kind == "main" {
				p = &replyProvider{inner: inner}
			} else {
				p = &fitProvider{inner: inner, k: &Kernel{}, fit: newTurnFit(1, nil)}
			}
			_, err := p.Chat(context.Background(), provider.Request{Tools: []provider.ToolDef{{Name: "native_lookup", Parameters: map[string]any{"type": "object"}}}}, nil)
			if err == nil || calls != 1 {
				t.Fatalf("schema error retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestReplyOverflowStillRetriesOnce(t *testing.T) {
	calls, overflow := 0, 0
	inner := providerFunc(func(context.Context, provider.Request, func(string)) (provider.Message, error) {
		calls++
		if calls == 1 {
			return provider.Message{}, provider.NewStatusError(400, "maximum context length exceeded")
		}
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
	})
	p := &replyProvider{inner: inner, fit: func(_ context.Context, r provider.Request, o bool) provider.Request {
		if o {
			overflow++
		}
		return r
	}}
	msg, err := p.Chat(context.Background(), provider.Request{}, nil)
	if err != nil || msg.Text() != "ok" || calls != 2 || overflow != 1 {
		t.Fatalf("msg=%v err=%v calls=%d overflow=%d", msg, err, calls, overflow)
	}
}

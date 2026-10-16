package kernel

import (
	"strings"
	"testing"

	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/vault"
)

func TestScrubEntryUsesRedact(t *testing.T) {
	k := &Kernel{Redact: func(s string) string {
		return strings.ReplaceAll(s, "tok-value", "[secret]")
	}}
	out := k.scrubEntry(provider.Message{
		Role:    provider.RoleTool,
		Content: "result tok-value here",
		ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "http_request", Arguments: `{"url":"http://a.com/?t=tok-value"}`},
		}},
	})
	if strings.Contains(out.Content, "tok-value") || !strings.Contains(out.Content, "[secret]") {
		t.Fatalf("content not redacted: %q", out.Content)
	}
	if strings.Contains(out.ToolCalls[0].Function.Arguments, "tok-value") {
		t.Fatalf("arguments not redacted: %q", out.ToolCalls[0].Function.Arguments)
	}
}

func TestModelToolsIncludeVaultToolsOnlyWithVault(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	toolsLink := link.NewToolsLink()
	k := &Kernel{Bot: b, Links: []link.Link{toolsLink}, Vault: store}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolName(list, "http_request") || !hasToolName(list, "list_secrets") {
		t.Fatal("vault tools missing with a vault")
	}

	bare := &Kernel{Bot: b, Links: []link.Link{toolsLink}}
	list, err = bare.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if hasToolName(list, "http_request") || hasToolName(list, "list_secrets") {
		t.Fatal("vault tools listed without a vault")
	}
}

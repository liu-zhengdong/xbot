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
	out := k.scrubEntry(provider.Message{Role: provider.RoleTool, Content: append(provider.TextContent("result tok-value here"), provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function",
		Function: provider.FunctionCall{Name: "http_request", Arguments: `{"url":"http://a.com/?t=tok-value"}`},
	}})...)})
	if strings.Contains(out.Text(), "tok-value") || !strings.Contains(out.Text(), "[secret]") {
		t.Fatalf("content not redacted: %q", out.Text())
	}
	if strings.Contains(out.Calls()[0].Function.Arguments, "tok-value") {
		t.Fatalf("arguments not redacted: %q", out.Calls()[0].Function.Arguments)
	}
}

// http_request is retired into the runtime CLI (ADR 0068): the vault stays on
// the kernel for the runtime handler, but no vault tool name may reach the
// model whether a vault exists or not.
func TestModelToolsNeverListRetiredVaultTools(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	for label, k := range map[string]*Kernel{
		"vault":    {Bot: b, Vault: store},
		"no-vault": {Bot: b},
	} {
		list, err := k.modelTools()
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"http_request", "list_secrets"} {
			if hasToolName(list, name) || registered(k, name) {
				t.Fatalf("%s kernel lists retired tool %s", label, name)
			}
		}
	}
}

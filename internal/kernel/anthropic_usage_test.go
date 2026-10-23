package kernel

import (
	"strings"
	"testing"

	"xbot/internal/compact"
	"xbot/internal/provider"
)

func TestContinuationBillingDoesNotInflateContext(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("context ", 100))}}}
	estimate := compact.EstimateMessages(req.Messages)
	msg := provider.Message{Role: provider.RoleAssistant, Usage: provider.TokenUsage{PromptTokens: estimate * 2, InitialPromptTokens: estimate, CompletionTokens: 11, CachedTokens: 7, Reported: true, Billed: &provider.TokenTotals{PromptTokens: estimate * 3, CompletionTokens: 22, CachedTokens: 14}}}
	k.noteUsage(nil, req, msg)
	snap := k.tokens.snap()
	if snap.Prompt != estimate*2 || snap.SessionPrompt != estimate*3 || snap.Completion != 11 || snap.SessionCompletion != 22 || snap.Cached != 7 || snap.SessionCached != 14 || k.tokens.ratio != calibrate(estimate, estimate) {
		t.Fatalf("context/billing mixed: %+v ratio=%f", snap, k.tokens.ratio)
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 1 || traces[0].Prompt != estimate*2 || traces[0].SessionPrompt != estimate*3 {
		t.Fatalf("trace accounting %+v", traces)
	}
}

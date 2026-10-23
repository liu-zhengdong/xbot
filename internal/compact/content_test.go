package compact

import (
	"strings"
	"testing"
	"xbot/internal/provider"
)

func TestRenderIncludesUnknownPartSummary(t *testing.T) {
	m := provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{
		{Type: "server_result", Text: "重要结论", Vendor: "other"},
		{Type: "future", Vendor: "other"},
	}}
	text := render(m)
	if !strings.Contains(text, "重要结论") || !strings.Contains(text, "[future content from other]") {
		t.Fatalf("facts lost: %q", text)
	}
}

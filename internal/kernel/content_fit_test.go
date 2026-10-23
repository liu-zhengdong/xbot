package kernel

import (
	"context"
	"strings"
	"testing"
	"xbot/internal/compact"
	"xbot/internal/provider"
)

type contentCaptureCompactor struct{ messages []provider.Message }

func (c *contentCaptureCompactor) Compact(_ context.Context, _ string, m []provider.Message) (string, error) {
	c.messages = append(c.messages, m...)
	return "summary", nil
}

func TestFoldFactsSplitsUnknownContentSummary(t *testing.T) {
	c := &contentCaptureCompactor{}
	k := &Kernel{Compactor: c, Budget: compact.Budget{Ceiling: 8000}}
	text := strings.Repeat("事实", 5000)
	m := provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{{Type: "server_result", Text: text, Vendor: "other"}}}
	if _, err := k.foldFacts(context.Background(), "", []provider.Message{m}); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for _, part := range c.messages {
		body := part.PortableText()
		if len(body) > 8192 {
			t.Fatalf("unsplit summary: %d", len(body))
		}
		got.WriteString(body)
	}
	if got.String() != text {
		t.Fatalf("summary lost or duplicated: got %d bytes want %d", got.Len(), len(text))
	}
	if m.Content[0].Type != "server_result" || m.Content[0].Text != text {
		t.Fatal("source mutated")
	}
}

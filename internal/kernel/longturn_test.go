package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// blockTool blocks until released.
type blockTool struct{ release chan struct{} }

func (*blockTool) Name() string        { return "block" }
func (*blockTool) Description() string { return "block" }
func (*blockTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*blockTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "block", nil
}
func (b *blockTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return "ok", nil
}

// callBlock calls the block tool with id as its argument, so a run of calls
// is not a loop of identical ones.
func callBlock(id string) provider.Message {
	args := `{"id":"` + id + `"}`
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: "block", Arguments: args}}})}
}

func capKernel(t *testing.T, resps ...provider.Message) (*Kernel, *blockTool) {
	t.Helper()
	k, _, _ := forkKernel(t)
	bt := &blockTool{release: make(chan struct{})}
	k.Links = []link.Link{link.Terminal{}, oneToolLink{tools.Tool(bt)}}
	k.Provider = &scripted{resps: resps}
	k.MaxSteps = 10
	return k, bt
}

// A long turn with no owner message stays on the main line: no step or time
// limit moves it to the background on its own (ADR 0028). Under ADR 0016 the
// fifth tool call forked it with "已转到后台".
func TestLongTurnStaysOnMainLine(t *testing.T) {
	k, bt := capKernel(t, callBlock("c1"), callBlock("c2"), callBlock("c3"), callBlock("c4"), callBlock("c5"), callBlock("c6"), provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("全部做完")})
	close(bt.release)
	var streamed strings.Builder
	k.OnDelta = func(s string) { streamed.WriteString(s) }
	ev := link.TerminalEvent("u1", "做六步", time.Unix(10, 0).UTC())
	_ = k.Inbox.Post(ev)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(streamed.String(), "已转到后台") {
		t.Fatalf("streamed %q", streamed.String())
	}
	if n := len(k.Backgrounds()) + len(k.FinishedBackgrounds()); n != 0 {
		t.Fatalf("backgrounds %d", n)
	}
	entries, _ := k.Bot.Entries()
	if last := entries[len(entries)-1]; last.Branch != "" || last.Message.Text() != "全部做完" {
		t.Fatalf("last %+v", last)
	}
}

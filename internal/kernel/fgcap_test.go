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

func callBlock(id string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: "block", Arguments: "{}"}}}}
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

func TestForegroundToolCapMovesTurnToBackground(t *testing.T) {
	k, bt := capKernel(t, callBlock("c1"), callBlock("c2"), callBlock("c3"), provider.Message{Role: provider.RoleAssistant, Content: "全部做完"})
	close(bt.release)
	k.ForegroundMaxTools = 2
	var streamed strings.Builder
	k.OnDelta = func(s string) { streamed.WriteString(s) }
	ev := link.TerminalEvent("u1", "做三步", time.Unix(10, 0).UTC())
	_ = k.Inbox.Post(ev)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(streamed.String(), "已转到后台 bg_") {
		t.Fatalf("streamed %q", streamed.String())
	}
	waitNoBackgrounds(t, k, oneBackgroundID(t, k))
	entries, _ := k.Bot.Entries()
	var notice, bgDone bool
	for _, e := range entries {
		notice = notice || (e.Branch == "" && strings.Contains(e.Message.Content, "已转到后台"))
		bgDone = bgDone || (e.Branch != "" && e.Message.Content == "全部做完")
	}
	if !notice || !bgDone {
		t.Fatalf("notice %v background answer %v", notice, bgDone)
	}
}

func TestForegroundTimeCapMovesTurnToBackground(t *testing.T) {
	k, bt := capKernel(t, callBlock("c1"), provider.Message{Role: provider.RoleAssistant, Content: "好了"})
	k.ForegroundMaxTime = 400 * time.Millisecond
	ev := link.TerminalEvent("u1", "慢活", time.Unix(10, 0).UTC())
	_ = k.Inbox.Post(ev)
	start := time.Now()
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Handle took %v", d)
	}
	if len(k.Backgrounds()) != 1 {
		t.Fatal("no background after the time cap")
	}
	id := k.Backgrounds()[0].ID
	close(bt.release)
	waitNoBackgrounds(t, k, id)
}

func oneBackgroundID(t *testing.T, k *Kernel) string {
	t.Helper()
	if bgs := k.Backgrounds(); len(bgs) == 1 {
		return bgs[0].ID
	}
	fin := k.FinishedBackgrounds()
	if len(fin) >= 1 {
		return fin[0].ID
	}
	t.Fatal("no background id")
	return ""
}

func TestForegroundCapWithSlotsFullStaysForeground(t *testing.T) {
	k, bt := capKernel(t, callBlock("c1"), callBlock("c2"), provider.Message{Role: provider.RoleAssistant, Content: "前台做完"})
	close(bt.release)
	k.ForegroundMaxTools = 1
	k.ForkCap = 1
	k.bgs = map[string]*turn{"bg_x": {branch: "bg_x"}}
	ev := link.TerminalEvent("u1", "两步", time.Unix(10, 0).UTC())
	_ = k.Inbox.Post(ev)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if n := len(k.Backgrounds()); n != 1 {
		t.Fatalf("backgrounds %d", n)
	}
	entries, _ := k.Bot.Entries()
	if last := entries[len(entries)-1]; last.Branch != "" || last.Message.Content != "前台做完" {
		t.Fatalf("last %+v", last)
	}
}

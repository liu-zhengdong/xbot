package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type gateLink struct{ g *gateTool }

func (gateLink) Name() string          { return "gate" }
func (l gateLink) Tools() []tools.Tool { return []tools.Tool{l.g} }

// gateTool posts an owner message mid-turn, then blocks until released.
type gateTool struct {
	box     *inbox.Inbox
	release chan struct{}
}

func (*gateTool) Name() string        { return "gate" }
func (*gateTool) Description() string { return "wait" }
func (*gateTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*gateTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "gate", nil
}
func (g *gateTool) Run(context.Context, json.RawMessage) (string, error) {
	if err := g.box.Post(link.TerminalEvent("u1", "插一句", time.Unix(20, 0).UTC())); err != nil {
		return "", err
	}
	<-g.release
	return "released", nil
}

func forkKernel(t *testing.T) (*Kernel, *gateTool, *inbox.Inbox) {
	t.Helper()
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	g := &gateTool{box: box, release: make(chan struct{})}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "gate", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, Content: "后台做完了"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, gateLink{g}}, AllowFork: true,
	}
	return k, g, box
}

func TestOwnerMessageForksRunningTurn(t *testing.T) {
	k, g, box := forkKernel(t)
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Handle took %v; the turn should have forked", d)
	}
	bgs := k.Backgrounds()
	if len(bgs) != 1 || !strings.HasPrefix(bgs[0].ID, "bg_") || bgs[0].Task != "做个长任务" {
		t.Fatalf("backgrounds %+v", bgs)
	}
	id := bgs[0].ID
	close(g.release)
	deadline := time.Now().Add(5 * time.Second)
	var end link.Event
	for time.Now().Before(deadline) && end.ID == "" {
		_ = box.Reload()
		for _, e := range box.Pending() {
			if e.Type == "fork_end" {
				end = e
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if end.ID == "" || !strings.Contains(end.Content, "done") || !strings.Contains(end.Content, "后台做完了") || end.Sender != id {
		t.Fatalf("completion event %+v", end)
	}
	if len(k.Backgrounds()) != 0 {
		t.Fatal("background still registered")
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var sawStart, sawEnd, sawTagged bool
	for _, e := range entries {
		if e.Mark != nil && e.Mark.Branch == id {
			sawStart = sawStart || e.Mark.Kind == bot.ForkKindStart
			sawEnd = sawEnd || e.Mark.Kind == bot.ForkKindEnd
		}
		if e.Branch == id && e.Message.Content == "后台做完了" {
			sawTagged = true
		}
	}
	if !sawStart || !sawEnd || !sawTagged {
		t.Fatalf("start %v end %v tagged %v", sawStart, sawEnd, sawTagged)
	}
}

func TestForkRespectsCap(t *testing.T) {
	k, g, box := forkKernel(t)
	k.ForkCap = 1
	k.bgs = map[string]*turn{"bg_x": {branch: "bg_x", task: "旧任务"}}
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(800 * time.Millisecond); close(g.release) }()
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if n := len(k.Backgrounds()); n != 1 {
		t.Fatalf("backgrounds %d; cap should block a second", n)
	}
}

func TestForegroundNoteListsBackgrounds(t *testing.T) {
	k, _, box := forkKernel(t)
	k.Provider = &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "好"}}}
	k.bgs = map[string]*turn{
		"bg_a": {branch: "bg_a", task: "整理日志", steps: 3, lastTool: "shell"},
		"bg_b": {branch: "bg_b", task: "写报告", steps: 1},
	}
	ev := link.TerminalEvent("u1", "现在怎样", time.Unix(30, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var note string
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "后台任务正在执行") {
			note = e.Message.Content
		}
	}
	if !strings.Contains(note, "bg_a：整理日志，已跑 3 步，最近在用 shell") || !strings.Contains(note, "bg_b：写报告") {
		t.Fatalf("note %q", note)
	}
}

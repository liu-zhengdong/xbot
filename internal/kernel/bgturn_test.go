package kernel

import (
	"context"
	"encoding/json"
	"fmt"
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
		"bg_a": {branch: "bg_a", task: "整理日志", replies: 3, lastTool: "shell"},
		"bg_b": {branch: "bg_b", task: "写报告", replies: 1},
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

func TestFinishedBackgroundsCapAndOrder(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b}
	base := time.Unix(100, 0).UTC()
	k.Now = func() time.Time { return base }
	for i := 1; i <= 4; i++ {
		bg := &turn{branch: fmt.Sprintf("bg_%d", i), task: fmt.Sprintf("任务%d", i), replies: i}
		k.finishBackground(bg, nil, nil)
		base = base.Add(time.Minute)
	}
	got := k.FinishedBackgrounds()
	if len(got) != 3 {
		t.Fatalf("finished %d; want cap 3", len(got))
	}
	for i, want := range []string{"bg_4", "bg_3", "bg_2"} {
		if got[i].ID != want || got[i].Status != "done" || got[i].Ended == nil {
			t.Fatalf("finished[%d] %+v", i, got[i])
		}
	}
	if got[0].Task != "任务4" || got[0].Steps != 4 {
		t.Fatalf("finished[0] %+v", got[0])
	}
	// A copy, so a caller cannot mutate the kernel's list.
	got[0].ID = "mutated"
	if again := k.FinishedBackgrounds(); again[0].ID != "bg_4" {
		t.Fatalf("FinishedBackgrounds aliases internal state: %+v", again)
	}
}

func TestResourceName(t *testing.T) {
	cases := []struct{ res, want string }{
		{"file:/a/b.txt", "b.txt"},
		{"dir:/x/y", "y"},
		{"repo:o/r", "r"},
		{"file:/a/b/", "b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := resourceName(c.res); got != c.want {
			t.Errorf("resourceName(%q) = %q, want %q", c.res, got, c.want)
		}
	}
}

func TestQueueItemName(t *testing.T) {
	k := &Kernel{}
	k.bgs = map[string]*turn{"bg_x": {branch: "bg_x", ctrl: []*Control{
		{ID: "q1", Kind: "conflict", Resource: "file:/a/b.txt", Text: "写文件", State: "pending"},
		{ID: "q2", Kind: "queue", Text: "清理", State: "pending"},
	}}}
	items := k.Queue()
	if len(items) != 2 {
		t.Fatalf("queue %+v", items)
	}
	if items[0].Name != "b.txt" || items[1].Name != "" {
		t.Fatalf("names %+v", items)
	}
}

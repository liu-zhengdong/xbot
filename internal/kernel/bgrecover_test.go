package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// writeRunningMeta leaves the branch's meta.json as a crash right after the fork
// would have: status running with a live step count.
func writeRunningMeta(t *testing.T, b *bot.Bot, id, task string, steps int) {
	t.Helper()
	dir := b.AgentDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"id": id, "mode": "fork-turn", "status": "running", "task": task,
		"from": 1, "steps": steps, "started": time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMeta(t *testing.T, b *bot.Bot, id string) (string, int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.AgentDir(id), "meta.json"))
	if err != nil {
		t.Fatalf("read meta %s: %v", id, err)
	}
	var m struct {
		Status string `json:"status"`
		Steps  int    `json:"steps"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Mode != "fork-turn" {
		t.Fatalf("meta mode %q", m.Mode)
	}
	return m.Status, m.Steps
}

// inboxPostCount counts raw post records with the given id; Post dedups by id, so
// a second recovery must not add another record.
func inboxPostCount(t *testing.T, path, id string) int {
	t.Helper()
	return len(inboxPosts(t, path, id))
}

// inboxPosts returns every raw post record with the given id, in log order.
func inboxPosts(t *testing.T, path, id string) []link.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []link.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec struct {
			Op    string      `json:"op"`
			Event *link.Event `json:"event"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Op == "post" && rec.Event != nil && rec.Event.ID == id {
			out = append(out, *rec.Event)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func forkEndCount(t *testing.T, b *bot.Bot, id string) int {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Mark != nil && e.Mark.Kind == bot.ForkKindEnd && e.Mark.Branch == id {
			n++
		}
	}
	return n
}

// A transcript with a fork and some branch rows but no fork_end is a crash. The
// first Handle on a fresh kernel closes it, and a second kernel does not repeat.
func TestRecoverInterruptedClosesOpenBranch(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.Append(bot.Entry{EventID: "u0", Time: time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleUser, Content: "做个长任务"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 1, "整理日志"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: "bg_7", Message: provider.Message{Role: provider.RoleAssistant, Content: "第一步"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: "bg_7", Message: provider.Message{Role: provider.RoleAssistant, Content: "第二步"}}); err != nil {
		t.Fatal(err)
	}
	writeRunningMeta(t, b, "bg_7", "整理日志", 9)

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "收到"}, {Role: provider.RoleAssistant, Content: "任务中断已报告"}}},
		Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := link.TerminalEvent("u1", "现在怎样", time.Unix(10, 0).UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if n := forkEndCount(t, b, "bg_7"); n != 1 {
		t.Fatalf("fork_end rows %d", n)
	}
	entries, _ := b.Entries()
	for _, e := range entries {
		if e.Mark != nil && e.Mark.Kind == bot.ForkKindEnd && e.Mark.Branch == "bg_7" {
			if e.Mark.Status != bot.ForkInterrupted || e.Mark.Steps != 2 {
				t.Fatalf("fork_end %+v", e.Mark)
			}
		}
	}
	if status, steps := readMeta(t, b, "bg_7"); status != "interrupted" || steps != 2 {
		t.Fatalf("meta %s %d", status, steps)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_bg_7_end"); n != 1 {
		t.Fatalf("fork_end events %d", n)
	}
	posted := inboxPosts(t, b.InboxPath(), "ev_bg_7_end")
	end := posted[0]
	if end.Source != link.SourceSubagent || end.Type != "fork_end" {
		t.Fatalf("event %+v", end)
	}
	if !strings.Contains(end.Content, "因进程重启中断") || !strings.Contains(end.Content, "整理日志") ||
		!strings.Contains(end.Content, "第 2 步") || !strings.Contains(end.Content, "没有自动续跑") {
		t.Fatalf("event content %q", end.Content)
	}

	// The method itself is idempotent, not only the once-per-lifetime call.
	k.RecoverInterrupted()
	if n := forkEndCount(t, b, "bg_7"); n != 1 {
		t.Fatalf("direct second recovery added a fork_end: %d", n)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_bg_7_end"); n != 1 {
		t.Fatalf("direct second recovery added an event: %d", n)
	}

	// A second kernel over the same transcript must not write a second marker or event.
	box2, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k2 := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "好"}}},
		Inbox: box2, Memory: memory.NopStore{}, MaxSteps: 4}
	ev2 := link.TerminalEvent("u1", "再问", time.Unix(11, 0).UTC())
	if err := k2.Handle(context.Background(), ev2); err != nil {
		t.Fatal(err)
	}
	if n := forkEndCount(t, b, "bg_7"); n != 1 {
		t.Fatalf("second open added a fork_end: %d", n)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_bg_7_end"); n != 1 {
		t.Fatalf("second open added an event: %d", n)
	}
}

// Recovery runs before repairTail, so an interrupted branch's dangling call is
// repaired with the branch tag and the fork_end does not block it.
func TestRecoverBeforeRepairTagsBranchPlaceholder(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.Append(bot.Entry{EventID: "u0", Time: time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleUser, Content: "做个长任务"}}); err != nil {
		t.Fatal(err)
	}
	callRow := bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: call("c1")}}
	if err := b.Append(callRow); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 1, "长任务"); err != nil {
		t.Fatal(err)
	}
	writeRunningMeta(t, b, "bg_7", "长任务", 1)

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "收到"}, {Role: provider.RoleAssistant, Content: "任务中断已报告"}}},
		Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := link.TerminalEvent("u1", "现在怎样", time.Unix(10, 0).UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	toolSeq, endSeq := 0, 0
	for i, e := range entries {
		if e.Message.Role == provider.RoleTool && e.Message.ToolCallID == "c1" {
			toolSeq = i + 1
			if e.Branch != "bg_7" {
				t.Fatalf("placeholder branch %q", e.Branch)
			}
			if !strings.Contains(e.Message.Content, "interrupted") {
				t.Fatalf("placeholder content %q", e.Message.Content)
			}
		}
		if e.Mark != nil && e.Mark.Kind == bot.ForkKindEnd && e.Mark.Branch == "bg_7" {
			endSeq = i + 1
		}
	}
	if toolSeq == 0 || endSeq == 0 || endSeq > toolSeq {
		t.Fatalf("order end=%d tool=%d", endSeq, toolSeq)
	}
}

// A live forking kernel that still owns backgrounds must never treat its own
// in-flight branch as a crash.
func TestRecoverSkipsLiveBackground(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.Append(bot.Entry{EventID: "u0", Time: time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleUser, Content: "做个长任务"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_live", 1, "正在跑"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: "bg_live", Message: provider.Message{Role: provider.RoleAssistant, Content: "一步"}}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Inbox: box, Memory: memory.NopStore{}, AllowFork: true,
		bgs: map[string]*turn{"bg_live": {branch: "bg_live"}}}
	k.RecoverInterrupted()
	if n := forkEndCount(t, b, "bg_live"); n != 0 {
		t.Fatalf("live branch was marked interrupted: %d", n)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_bg_live_end"); n != 0 {
		t.Fatalf("live branch got a recovery event: %d", n)
	}
}

type holdTool struct {
	started chan struct{}
	once    sync.Once
}

func (*holdTool) Name() string        { return "hold" }
func (*holdTool) Description() string { return "hold until cancelled" }
func (*holdTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*holdTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "hold", nil
}
func (h *holdTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	h.once.Do(func() { close(h.started) })
	<-ctx.Done()
	return "", ctx.Err()
}

type holdLink struct{ t *holdTool }

func (holdLink) Name() string          { return "hold" }
func (l holdLink) Tools() []tools.Tool { return []tools.Tool{l.t} }

// Cancelling a running background writes fork_end{cancelled}, a cancelled meta,
// exactly one completion event, and drops it from k.bgs.
func TestCancelBackgroundWritesForkEnd(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	h := &holdTool{started: make(chan struct{})}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "hold", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, Content: "后台完成"},
	}}
	k := &Kernel{Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, holdLink{h}}, AllowFork: true}
	// The owner's message waits in the inbox, so the running turn forks.
	if err := box.Post(link.TerminalEvent("u1", "插一句", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	bgs := k.Backgrounds()
	if len(bgs) != 1 {
		t.Fatalf("backgrounds %+v", bgs)
	}
	id := bgs[0].ID
	if _, err := k.ControlSubagent(context.Background(), id, "cancel", ""); err != nil {
		t.Fatal(err)
	}
	waitNoBackgrounds(t, k, id)

	if n := forkEndCount(t, b, id); n != 1 {
		t.Fatalf("fork_end rows %d", n)
	}
	entries, _ := b.Entries()
	for _, e := range entries {
		if e.Mark != nil && e.Mark.Kind == bot.ForkKindEnd && e.Mark.Branch == id && e.Mark.Status != bot.ForkCancelled {
			t.Fatalf("status %+v", e.Mark)
		}
	}
	if status, _ := readMeta(t, b, id); status != "cancelled" {
		t.Fatalf("meta status %q", status)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_"+id+"_end"); n != 1 {
		t.Fatalf("completion events %d", n)
	}
	if len(k.Backgrounds()) != 0 {
		t.Fatal("cancelled background still registered")
	}
}

package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
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
func inboxPosts(t *testing.T, path, id string) []inbox.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []inbox.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec struct {
			Op    string       `json:"op"`
			Event *inbox.Event `json:"event"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Op == "post" && rec.Event != nil && (id == "" || rec.Event.ID == id) {
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
	b := testBot(t, inbox.NameTerminal)
	if err := b.Append(bot.Entry{EventID: "u0", Time: time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("做个长任务")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 1, "整理日志"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: "bg_7", Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第一步")}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: "bg_7", Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第二步")}}); err != nil {
		t.Fatal(err)
	}
	writeRunningMeta(t, b, "bg_7", "整理日志", 9)

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("收到")}, {Role: provider.RoleAssistant, Content: provider.TextContent("任务中断已报告")}}},
		Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := inbox.TerminalEvent("u1", "现在怎样", time.Unix(10, 0).UTC())
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
	if end.Source != inbox.SourceSubagent || end.Type != "fork_end" {
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
	k2 := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("好")}}},
		Inbox: box2, Memory: memory.NopStore{}, MaxSteps: 4}
	ev2 := inbox.TerminalEvent("u1", "再问", time.Unix(11, 0).UTC())
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
	b := testBot(t, inbox.NameTerminal)
	if err := b.Append(bot.Entry{EventID: "u0", Time: time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("做个长任务")}}); err != nil {
		t.Fatal(err)
	}
	callRow := bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent(call("c1"))}}
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
	k := &Kernel{Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("收到")}, {Role: provider.RoleAssistant, Content: provider.TextContent("任务中断已报告")}}},
		Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := inbox.TerminalEvent("u1", "现在怎样", time.Unix(10, 0).UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	toolSeq, endSeq := 0, 0
	for i, e := range entries {
		if e.Message.Role == provider.RoleTool && e.Message.CallID() == "c1" {
			toolSeq = i + 1
			if e.Branch != "bg_7" {
				t.Fatalf("placeholder branch %q", e.Branch)
			}
			if !strings.Contains(e.Message.Text(), "interrupted") {
				t.Fatalf("placeholder content %q", e.Message.Text())
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

// Legacy metadata can still identify a process from before the engine retired.
func TestRecoverSkipsLiveLegacyProcess(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("start")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_live", 1, "正在跑"); err != nil {
		t.Fatal(err)
	}
	pid, start := tools.CurrentProcess()
	raw, err := json.Marshal(forkMetaFile{ID: "bg_live", PID: pid, ProcStart: start, Mode: "fork-turn", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.AgentDir("bg_live"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AgentDir("bg_live"), "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Inbox: box}
	k.RecoverInterrupted()
	if n := forkEndCount(t, b, "bg_live"); n != 0 {
		t.Fatalf("live process was marked interrupted: %d", n)
	}
	if n := inboxPostCount(t, b.InboxPath(), "ev_bg_live_end"); n != 0 {
		t.Fatalf("live branch got a recovery event: %d", n)
	}
}

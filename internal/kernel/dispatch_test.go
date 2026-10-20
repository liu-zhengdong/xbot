package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// roleModel answers the foreground and its subagents from separate scripts.
// A request whose system prompt is a subagent's goes to sub.
type roleModel struct {
	mu      sync.Mutex
	fore    []provider.Message
	sub     []provider.Message
	foreReq []provider.Request
	subReq  []provider.Request
	// hold, when set, keeps the subagent's first call waiting until it is
	// closed, so its result cannot be absorbed into the turn that sent it.
	hold chan struct{}
}

func (m *roleModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	isSub := len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, exec.SubPersona)
	if isSub && m.hold != nil {
		<-m.hold
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if isSub {
		m.subReq = append(m.subReq, snapshotReq(req))
		if len(m.sub) == 0 {
			return provider.Message{Role: provider.RoleAssistant, Content: "Findings\nnone"}, nil
		}
		msg := m.sub[0]
		m.sub = m.sub[1:]
		return msg, nil
	}
	m.foreReq = append(m.foreReq, snapshotReq(req))
	if len(m.fore) == 0 {
		return provider.Message{Role: provider.RoleAssistant, Content: "ok"}, nil
	}
	msg := m.fore[0]
	m.fore = m.fore[1:]
	return msg, nil
}

func dispatchKernel(t *testing.T, p provider.Provider) (*Kernel, *bot.Bot, *inbox.Inbox) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameAgents)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, Inbox: box, MaxSteps: 8,
		Dispatch: true, WorkDir: dir,
		Links: []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, ""), link.Agents{}},
	}
	return k, b, box
}

func waitEvent(t *testing.T, box *inbox.Inbox, typ string) link.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = box.Reload()
		for _, ev := range box.Pending() {
			if ev.Source == link.SourceSubagent && ev.Type == typ {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s event from the subagent; pending %+v", typ, box.Pending())
	return link.Event{}
}

// The foreground hands a task to a research subagent. The subagent gets the
// type's prompt and report sections, no tool that writes, and a shell that only
// reads; its result comes back as an event.
func TestDispatchStartsATypedSubagent(t *testing.T) {
	m := &roleModel{
		fore: []provider.Message{
			assistantCall("d1", "", "dispatch", `{"type":"research","task":"find the release date of Go 1.30"}`),
			{Role: provider.RoleAssistant, Content: "已派调研员去查。"},
		},
		sub:  []provider.Message{{Role: provider.RoleAssistant, Content: "Findings\nAugust\n\nSources\ngo.dev\n\nNot found\nnone"}},
		hold: make(chan struct{}),
	}
	k, b, box := dispatchKernel(t, m)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "Go 1.30 什么时候发布？", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	close(m.hold)
	ev := waitEvent(t, box, exec.EvDone)
	if !strings.Contains(ev.Content, "Sources") {
		t.Fatalf("event %q", ev.Content)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fore := toolNamesOf(m.foreReq[0].Tools)
	if !contains(fore, "dispatch") || contains(fore, "spawn") {
		t.Fatalf("foreground tools %v", fore)
	}
	sub := m.subReq[0]
	names := toolNamesOf(sub.Tools)
	for _, banned := range []string{"write_file", "edit_file", "dispatch", "spawn"} {
		if contains(names, banned) {
			t.Fatalf("research subagent has %s: %v", banned, names)
		}
	}
	if !contains(names, "shell") || !contains(names, "read_file") {
		t.Fatalf("research subagent tools %v", names)
	}
	sys := sub.Messages[0].Content
	for _, want := range []string{"You are a researcher.", "Findings, Sources, Not found"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt lacks %q: %q", want, sys)
		}
	}
	if got := sub.Messages[1].Content; got != "find the release date of Go 1.30" {
		t.Fatalf("task %q", got)
	}
	metas, _ := filepath.Glob(filepath.Join(b.Root, "bots", b.Name(), "agents", "sa_*", "meta.json"))
	if len(metas) != 1 {
		t.Fatalf("meta files %v", metas)
	}
	raw, _ := os.ReadFile(metas[0])
	var meta exec.Meta
	_ = json.Unmarshal(raw, &meta)
	if meta.Type != "research" {
		t.Fatalf("meta %+v", meta)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestDispatchRejectsUnknownType(t *testing.T) {
	k, _, _ := dispatchKernel(t, &roleModel{})
	if _, err := k.dispatch(context.Background(), "painter", "draw"); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("err %v", err)
	}
	if _, err := k.dispatch(context.Background(), "dev", "  "); err == nil {
		t.Fatal("empty task accepted")
	}
}

// A bot's own type file adds a type the foreground can pick.
func TestDispatchOffersTheBotsOwnTypes(t *testing.T) {
	k, b, _ := dispatchKernel(t, &roleModel{})
	if err := os.MkdirAll(b.AgentTypesDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AgentTypesDir(), "writer.md"), []byte("---\nwhen: draft long prose\ntools: read_file, write_file\n---\nWrite plainly.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := dispatchTool{k}
	if !strings.Contains(d.Description(), "writer: draft long prose") {
		t.Fatalf("description %q", d.Description())
	}
	got := k.toolsFor(exec.StartRequest{ID: "sa_0123456789", Type: "writer"})
	var names []string
	for _, t := range got {
		names = append(names, t.Name())
	}
	if len(names) != 2 || !contains(names, "read_file") || !contains(names, "write_file") {
		t.Fatalf("writer tools %v", names)
	}
}

func TestReadOnlyShellRefusesWrites(t *testing.T) {
	dir := t.TempDir()
	sh := tools.NewShell()
	sh.Dir = dir
	ro := readOnlyShell{sh}
	for _, cmd := range []string{"echo hi > f.txt", "rm -rf x", "ls; rm x", "touch f"} {
		args, _ := json.Marshal(map[string]any{"command": cmd})
		if _, err := ro.Run(context.Background(), args); err == nil {
			t.Fatalf("%q ran", cmd)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err == nil {
		t.Fatal("the refused write happened")
	}
	args, _ := json.Marshal(map[string]any{"command": "ls"})
	if _, err := ro.Run(context.Background(), args); err != nil {
		t.Fatalf("ls refused: %v", err)
	}
}

// A subagent can run for hours, so its requests are kept under the lines the
// same way a turn's are.
func TestSubagentRequestsAreFit(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal), Budget: compact.Budget{Ceiling: 6000, KeepTail: 1}}
	inner := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	p := k.wrapSub(exec.StartRequest{}, inner, 2)
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "task"}}
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs, assistantCall(id, "step", "read_file", `{}`), toolResult(id, "read_file", strings.Repeat("x", 8000)))
	}
	if _, err := p.Chat(context.Background(), provider.Request{Messages: msgs}, nil); err != nil {
		t.Fatal(err)
	}
	sent := inner.reqs[0].Messages
	if n := compact.EstimateMessages(sent); n > 6000 {
		t.Fatalf("sent %d tokens", n)
	}
	if !strings.Contains(sent[3].Content, "Read the file or run the call again") {
		t.Fatalf("note %q", sent[3].Content)
	}
}

// The dispatching foreground cannot do the work: a file edit outside memory
// and a shell command that writes are refused, a plain read runs, and the
// tools that only do work are not offered at all.
func TestForegroundCannotDoTheWork(t *testing.T) {
	m := &roleModel{}
	k, b, _ := dispatchKernel(t, m)
	k.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	memFile := filepath.Join(b.Root, "bots", b.Name(), "memory", "notes.md")
	m.fore = []provider.Message{
		toolCall("w1", "write_file", mustArgs(t, map[string]string{"path": filepath.Join(k.WorkDir, "a.txt"), "content": "x"})),
		toolCall("s1", "shell", mustArgs(t, map[string]any{"command": "echo x > b.txt"})),
		toolCall("s2", "shell", mustArgs(t, map[string]any{"command": "ls"})),
		toolCall("m1", "write_file", mustArgs(t, map[string]string{"path": memFile, "content": "- likes tea\n"})),
		{Role: provider.RoleAssistant, Content: "done"},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	got := bodies(t, k)
	if len(got) != 4 {
		t.Fatalf("results %q", got)
	}
	for i := 0; i < 2; i++ {
		if !strings.Contains(got[i], "hand it to a subagent with dispatch") {
			t.Fatalf("call %d ran: %q", i, got[i])
		}
	}
	if strings.Contains(got[2], "dispatch") {
		t.Fatalf("a plain read was refused: %q", got[2])
	}
	if strings.Contains(got[3], "dispatch") {
		t.Fatalf("a memory edit was refused: %q", got[3])
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(k.WorkDir, f)); err == nil {
			t.Fatalf("%s was written", f)
		}
	}
	if raw, err := os.ReadFile(memFile); err != nil || !strings.Contains(string(raw), "likes tea") {
		t.Fatalf("memory not written: %v %q", err, raw)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	names := toolNamesOf(m.foreReq[0].Tools)
	for _, hidden := range []string{"spawn", "http_request", "code_start", "write_skill"} {
		if contains(names, hidden) {
			t.Fatalf("foreground offers %s", hidden)
		}
	}
	if sys := m.foreReq[0].Messages[0].Content; !strings.Contains(sys, DispatchRule) {
		t.Fatal("the foreground's rules lack the dispatch rule")
	}
}

// The owner is never shown a card for a call the foreground refuses: with
// every command asking (machine "ask"), a write is refused before a card,
// and a chain of reads is let through to its card.
func TestForegroundRefusesBeforeTheCard(t *testing.T) {
	m := &roleModel{}
	k, _, _ := dispatchKernel(t, m)
	k.Machine = func() (string, error) { return "ask", nil }
	var asked []string
	k.Approve = func(_ context.Context, tool, summary string) (bool, error) {
		asked = append(asked, summary)
		return true, nil
	}
	m.fore = []provider.Message{
		toolCall("s1", "shell", mustArgs(t, map[string]any{"command": "echo x > b.txt"})),
		toolCall("s2", "shell", mustArgs(t, map[string]any{"command": "pwd && ls -la"})),
		{Role: provider.RoleAssistant, Content: "done"},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	got := bodies(t, k)
	if len(got) != 2 || !strings.Contains(got[0], "hand it to a subagent with dispatch") || strings.Contains(got[1], "dispatch") {
		t.Fatalf("results %q", got)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "pwd && ls -la") {
		t.Fatalf("cards %q; want only the chain of reads", asked)
	}
}

// Without dispatch the foreground keeps every tool and no dispatch rule.
func TestForegroundUnchangedWithoutDispatch(t *testing.T) {
	m := &roleModel{fore: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k, _, _ := dispatchKernel(t, m)
	k.Dispatch = false
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	names := toolNamesOf(m.foreReq[0].Tools)
	if !contains(names, "spawn") || contains(names, "dispatch") {
		t.Fatalf("tools %v", names)
	}
	if strings.Contains(m.foreReq[0].Messages[0].Content, DispatchRule) {
		t.Fatal("dispatch rule without dispatch")
	}
}

// At the foreground limit the model is told to hand the rest out and is left
// with only dispatch and the controls; past the grace calls the run stops.
func TestForegroundLimitHandsTheRestOut(t *testing.T) {
	read := func(id string) provider.Message {
		return toolCall(id, "shell", mustArgs(t, map[string]any{"command": "ls"}))
	}
	m := &roleModel{fore: []provider.Message{
		read("r1"), read("r2"), read("r3"), read("r4"), read("r5"), read("r6"),
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	k, b, _ := dispatchKernel(t, m)
	k.ForegroundMaxTools = 2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	reqs := append([]provider.Request(nil), m.foreReq...)
	m.mu.Unlock()
	if len(reqs) < 3 {
		t.Fatalf("requests %d", len(reqs))
	}
	capped := reqs[2]
	if !strings.Contains(messagesText(capped.Messages), "foreground's step limit") {
		t.Fatal("no limit note after the cap")
	}
	for _, n := range toolNamesOf(capped.Tools) {
		if !capTools[n] {
			t.Fatalf("after the cap the foreground still has %s", n)
		}
	}
	trs, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	for _, tr := range trs {
		if tr.Kind == bot.TraceStop && tr.Reason == StopForegroundLimit {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("the foreground kept working past the grace calls")
	}
}

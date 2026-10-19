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

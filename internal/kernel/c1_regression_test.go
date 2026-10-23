package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// This file uses only the pre-C1 public kernel/tool entry points, so the old
// implementation must compile and fail the behavioral deadline assertions.
type c1Link struct {
	tool tools.Tool
	more []tools.Tool
}

func (c1Link) Name() string          { return "c1-tools" }
func (l c1Link) Tools() []tools.Tool { return append([]tools.Tool{l.tool}, l.more...) }

type c1Slow struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (*c1Slow) Name() string        { return "c1_lookup" }
func (*c1Slow) Description() string { return "Look up a result" }
func (*c1Slow) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
}
func (*c1Slow) Confirm(json.RawMessage) (bool, string, error) { return false, "look up", nil }
func (*c1Slow) ReadOnlyHint() bool                            { return true }
func (s *c1Slow) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	if s.calls.Add(1) == 1 {
		close(s.entered)
	}
	select {
	case <-s.release:
		return "C1_ACCEPT_2381", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type c1Fetcher struct{ s *c1Slow }

func (f c1Fetcher) Fetch(ctx context.Context, _ string) (tools.Page, error) {
	text, err := f.s.Run(ctx, nil)
	return tools.Page{Text: text}, err
}

func c1Fixture(t *testing.T, tool tools.Tool) (*Kernel, *scripted) {
	t.Helper()
	b := testBot(t, link.NameTerminal, "c1-tools")
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{}
	k := &Kernel{Bot: b, Inbox: in, Provider: p, Memory: memory.NopStore{}, Permissive: true, WorkDir: t.TempDir(), MaxSteps: 4, Links: []link.Link{link.Terminal{}, c1Link{tool: tool}}}
	return k, p
}

func TestC1AllSlowToolsYieldAndReport(t *testing.T) {
	for _, kind := range []string{"web_fetch", "web_search", "arbitrary"} {
		t.Run(kind, func(t *testing.T) {
			slow := &c1Slow{entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			var handles sync.WaitGroup
			var tool tools.Tool = slow
			args := "{}"
			switch kind {
			case "web_fetch":
				tool = &tools.WebFetch{Fetcher: c1Fetcher{slow}}
				args = `{"url":"https://example.org/test"}`
			case "web_search":
				tool = &tools.WebSearch{Backend: tools.FuncSearch(func(ctx context.Context, _ string, _ int) ([]tools.Hit, error) {
					text, err := slow.Run(ctx, nil)
					return []tools.Hit{{Title: text, URL: "https://example.org/test", Snippet: text}}, err
				})}
				args = `{"query":"c1"}`
			}
			k, p := c1Fixture(t, tool)
			defer func() { once.Do(func() { close(slow.release) }); handles.Wait(); _ = k.Close() }()
			p.resps = []provider.Message{toolCall("c1", tool.Name(), args), {Role: provider.RoleAssistant, Content: "已收到，查完告诉你"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			start := time.Now()
			errc := make(chan error, 1)
			handles.Add(1)
			go func() { defer handles.Done(); errc <- k.Handle(ctx, link.TerminalEvent("u1", "lookup", time.Now())) }()
			select {
			case err := <-errc:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2900 * time.Millisecond):
				t.Fatal("slow tool blocked foreground beyond the common 2s wait")
			}
			t.Logf("%s foreground wait: %s", kind, time.Since(start))
			k.turn.mu.Lock()
			calls := k.turn.tools
			k.turn.mu.Unlock()
			if calls != 1 {
				t.Fatalf("background receipt lost foreground call count: %d", calls)
			}
			rows, err := k.Bot.Entries()
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, row := range rows {
				if row.Message.ToolCallID == "c1" {
					count++
					if !strings.HasPrefix(row.Message.Content, "background sh_") {
						t.Fatalf("not a background receipt: %s", row.Message.Content)
					}
				}
			}
			if count != 1 {
				t.Fatalf("tool result rows=%d", count)
			}
			cancel() // the first HTTP/turn context ending must not kill the call
			once.Do(func() { close(slow.release) })
			deadline := time.Now().Add(time.Second)
			for len(k.Inbox.Pending()) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			pending := k.Inbox.Pending()
			if len(pending) != 1 || !strings.Contains(pending[0].Content, "C1_ACCEPT_2381") {
				t.Fatalf("completion: %+v", pending)
			}
			if slow.calls.Load() != 1 {
				t.Fatalf("ran %d times", slow.calls.Load())
			}
			if pending[0].Type != "shell_job" {
				t.Fatalf("new message type: %s", pending[0].Type)
			}
		})
	}
}

func TestC1InterjectionYieldsRegisteredOrUpcomingCall(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "before-registration"}[before], func(t *testing.T) {
			slow := &c1Slow{entered: make(chan struct{}), release: make(chan struct{})}
			k, p := c1Fixture(t, slow)
			var release sync.Once
			var handles sync.WaitGroup
			defer func() { release.Do(func() { close(slow.release) }); handles.Wait(); _ = k.Close() }()
			p.resps = []provider.Message{toolCall("slow", slow.Name(), "{}"), {Role: provider.RoleAssistant, Content: "原任务和新话都收到了"}}
			toolStart := make(chan struct{})
			register := make(chan struct{})
			var reg sync.Once
			defer reg.Do(func() { close(register) })
			if before {
				k.OnTool = func(string, string) { close(toolStart); <-register }
			}
			errC := make(chan error, 1)
			handles.Add(1)
			go func() {
				defer handles.Done()
				errC <- k.Handle(context.Background(), link.TerminalEvent("u1", "first", time.Now()))
			}()
			if before {
				<-toolStart
			} else {
				<-slow.entered
			}
			start := time.Now()
			if !Interject(k.Bot, link.TerminalEvent("u1", "second", time.Now()), &Sink{Done: make(chan Outcome, 1)}) {
				t.Fatal("no live turn")
			}
			reg.Do(func() { close(register) })
			select {
			case err := <-errC:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("owner waited for running tool")
			}
			t.Logf("interjection to reply: %s", time.Since(start))
			rows, err := k.Bot.Entries()
			if err != nil {
				t.Fatal(err)
			}
			inputs, results := 0, 0
			for _, row := range rows {
				if row.Message.Content == "second" {
					inputs++
				}
				if row.Message.ToolCallID == "slow" {
					results++
				}
			}
			if inputs != 1 || results != 1 {
				t.Fatalf("inputs=%d results=%d", inputs, results)
			}
			release.Do(func() { close(slow.release) })
		})
	}
}

// A delayed wrapper over the real file tool exercises the existing validation
// and rollback, rather than returning a fake success without touching the file.
type c1MemoryWrite struct {
	*tools.WriteFile
	entered, release chan struct{}
}

func (w c1MemoryWrite) Run(ctx context.Context, args json.RawMessage) (string, error) {
	close(w.entered)
	<-w.release
	return w.WriteFile.Run(ctx, args)
}
func TestC1MemoryWriteDoesNotBlockNewInput(t *testing.T) {
	b := testBot(t, link.NameTerminal, "c1-tools")
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "memory.md")
	old := "- (2026-10-23) stable\n"
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	w := c1MemoryWrite{WriteFile: &tools.WriteFile{}, entered: make(chan struct{}), release: make(chan struct{})}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{toolCall("read", "read_file", mustArgs(t, map[string]string{"path": path})), toolCall("write", "write_file", mustArgs(t, map[string]string{"path": path, "content": "INVALID_MEMORY_LINE"})), {Role: provider.RoleAssistant, Content: "新话收到了"}}}
	k := &Kernel{Bot: b, Inbox: in, Provider: p, Memory: memory.Open(b.Root), Permissive: true, WorkDir: t.TempDir(), MaxSteps: 5, Links: []link.Link{link.Terminal{}, c1Link{tool: w}, freshReadLink{}}}
	var once sync.Once
	var handles sync.WaitGroup
	defer func() { once.Do(func() { close(w.release) }); handles.Wait(); _ = k.Close() }()
	done := make(chan error, 1)
	handles.Add(1)
	go func() {
		defer handles.Done()
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "write", time.Now()))
	}()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("write did not start")
	}
	start := time.Now()
	accepted := make(chan bool, 1)
	go func() {
		accepted <- Interject(b, link.TerminalEvent("u1", "new question", time.Now()), &Sink{Done: make(chan Outcome, 1)})
	}()
	select {
	case ok := <-accepted:
		if !ok {
			t.Fatal("interjection not accepted")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("input blocked on memory write lock")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request assembly blocked on memory write")
	}
	t.Logf("memory-write interjection reply: %s", time.Since(start))
	once.Do(func() { close(w.release) })
	end := time.Now().Add(time.Second)
	for len(in.Pending()) == 0 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != old {
		t.Fatalf("rollback failed: %q", raw)
	}
	if facts := k.turn.notable.Items(); len(facts) > 0 {
		t.Fatalf("rejected write committed facts: %+v", facts)
	}

	if len(in.Pending()) != 1 || !strings.Contains(in.Pending()[0].Content, "error:") {
		t.Fatalf("failed completion: %+v", in.Pending())
	}
}

func TestC1BackgroundMemoryWriteDoesNotBlockNextTurn(t *testing.T) {
	b := testBot(t, link.NameTerminal, "c1-tools")
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "memory.md")
	if err := os.WriteFile(path, []byte("- (2026-10-23) stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := c1MemoryWrite{WriteFile: &tools.WriteFile{}, entered: make(chan struct{}), release: make(chan struct{})}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		toolCall("read", "read_file", mustArgs(t, map[string]string{"path": path})),
		toolCall("write", "write_file", mustArgs(t, map[string]string{"path": path, "content": "INVALID_MEMORY_LINE"})),
		{Role: provider.RoleAssistant, Content: "写入在后台继续"},
		{Role: provider.RoleAssistant, Content: "下一句话已收到"},
	}}
	k := &Kernel{Bot: b, Inbox: in, Provider: p, Memory: memory.Open(b.Root), Permissive: true, WorkDir: t.TempDir(), MaxSteps: 5, Links: []link.Link{link.Terminal{}, c1Link{tool: w}, freshReadLink{}}}
	var once sync.Once
	var handles sync.WaitGroup
	defer func() { once.Do(func() { close(w.release) }); handles.Wait(); _ = k.Close() }()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "first", time.Now())); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	handles.Add(1)
	go func() {
		defer handles.Done()
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "next", time.Now()))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("next turn blocked on background memory writer")
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	refreshed := make(chan error, 1)
	handles.Add(1)
	go func() { defer handles.Done(); refreshed <- k.refreshMemSnap(context.Background(), &st) }()
	select {
	case err := <-refreshed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("compaction snapshot waited on background memory writer")
	}
	if !strings.Contains(st.MemSnap, "stable") || strings.Contains(st.MemSnap, "INVALID") {
		t.Fatal("prefix lost committed memory")
	}
}

type c1Fast struct{ *c1Slow }

func (*c1Fast) Name() string { return "c1_fast" }
func TestC1MixedBatchKeepsOneOrderedResultPerCall(t *testing.T) {
	a := &c1Slow{entered: make(chan struct{}), release: make(chan struct{})}
	b := &c1Slow{entered: make(chan struct{}), release: make(chan struct{})}
	fast := &c1Fast{&c1Slow{entered: make(chan struct{}), release: make(chan struct{})}}
	close(fast.release)
	tool := &tools.WebFetch{Fetcher: c1Fetcher{a}}
	search := &tools.WebSearch{Backend: tools.FuncSearch(func(ctx context.Context, _ string, _ int) ([]tools.Hit, error) {
		s, e := b.Run(ctx, nil)
		return []tools.Hit{{Title: s, URL: "https://example.org/b", Snippet: s}}, e
	})}
	k, p := c1Fixture(t, fast)
	k.Links = []link.Link{link.Terminal{}, c1Link{tool: fast, more: []tools.Tool{tool, search}}}
	var once sync.Once
	var handles sync.WaitGroup
	defer func() { once.Do(func() { close(a.release); close(b.release) }); handles.Wait(); _ = k.Close() }()
	m := toolCall("fast", fast.Name(), "{}")
	m.ToolCalls = append(m.ToolCalls, toolCall("fetch", "web_fetch", `{"url":"https://example.org/a"}`).ToolCalls...)
	m.ToolCalls = append(m.ToolCalls, toolCall("search", "web_search", `{"query":"batch"}`).ToolCalls...)
	p.resps = []provider.Message{m, {Role: provider.RoleAssistant, Content: "三项都收到了"}}
	done := make(chan error, 1)
	handles.Add(1)
	go func() {
		defer handles.Done()
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "batch", time.Now()))
	}()
	<-a.entered
	<-b.entered
	if !Interject(k.Bot, link.TerminalEvent("u1", "new", time.Now()), &Sink{Done: make(chan Outcome, 1)}) {
		t.Fatal("not accepted")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("batch did not yield")
	}
	rows, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range rows {
		if row.Message.Role == provider.RoleTool {
			ids = append(ids, row.Message.ToolCallID)
		}
	}
	if strings.Join(ids, ",") != "fast,fetch,search" {
		t.Fatalf("results reordered or duplicated: %v", ids)
	}
	if len(k.Backgrounds()) != 0 || k.execSnapshot() != nil {
		t.Fatal("tool job consumed a subagent/fork slot")
	}
	once.Do(func() { close(a.release); close(b.release) })
	end := time.Now().Add(time.Second)
	for len(k.Inbox.Pending()) < 2 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	if len(k.Inbox.Pending()) != 2 || a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatalf("completion=%+v calls=%d/%d", k.Inbox.Pending(), a.calls.Load(), b.calls.Load())
	}
}

// Keep the read fixture separate from tools link defaults so only the named
// calls above run; no network or real provider is involved.
type freshReadLink struct{}

func (freshReadLink) Name() string        { return "c1-read" }
func (freshReadLink) Tools() []tools.Tool { return []tools.Tool{&tools.ReadFile{}} }

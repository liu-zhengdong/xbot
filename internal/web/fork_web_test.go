package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type slowLink struct{ t *slowTool }

func (slowLink) Name() string          { return "slow" }
func (l slowLink) Tools() []tools.Tool { return []tools.Tool{l.t} }

type slowTool struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*slowTool) Name() string        { return "slow" }
func (*slowTool) Description() string { return "a long job" }
func (*slowTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*slowTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "slow", nil
}
func (s *slowTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return "job done", nil
}

// forkModel answers the interrupt in the foreground and runs the long job in the turn it forks from.
type forkModel struct{}

func (forkModel) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	last := req.Messages[len(req.Messages)-1]
	var lastUser string
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			lastUser = m.Content
		}
	}
	switch {
	case strings.Contains(lastUser, "插一句"):
		if onDelta != nil {
			onDelta("前台回答")
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "前台回答"}, nil
	case last.Role == provider.RoleTool:
		return provider.Message{Role: provider.RoleAssistant, Content: "后台完成"}, nil
	default:
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "slow", Arguments: "{}"},
		}}}, nil
	}
}

func TestWebInterruptForksAndAnswersFast(t *testing.T) {
	holdPoll = 20 * time.Millisecond
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", ForkTurns: true}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	slow := &slowTool{started: make(chan struct{}), release: make(chan struct{})}
	var k *kernel.Kernel
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		k = &kernel.Kernel{Bot: b, Provider: forkModel{}, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
			Links: []link.Link{slowLink{slow}}}
		return k, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	first := make(chan map[string]string, 1)
	go func() {
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"做个长任务"}`)
		first <- parseSSE(t, res.Body)
	}()
	select {
	case <-slow.started:
	case <-time.After(5 * time.Second):
		t.Fatal("long job never started")
	}
	start := time.Now()
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	took := time.Since(start)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("interrupt %d %s", res.StatusCode, res.Body)
	}
	ev := parseSSE(t, res.Body)
	if took > 3*time.Second || !strings.Contains(ev["done"], "前台回答") {
		t.Fatalf("interrupt took %v, events %v", took, ev)
	}
	var f map[string]string
	select {
	case f = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not end after the fork")
	}
	if !strings.Contains(f["done"], `"background":"bg_`) {
		t.Fatalf("first done %v", f)
	}
	if _, ok, _ := b.TryLockHandle(); ok {
		t.Fatal("handle lock released while a background is alive")
	}
	close(slow.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(k.Backgrounds()) > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(k.Backgrounds()) != 0 {
		t.Fatal("background never finished")
	}
	var lock interface{ Release() error }
	for time.Now().Before(deadline) {
		l, ok, _ := b.TryLockHandle()
		if ok {
			lock = l
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lock == nil {
		t.Fatal("handle lock never released after the background ended")
	}
	lock.Release()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var sawEnd, sawBG bool
	for _, e := range entries {
		if e.Mark != nil && e.Mark.Kind == bot.ForkKindEnd && e.Mark.Status == "done" {
			sawEnd = true
		}
		if strings.Contains(e.Message.Content, "interrupted before the tool returned") {
			t.Fatal("the foreground repaired a call the background still had in flight")
		}
		if e.Branch != "" && e.Message.Content == "后台完成" {
			sawBG = true
		}
	}
	if !sawEnd || !sawBG {
		t.Fatalf("fork_end %v background reply %v", sawEnd, sawBG)
	}
}

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// interjectModel answers the owner's interjection as soon as it is in the
// request. Before that it runs the slow tool once, and its plain answer to
// the first message waits on hold when hold is set.
type interjectModel struct {
	hold    chan struct{}
	holding chan struct{}
}

func (m interjectModel) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser && strings.Contains(msg.Content, "插一句") {
			if onDelta != nil {
				onDelta("答插话")
			}
			return provider.Message{Role: provider.RoleAssistant, Content: "答插话"}, nil
		}
	}
	if m.hold != nil {
		close(m.holding)
		select {
		case <-m.hold:
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
		if onDelta != nil {
			onDelta("第一件答完")
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "第一件答完"}, nil
	}
	if req.Messages[len(req.Messages)-1].Role == provider.RoleTool {
		return provider.Message{Role: provider.RoleAssistant, Content: "第一件答完"}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "slow", Arguments: "{}"},
	}}}, nil
}

func interjectServer(t *testing.T, model provider.Provider, list ...tools.Tool) (*httptest.Server, *http.Cookie) {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", PollInterval: 20 * time.Millisecond}
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
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		k := &kernel.Kernel{Bot: b, Provider: model, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4}
		if len(list) > 0 {
			k.Links = []link.Link{slowLink{list[0].(*slowTool)}}
		}
		return k, nil
	}
	ui := httptest.NewServer(muxOf(s))
	t.Cleanup(ui.Close)
	return ui, loginCookie(t, ui.URL, tok)
}

// An interjection sent while a turn runs must reach that turn at its next
// step and come back on its own request, not as a 202 "queued" left for a
// later serve pass.
func TestInterjectionIsReadByTheRunningTurn(t *testing.T) {
	slow := &slowTool{started: make(chan struct{}), release: make(chan struct{})}
	ui, cookie := interjectServer(t, interjectModel{}, slow)

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
	second := make(chan httpResult, 1)
	go func() {
		second <- postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	}()
	time.Sleep(100 * time.Millisecond)
	close(slow.release)
	var res httpResult
	select {
	case res = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("interjection never answered")
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("interjection %d %s", res.StatusCode, res.Body)
	}
	if ev := parseSSE(t, res.Body); !strings.Contains(ev["done"], "答插话") {
		t.Fatalf("interjection events %v", ev)
	}
	select {
	case f := <-first:
		if !strings.Contains(f["done"], "答插话") {
			t.Fatalf("running turn did not read the interjection: %v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request never ended")
	}
}

// When the running turn is already writing its last answer, it never reads
// the interjection. The waiting request answers it right after, instead of
// leaving it in the inbox.
func TestInterjectionAfterTheLastStepIsAnsweredAtOnce(t *testing.T) {
	m := interjectModel{hold: make(chan struct{}), holding: make(chan struct{})}
	ui, cookie := interjectServer(t, m)

	first := make(chan map[string]string, 1)
	go func() {
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"第一件"}`)
		first <- parseSSE(t, res.Body)
	}()
	select {
	case <-m.holding:
	case <-time.After(5 * time.Second):
		t.Fatal("first answer never started")
	}
	second := make(chan httpResult, 1)
	go func() {
		second <- postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	}()
	time.Sleep(100 * time.Millisecond)
	close(m.hold)
	select {
	case f := <-first:
		if !strings.Contains(f["done"], "第一件答完") {
			t.Fatalf("first events %v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request never ended")
	}
	select {
	case res := <-second:
		if res.StatusCode != http.StatusOK {
			t.Fatalf("interjection %d %s", res.StatusCode, res.Body)
		}
		if ev := parseSSE(t, res.Body); !strings.Contains(ev["done"], "答插话") {
			t.Fatalf("interjection events %v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interjection never answered")
	}
}

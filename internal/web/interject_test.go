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
		if msg.Role == provider.RoleUser && strings.Contains(msg.Text(), "插一句") {
			if onDelta != nil {
				onDelta("答插话")
			}
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("答插话")}, nil
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
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第一件答完")}, nil
	}
	if req.Messages[len(req.Messages)-1].Role == provider.RoleTool {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第一件答完")}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "slow", Arguments: "{}"},
	}})}, nil
}

func interjectServer(t *testing.T, model provider.Provider, list ...tools.Tool) (*httptest.Server, *http.Cookie, *bot.Bot) {
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
	return ui, loginCookie(t, ui.URL, tok), b
}

// An interjection sent while a tool runs goes straight into that turn, not
// the inbox: the turn reads it as soon as the tool returns, and the answer
// streams back on the interjecting request. The first request hands off.
func TestInterjectionGoesStraightIntoTheTurn(t *testing.T) {
	slow := &slowTool{started: make(chan struct{}), release: make(chan struct{})}
	ui, cookie, b := interjectServer(t, interjectModel{}, slow)

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
	select {
	case f := <-first:
		if !strings.Contains(f["done"], `"handed_off":true`) {
			t.Fatalf("first request did not hand off: %v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request never handed off")
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(box.Pending()); n != 0 {
		t.Fatalf("interjection went through the inbox: %d pending", n)
	}
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
	// The owner is persisted while the tool is still running. Its result lands
	// afterwards; the protocol adapter pairs it without moving transcript rows.
	var order []string
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch {
		case e.Message.Role == provider.RoleTool:
			order = append(order, "tool")
		case strings.Contains(e.Message.Text(), "插一句"):
			order = append(order, "owner")
		case strings.Contains(e.Message.Text(), "<owner_interjected>") && e.IsInternal():
			order = append(order, "note")
		case e.Message.Text() == "答插话":
			order = append(order, "answer")
		}
	}
	if strings.Join(order, ",") != "owner,tool,note,answer" {
		t.Fatalf("transcript order %v", order)
	}
}

// While the model is answering, an interjection stops that call at once and
// sends it again with the owner's words: no waiting for it to finish.
func TestInterjectionStopsTheModelCall(t *testing.T) {
	m := interjectModel{hold: make(chan struct{}), holding: make(chan struct{})}
	ui, cookie, b := interjectServer(t, m)
	defer close(m.hold)

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
	start := time.Now()
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("interjection waited %v for the model call", took)
	}
	if ev := parseSSE(t, res.Body); res.StatusCode != http.StatusOK || !strings.Contains(ev["done"], "答插话") {
		t.Fatalf("interjection %d %v", res.StatusCode, ev)
	}
	select {
	case f := <-first:
		if !strings.Contains(f["done"], `"handed_off":true`) {
			t.Fatalf("first events %v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first request never ended")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Message.Text() == "第一件答完" {
			t.Fatal("the stopped call's answer was recorded")
		}
	}
}

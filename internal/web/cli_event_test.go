package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/provider"
)

type cliEventModel struct {
	messages []provider.Message
	notify   bool
	calls    int
}

func (m *cliEventModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	m.messages = append(m.messages, req.Messages...)
	m.calls++
	if m.notify && m.calls == 1 {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "say", Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: `{"text":"visible report"}`}}})}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}, nil
}
func TestCLIEventWake(t *testing.T) {
	s, ui, cookie, b := questionServer(t, time.Second)
	model := &cliEventModel{}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{Bot: b, Inbox: box, Provider: model, MaxSteps: 4}, nil
	}
	req, _ := http.NewRequest("POST", ui.URL+"/api/bots/ada/events", strings.NewReader(`{"sender":"u1","text":"external ping"}`))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(raw), "event: accepted") || !strings.Contains(string(raw), "event: done") || strings.Contains(string(raw), "event: error") {
		t.Fatalf("status %d body %s", res.StatusCode, raw)
	}
	rawLog, err := os.ReadFile(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	var posted link.Event
	for _, line := range strings.Split(string(rawLog), "\n") {
		var record struct {
			Op    string      `json:"op"`
			Event *link.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Op == "post" && record.Event != nil {
			posted = *record.Event
		}
	}
	if posted.ID == "" || posted.Source != "cli" || posted.Sender != "u1" || !posted.Untrusted || posted.Content != "external ping" {
		t.Fatalf("posted event %+v", posted)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(box.Pending()) != 0 {
		t.Fatal("event remains pending")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "external ping") {
			found = true
			if e.Origin != bot.OriginInternal || !strings.Contains(e.Message.Text(), `<untrusted_event source="cli" type="event" sender="u1"`) {
				t.Fatalf("entry %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("missing event")
	}
	found = false
	for _, m := range model.messages {
		if strings.Contains(m.Text(), `<untrusted_event source="cli"`) && strings.Contains(m.Text(), "external ping") {
			found = true
		}
	}
	if !found {
		t.Fatal("model missed untrusted event")
	}
	if link.OwnerTrusted("cli", "u1", []string{"u1"}) {
		t.Fatal("CLI is trusted")
	}
	req, _ = http.NewRequest("POST", ui.URL+"/api/bots/ada/events", strings.NewReader(`{"sender":"codex","text":"x"}`))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status %d", res.StatusCode)
	}
}

func TestCLIEventVisibleNotification(t *testing.T) {
	s, ui, cookie, _ := questionServer(t, time.Second)
	model := &cliEventModel{notify: true}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{Bot: b, Inbox: box, Provider: model, MaxSteps: 4, Desktop: func(context.Context, string, string) error { return nil }}, nil
	}
	req, _ := http.NewRequest("POST", ui.URL+"/api/bots/ada/events", strings.NewReader(`{"sender":"codex","text":"ping"}`))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(raw), `"text":"visible report"`) || !strings.Contains(string(raw), "event: done") {
		t.Fatalf("response %s", raw)
	}
}

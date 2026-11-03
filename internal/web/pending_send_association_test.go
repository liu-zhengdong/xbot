package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
)

func TestPendingSendHTTPIdentity(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b := mustWebBot(t, s.Root, "ada", "u1")
	defer bot.CloseStore(b)
	s.NewKernel = func(b *bot.Bot, _ *kernel.BotResources) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: &turnProvider{resps: []provider.Message{
			{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")},
			{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")},
			{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")},
		}}, Memory: memory.NopStore{}, MaxSteps: 2}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	for _, id := range []string{"web_" + strings.Repeat("1", 32), "web_" + strings.Repeat("2", 32), ""} {
		payload := map[string]string{"text": "same words"}
		if id != "" {
			payload["client_id"] = id
		}
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", ui.URL+"/api/bots/ada/messages", strings.NewReader(string(raw)))
		req.AddCookie(cookie)
		res, err := ui.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || !strings.Contains(string(body), "event: done") {
			t.Fatalf("send %q: %d %s", id, res.StatusCode, body)
		}
		eventID := res.Header.Get("X-Xbot-Event-ID")
		if eventID == "" || id != "" && eventID != id {
			t.Fatalf("wrong association header: %q want %q", eventID, id)
		}
		entries, err := b.Entries()
		if err != nil {
			t.Fatal(err)
		}
		var found int
		for _, e := range entries {
			if e.EventID == eventID && e.Message.Role == provider.RoleUser {
				found++
			}
		}
		var chat bot.ChatWindow
		if err := json.Unmarshal(getAuth(t, ui.URL+"/api/bots/ada/chat", cookie), &chat); err != nil {
			t.Fatal(err)
		}
		matches := 0
		for _, m := range chat.Messages {
			if m.Kind == "owner" && m.Meta.EventID == eventID {
				matches++
			}
		}
		if found != 1 || matches != 1 {
			t.Fatalf("association not preserved: transcript=%d chat=%d id=%q", found, matches, eventID)
		}
	}
	before, _ := b.Count()
	for _, id := range []string{"web_" + strings.Repeat("a", 31), "web_" + strings.Repeat("a", 33), "web_" + strings.Repeat("A", 32), "ev_" + strings.Repeat("a", 32), "web_" + strings.Repeat("g", 32)} {
		raw, _ := json.Marshal(map[string]string{"text": "reject identity", "client_id": id})
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, string(raw))
		if res.StatusCode != 400 {
			t.Fatalf("invalid id accepted: %q %d", id, res.StatusCode)
		}
	}
	after, _ := b.Count()
	if after != before {
		t.Fatalf("invalid identity wrote transcript: %d -> %d", before, after)
	}
}

func TestPendingSendInterjectionIdentity(t *testing.T) {
	model := interjectModel{hold: make(chan struct{}), holding: make(chan struct{})}
	ui, cookie, _ := interjectServer(t, model)
	defer close(model.hold)
	firstID, secondID := "web_"+strings.Repeat("4", 32), "web_"+strings.Repeat("5", 32)
	first := make(chan httpResult, 1)
	go func() {
		first <- postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"wait for interjection","client_id":"`+firstID+`"}`)
	}()
	select {
	case <-model.holding:
	case <-time.After(3 * time.Second):
		t.Fatal("first call did not start")
	}
	response := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：identity","client_id":"`+secondID+`"}`)
	if response.StatusCode != 200 || !strings.Contains(parseSSE(t, response.Body)["done"], "答插话") {
		t.Fatalf("interjection failed: %d %s", response.StatusCode, response.Body)
	}
	select {
	case response = <-first:
		if !strings.Contains(parseSSE(t, response.Body)["done"], `"handed_off":true`) {
			t.Fatal("first stream did not hand off")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first stream did not finish")
	}
	var window bot.ChatWindow
	if err := json.Unmarshal(getAuth(t, ui.URL+"/api/bots/ada/chat", cookie), &window); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range window.Messages {
		if row.Kind == "owner" {
			counts[row.Meta.EventID]++
		}
	}
	if counts[firstID] != 1 || counts[secondID] != 1 {
		t.Fatalf("interjection lost identity: %v", counts)
	}
}

func TestPendingSendQueuedIdentity(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1", PollInterval: time.Millisecond}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b := mustWebBot(t, s.Root, "ada", "u1")
	defer bot.CloseStore(b)
	lock, ok, err := b.TryLockHandle()
	if err != nil || !ok {
		t.Fatalf("hold: %v %v", ok, err)
	}
	defer lock.Release()
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	id := "web_" + strings.Repeat("3", 32)
	req, _ := http.NewRequestWithContext(ctx, "POST", ui.URL+"/api/bots/ada/messages", strings.NewReader(`{"text":"queued input","client_id":"`+id+`"}`))
	req.AddCookie(cookie)
	res, err := ui.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("X-Xbot-Event-ID") != id {
		t.Fatalf("queue headers: %d %v", res.StatusCode, res.Header)
	}
	scanner := bufio.NewScanner(res.Body)
	event, data := "", ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
		if line == "" {
			break
		}
	}
	if event != "queued" || !strings.Contains(data, id) {
		t.Fatalf("missing queued identity: %q %q err=%v", event, data, scanner.Err())
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 1 || pending[0].ID != id || pending[0].Content != "queued input" {
		t.Fatalf("receipt did not reflect actual inbox: %+v", pending)
	}
	cancel()
}

func TestAbsorbedReplyUsesExactEventIdentity(t *testing.T) {
	for _, scene := range []string{"same_words", "empty_image", "missing_identity"} {
		t.Run(scene, func(t *testing.T) {
			b := mustWebBot(t, t.TempDir(), "ada", "u1")
			defer bot.CloseStore(b)
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			id := "target-event"
			text := "same words"
			if scene == "empty_image" {
				text = ""
			}
			ev := link.WebEvent("u1", text, time.Now())
			ev.ID = id
			if scene == "empty_image" {
				ev.Images = []string{"picture.png"}
			}
			if err := box.Post(ev); err != nil {
				t.Fatal(err)
			}
			if err := box.Ack(id); err != nil {
				t.Fatal(err)
			}
			appendRow := func(e bot.Entry) {
				t.Helper()
				if err := b.Append(e); err != nil {
					t.Fatal(err)
				}
			}
			if scene != "missing_identity" {
				appendRow(bot.Entry{EventID: id, Images: ev.Images, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
				appendRow(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("target answer")}})
			}
			appendRow(bot.Entry{EventID: "different-event", Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("same words")}})
			appendRow(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("different answer")}})
			s := &Server{}
			absorbed, reply, background, failed := s.absorbedReply(b, nil, box, id)
			if !absorbed || background != "" {
				t.Fatalf("unexpected result: %v %q", absorbed, background)
			}
			if scene == "missing_identity" {
				if !failed || reply != "" {
					t.Fatalf("borrowed unrelated input: %q failed=%v", reply, failed)
				}
			} else if failed || reply != "target answer" {
				t.Fatalf("wrong input answer: %q failed=%v", reply, failed)
			}
		})
	}
}

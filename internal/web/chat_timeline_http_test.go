package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/pipe/limits"
	"xbot/internal/provider"
)

func TestChatTimelineNewSayThroughHTTPKernel(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "reader"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(s.Root, "ada", "", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"reader"}); err != nil {
		t.Fatal(err)
	}
	model := &turnProvider{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{
			{ID: "one", Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: `{"text":"first delivery"}`}},
			{ID: "two", Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: `{"text":"second delivery"}`}},
		})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("hidden final prose")},
	}}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: model, Memory: memory.NopStore{}, MaxSteps: 4}, nil
	}
	server := httptest.NewServer(muxOf(s))
	defer server.Close()
	cookie := loginCookie(t, server.URL, token)
	response := postJSON(t, server.URL+"/api/bots/ada/messages", cookie, `{"text":"owner input"}`)
	events := parseSSE(t, response.Body)
	if events["done"] == "" || events["error"] != "" {
		t.Fatalf("round did not finish: %s", response.Body)
	}
	var window bot.ChatWindow
	if err := json.Unmarshal(getAuth(t, server.URL+"/api/bots/ada/chat", cookie), &window); err != nil {
		t.Fatal(err)
	}
	if len(window.Messages) != 3 || window.Messages[0].Text != "owner input" || window.Messages[1].Text != "first delivery" || window.Messages[2].Text != "second delivery" || window.Messages[1].DeliveryKey != "say:one" || window.Messages[2].DeliveryKey != "say:two" {
		t.Fatalf("new delivery missing at chat seam: %+v", window.Messages)
	}
	raw := getAuth(t, server.URL+"/api/bots/ada/transcript", cookie)
	if !strings.Contains(string(raw), "hidden final prose") || !strings.Contains(string(raw), "tool_calls") {
		t.Fatal("trace ledger was replaced by chat")
	}
}

func TestChatTimelineHTTP(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "reader"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "history", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"reader"}); err != nil {
		t.Fatal(err)
	}
	appendEntry := func(role, text string) {
		t.Helper()
		if err := b.Append(bot.Entry{Time: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), Message: provider.Message{Role: role, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 95; i++ {
		appendEntry(provider.RoleUser, fmt.Sprintf("visible record %03d 聊天线索", i))
		appendEntry(provider.RoleTool, "hiddenneedle "+strings.Repeat("noise ", 700))
	}
	h := muxOf(s)
	get := func(path, tag string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/bots/history/"+path, nil)
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) bot.ChatWindow {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
		}
		var out bot.ChatWindow
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	start := time.Now()
	first := get("chat", "", true)
	tail := decode(first)
	t.Logf("chat tail HTTP %s bytes=%d", time.Since(start), first.Body.Len())
	if len(tail.Messages) != 40 || tail.Window.Total != 95 || tail.Window.HeadID != 56 || tail.Messages[0].TranscriptSeq != 111 {
		t.Fatalf("not visible-row bounded: %+v", tail)
	}
	older := decode(get("chat?before=56&limit=12", "", true))
	if len(older.Messages) != 12 || older.Window.TailID != 55 || !older.Window.HasMoreAfter {
		t.Fatalf("before: %+v", older.Window)
	}
	after := decode(get("chat?after=55", "", true))
	if after.Window.HeadID != 56 || after.Window.HasMoreAfter {
		t.Fatalf("after: %+v", after.Window)
	}
	around := decode(get("chat?around=7&limit=5", "", true))
	if around.Window.HeadID > 7 || around.Window.TailID < 7 {
		t.Fatal("around missed target")
	}
	tag := first.Header().Get("ETag")
	if tag == "" || get("chat", tag, true).Code != 304 || get("chat?before=56", tag, true).Code != 200 {
		t.Fatal("chat validators lost range identity")
	}
	appendEntry(provider.RoleTool, "only tools changed")
	if get("chat", tag, true).Code != 304 {
		t.Fatal("tool noise invalidated chat")
	}
	appendEntry(provider.RoleUser, "new visible delivery")
	if next := get("chat", tag, true); next.Code != 200 || next.Header().Get("ETag") == tag {
		t.Fatal("chat delivery did not invalidate")
	}
	for _, q := range []string{"?before=2&after=3", "?around=0", "?limit=201", "?limit=2&limit=3"} {
		if get("chat"+q, "", true).Code != 400 {
			t.Fatal("accepted invalid cursor", q)
		}
	}
	if get("chat?around=999", "", true).Code != 404 || get("chat", "", false).Code != 401 {
		t.Fatal("target/auth status")
	}
	// A huge owner line advances one chat ID and retains the pipe bound.
	appendEntry(provider.RoleUser, strings.Repeat("超长记录<&>", 20000))
	big := get("chat?limit=200", "", true)
	projected := decode(big)
	if big.Body.Len() > limits.MaxBody || len(projected.Messages) == 0 || !strings.Contains(big.Body.String(), `"truncated":true`) || projected.Window.TailID != 97 {
		t.Fatalf("oversize window: bytes=%d %+v", big.Body.Len(), projected.Window)
	}
	for i := 0; i < 40; i++ {
		appendEntry(provider.RoleUser, strings.Repeat("plain ", 6000))
	}
	cappedResponse := get("chat?limit=200", "", true)
	capped := decode(cappedResponse)
	if cappedResponse.Body.Len() > limits.MaxBody || len(capped.Messages) >= 40 || capped.Window.TailID != 137 || !capped.Window.HasMoreBefore || capped.Window.NextBefore != capped.Messages[0].ID {
		t.Fatalf("aggregate pipe cap/cursor: bytes=%d rows=%d %+v", cappedResponse.Body.Len(), len(capped.Messages), capped.Window)
	}
	var hits struct {
		Matches []bot.ChatHit `json:"matches"`
	}
	for _, query := range []string{"hiddenneedle", "线索"} {
		response := get("chat/search?q="+query, "", true)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &hits); err != nil {
			t.Fatal(err)
		}
		if query == "hiddenneedle" && len(hits.Matches) != 0 || query == "线索" && len(hits.Matches) != 20 {
			t.Fatalf("chat FTS %s: %+v", query, hits)
		}
	}
	// Agent history still sees the tool original; it was not moved into chat.
	raw := get("transcript/search?q=hiddenneedle", "", true)
	if raw.Code != 200 || !strings.Contains(raw.Body.String(), "hiddenneedle") {
		t.Fatal("transcript search changed responsibility")
	}
	if err := b.SetOwners([]string{"another"}); err != nil {
		t.Fatal(err)
	}
	if get("chat", "", true).Code != 404 || get("chat/search?q=visible", "", true).Code != 404 {
		t.Fatal("non-owner can read chat")
	}
}

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

type questionModel struct{}

func (questionModel) Chat(_ context.Context, req provider.Request, delta func(string)) (provider.Message, error) {
	last := req.Messages[len(req.Messages)-1]
	if last.Role == provider.RoleUser && last.Content == "ASK_TIMEOUT" {
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "ask", Type: "function", Function: provider.FunctionCall{Name: "ask_user", Arguments: `{"question":"先做哪一步？","options":["先搭骨架","先写文档"],"recommended":"先搭骨架"}`}}}}, nil
	}
	text := "继续：" + last.Content
	if last.Role == provider.RoleTool {
		text = "等你选择。"
	}
	if delta != nil {
		delta(text)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: text}, nil
}
func questionServer(t *testing.T, wait time.Duration) (*Server, *httptest.Server, *http.Cookie, *bot.Bot) {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", ApprovalWait: wait}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{Bot: b, Provider: questionModel{}, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4, Budget: compact.DefaultBudget(), Links: []link.Link{link.Web{}}}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	t.Cleanup(func() { kernel.AnswerQuestions(b, "结束测试"); kernel.SetWaker(root, nil); ui.Close() })
	cookie := loginCookie(t, ui.URL, tok)
	return s, ui, cookie, b
}
func questionCard(t *testing.T, b *bot.Bot) bot.ApprovalCard {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var card bot.ApprovalCard
	for _, e := range entries {
		if c, ok := bot.ParseApprovalCard(e.Message.Content); ok && c.Tool == "ask_user" {
			card = c
		}
	}
	if card.ID == "" {
		t.Fatal("question card missing")
	}
	return card
}
func questionCode(t *testing.T, ui *httptest.Server, cookie *http.Cookie, id, choice string) int {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"choice": choice})
	req, _ := http.NewRequest("POST", ui.URL+"/api/bots/ada/questions/"+id, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}
func TestQuestionTimeoutChoosesRecommendedAndResumes(t *testing.T) {
	_, ui, cookie, b := questionServer(t, 200*time.Millisecond)
	for ev := range postSSE(t, ui.URL, cookie, "ASK_TIMEOUT") {
		if ev.name == "error" {
			t.Fatal(ev.data)
		}
	}
	waitTranscript(t, b, "继续：先搭骨架")
	entriesForShot, _ := b.Entries()
	for _, entry := range entriesForShot {
		if card, ok := bot.ParseApprovalCard(entry.Message.Content); ok && card.Tool == "ask_user" {
			raw, _ := json.Marshal(card)
			t.Logf("CARD_SNAPSHOT %s", raw)
		}
	}
	c := questionCard(t, b)
	if c.State != bot.ApprovalAutoApproved || !strings.Contains(c.Notice, "没人选，按推荐选了 先搭骨架") {
		t.Fatalf("card %+v", c)
	}
	var args struct {
		Choice string `json:"choice"`
	}
	if err := json.Unmarshal([]byte(c.Summary), &args); err != nil {
		t.Fatal(err)
	}
	if args.Choice != "先搭骨架" {
		t.Fatalf("choice %q", args.Choice)
	}
	// A later click is a fresh owner message, while the timeout record stays.
	if code := questionCode(t, ui, cookie, c.ID, "先写文档"); code != 200 {
		t.Fatalf("late click %d", code)
	}
	waitTranscript(t, b, "继续：先写文档")
	if got := questionCard(t, b); got.State != bot.ApprovalAutoApproved {
		t.Fatalf("timeout record overwritten: %+v", got)
	}
}
func TestQuestionOwnerChoosesBeforeDeadline(t *testing.T) {
	_, ui, cookie, b := questionServer(t, 400*time.Millisecond)
	for range postSSE(t, ui.URL, cookie, "ASK_TIMEOUT") {
	}
	c := questionCard(t, b)
	if code := questionCode(t, ui, nil, c.ID, "先写文档"); code != 401 {
		t.Fatalf("unauthenticated %d", code)
	}
	if code := questionCode(t, ui, cookie, "missing", "先写文档"); code != 404 {
		t.Fatalf("missing %d", code)
	}
	if code := questionCode(t, ui, cookie, c.ID, "other"); code != 400 {
		t.Fatalf("invalid choice %d", code)
	}
	if code := questionCode(t, ui, cookie, c.ID, "先写文档"); code != 200 {
		t.Fatalf("choose %d", code)
	}
	waitTranscript(t, b, "继续：先写文档")
	if code := questionCode(t, ui, cookie, c.ID, "先搭骨架"); code != 409 {
		t.Fatalf("duplicate %d", code)
	}
	time.Sleep(450 * time.Millisecond)
	if got := questionCard(t, b); got.State != bot.ApprovalApproved || got.Notice != "" || !strings.Contains(got.Summary, "先写文档") {
		t.Fatalf("manual choice overwritten %+v", got)
	}
	entries, _ := b.Entries()
	for _, e := range entries {
		if e.Message.Content == "继续：先搭骨架" {
			t.Fatal("timer resumed after owner chose")
		}
	}
}
func TestQuestionTypedReplyStopsTimer(t *testing.T) {
	_, ui, cookie, b := questionServer(t, 200*time.Millisecond)
	for range postSSE(t, ui.URL, cookie, "ASK_TIMEOUT") {
	}
	for range postSSE(t, ui.URL, cookie, "先检查测试") {
	}
	time.Sleep(250 * time.Millisecond)
	if got := questionCard(t, b); got.State != bot.ApprovalApproved || got.Notice != "" {
		t.Fatalf("typed reply %+v", got)
	}
}

package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestLinksApprovalCarriesSecretNames(t *testing.T) {
	const secret = "super-secret-value"
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &turnProvider{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "links", Arguments: `{"action":"install","id":"telegram"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("stopped")},
	}}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: p, MaxSteps: 4, Links: []link.Link{link.Terminal{}}}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	stream := postSSE(t, ui.URL, cookie, "install telegram")
	ev := waitEvent(t, stream, "approval")
	if strings.Contains(ev.data, secret) {
		t.Fatal(ev.data)
	}
	if !strings.Contains(ev.data, `"secrets":["TELEGRAM_BOT_TOKEN"]`) || !strings.Contains(ev.data, `"tool":"links"`) {
		t.Fatal(ev.data)
	}
	var card struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	decideBody(t, ui.URL, cookie, card.ID, `{"allow":true,"secrets":{"TELEGRAM_BOT_TOKEN":"`+secret+`"}}`)
	var blob strings.Builder
	for ev := range stream {
		blob.WriteString(ev.name)
		blob.WriteByte(' ')
		blob.WriteString(ev.data)
		blob.WriteByte('\n')
		if ev.name == "error" {
			t.Fatal(ev.data)
		}
		if ev.name == "done" {
			break
		}
	}
	if strings.Contains(blob.String(), secret) {
		t.Fatal(blob.String())
	}
	if !strings.Contains(blob.String(), "installed telegram") {
		t.Fatal(blob.String())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, e := range entries {
		joined.WriteString(e.Message.Text())
		joined.WriteByte('\n')
	}
	if strings.Contains(joined.String(), secret) {
		t.Fatal(joined.String())
	}
	raw, err := os.ReadFile(b.SecretPath())
	if err != nil || !strings.Contains(string(raw), secret) {
		t.Fatalf("secret file: %v %s", err, raw)
	}
	loaded, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Grants(link.NameTelegram) {
		t.Fatal("telegram was not granted")
	}
}

type turnProvider struct {
	resps []provider.Message
	i     int
}

func (s *turnProvider) Chat(_ context.Context, _ provider.Request, onDelta func(string)) (provider.Message, error) {
	if s.i >= len(s.resps) {
		return provider.Message{}, errors.New("no more responses")
	}
	m := s.resps[s.i]
	s.i++
	if onDelta != nil && m.Text() != "" {
		onDelta(m.Text())
	}
	return m, nil
}

func decideBody(t *testing.T, base string, cookie *http.Cookie, id, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/api/bots/ada/approvals/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("decide %d %s", res.StatusCode, raw)
	}
}

package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestTranscriptForkFields(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := b.Append(bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("start")}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("forked work")}, Branch: "bg_7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 1, "long job"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("after marker")}, Branch: "bg_7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd("bg_7", bot.ForkDone, 2); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bots/ada/transcript", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var out struct {
		Messages []apiRow `json:"messages"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 5 {
		t.Fatalf("rows %+v", out.Messages)
	}
	if out.Messages[0].Branch != "" {
		t.Fatalf("main row branch %q", out.Messages[0].Branch)
	}
	if out.Messages[1].Branch != "bg_7" || out.Messages[3].Branch != "bg_7" {
		t.Fatalf("branch rows %+v", out.Messages)
	}
	start := out.Messages[2]
	if start.Kind != bot.ForkKindStart || start.From != 1 || start.Task != "long job" || start.Branch != "bg_7" {
		t.Fatalf("fork marker %+v", start)
	}
	end := out.Messages[4]
	if end.Kind != bot.ForkKindEnd || end.Status != bot.ForkDone || end.Steps != 2 || end.Branch != "bg_7" {
		t.Fatalf("fork end marker %+v", end)
	}
}

package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestLinkCatalogInstall(t *testing.T) {
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
	if _, err := bot.Create(root, "eve", "persona", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Load(root, "eve")
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	call := func(method, path, body string, auth bool) (int, []byte) {
		t.Helper()
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rdr)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, raw
	}

	code, raw := call(http.MethodGet, "/api/links", "", false)
	if code != http.StatusUnauthorized {
		t.Fatalf("auth %d", code)
	}
	code, raw = call(http.MethodGet, "/api/links", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"id":"webhook"`) || !strings.Contains(string(raw), `"id":"coding-agent"`) || strings.Contains(string(raw), `"installed"`) {
		t.Fatalf("catalog %d %s", code, raw)
	}
	if !strings.Contains(string(raw), `"available":false`) || !strings.Contains(string(raw), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("unavailable %s", raw)
	}

	code, raw = call(http.MethodGet, "/api/bots/eve/links", "", true)
	if code != http.StatusNotFound {
		t.Fatalf("eve %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/links", `{"id":"telegram"}`, true)
	if code != http.StatusConflict || !strings.Contains(string(raw), "secrets required") {
		t.Fatalf("telegram %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/links", `{"id":"mcp"}`, true)
	if code != http.StatusConflict || !strings.Contains(string(raw), "not available yet") {
		t.Fatalf("mcp %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/links", `{"id":"missing"}`, true)
	if code != http.StatusNotFound {
		t.Fatalf("missing %d %s", code, raw)
	}
	b, _ = bot.Load(root, "ada")
	if len(b.Config().Links) != 1 {
		t.Fatalf("links changed %v", b.Config().Links)
	}

	code, raw = call(http.MethodPost, "/api/bots/ada/links", `{"id":"webhook"}`, true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"installed":true`) {
		t.Fatalf("install %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/links", `{"id":"webhook"}`, true)
	if code != http.StatusOK {
		t.Fatalf("second %d %s", code, raw)
	}
	b, _ = bot.Load(root, "ada")
	if !hasLink(b, link.NameWebhook) || len(b.Config().Links) != 2 {
		t.Fatalf("granted %v", b.Config().Links)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/links", "", true)
	if !strings.Contains(string(raw), `"id":"webhook"`) || !strings.Contains(string(raw), `"installed":true`) || !strings.Contains(string(raw), `"installed":false`) {
		t.Fatalf("bot links %d %s", code, raw)
	}

	code, raw = call(http.MethodDelete, "/api/bots/ada/links/webhook", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"installed":false`) {
		t.Fatalf("remove %d %s", code, raw)
	}
	b, _ = bot.Load(root, "ada")
	if hasLink(b, link.NameWebhook) {
		t.Fatalf("still granted %v", b.Config().Links)
	}
	code, raw = call(http.MethodDelete, "/api/bots/ada/links/nope", "", true)
	if code != http.StatusNotFound {
		t.Fatalf("remove missing %d %s", code, raw)
	}
}

func hasLink(b *bot.Bot, name string) bool {
	for _, l := range b.Config().Links {
		if l == name {
			return true
		}
	}
	return false
}

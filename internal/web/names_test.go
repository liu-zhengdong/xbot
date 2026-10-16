package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestWebBotNameUnicodeAndCase(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	// A Unicode name is stored exactly as written and routes through the API.
	res := postJSON(t, ui.URL+"/api/bots", cookie, `{"name":"小助手"}`)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(res.Body), `"name":"小助手"`) {
		t.Fatalf("create %d %s", res.StatusCode, res.Body)
	}
	tr := string(getAuth(t, ui.URL+"/api/bots/"+url.PathEscape("小助手")+"/transcript", cookie))
	if !strings.Contains(tr, `"messages":[]`) {
		t.Fatalf("transcript %s", tr)
	}

	// Separators, whitespace-only, control runes, and over-long names are rejected.
	bad := []string{
		`{"name":"a/b"}`,
		`{"name":"a\\b"}`,
		`{"name":"   "}`,
		`{"name":""}`,
		`{"name":"a\nb"}`,
		`{"name":"` + strings.Repeat("巨", 65) + `"}`,
	}
	for _, body := range bad {
		res := postJSON(t, ui.URL+"/api/bots", cookie, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s: %d %s", body, res.StatusCode, res.Body)
		}
	}

	// The case a name was written in survives create, list, and rename.
	res = postJSON(t, ui.URL+"/api/bots", cookie, `{"name":"xBoT"}`)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(res.Body), `"name":"xBoT"`) {
		t.Fatalf("create %d %s", res.StatusCode, res.Body)
	}
	list := string(getAuth(t, ui.URL+"/api/bots", cookie))
	if !strings.Contains(list, `"name":"xBoT"`) || strings.Contains(list, `"name":"Xbot"`) {
		t.Fatalf("list folded the case: %s", list)
	}
	res = patchJSON(t, ui.URL+"/api/bots/xBoT", cookie, `{"name":"Bot复刻"}`)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(res.Body), `"name":"Bot复刻"`) {
		t.Fatalf("rename %d %s", res.StatusCode, res.Body)
	}
	list = string(getAuth(t, ui.URL+"/api/bots", cookie))
	if strings.Contains(list, `"name":"xBoT"`) || !strings.Contains(list, `"name":"Bot复刻"`) {
		t.Fatalf("rename did not stick: %s", list)
	}
	if _, err := bot.Load(root, "Bot复刻"); err != nil {
		t.Fatal(err)
	}

	// The rename endpoint accepts the same names as create: a path separator is
	// rejected and the bot keeps its previous name.
	res = patchJSON(t, ui.URL+"/api/bots/Bot复刻", cookie, `{"name":"../evil"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal rename %d %s", res.StatusCode, res.Body)
	}
	if _, err := bot.Load(root, "Bot复刻"); err != nil {
		t.Fatalf("rename damaged the bot: %v", err)
	}
}

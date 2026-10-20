package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/work"
)

// TestOpenWorkdir pins the desktop-only endpoint: browsers get 404, the app
// passes the bot's work directory to the shell, and a failure is an error.
func TestOpenWorkdir(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", App: true}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "p", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	wantDir, _, err := work.Open(root, "ada")
	if err != nil {
		t.Fatal(err)
	}

	var opened []string
	s.OpenPath = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()

	call := func(cookie *http.Cookie, path string) int {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
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
	cookie := &http.Cookie{Name: cookieName, Value: tok}

	if code := call(nil, "/api/bots/ada/open-workdir"); code != http.StatusUnauthorized {
		t.Fatalf("without login: %d, want 401", code)
	}
	if code := call(cookie, "/api/bots/nobody/open-workdir"); code != http.StatusNotFound {
		t.Fatalf("unknown bot: %d, want 404", code)
	}
	if code := call(cookie, "/api/bots/ada/open-workdir"); code != http.StatusOK {
		t.Fatalf("app: %d, want 200", code)
	}
	if len(opened) != 1 || opened[0] != wantDir {
		t.Fatalf("opened %v, want [%s]", opened, wantDir)
	}

	s.App = false
	if code := call(cookie, "/api/bots/ada/open-workdir"); code != http.StatusNotFound {
		t.Fatalf("plain serve: %d, want 404 even with a callback", code)
	}

	s.App = true
	s.OpenPath = func(string) error { return errors.New("no file manager") }
	if code := call(cookie, "/api/bots/ada/open-workdir"); code != http.StatusInternalServerError {
		t.Fatalf("callback failure: %d, want 500", code)
	}
}

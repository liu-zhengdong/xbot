package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/version"
	"xbot/internal/work"
)

func TestGeneratedWorkdir(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"u1", "u2"} {
		b, err := bot.Create(s.Root, owner, "p", []string{link.NameWeb})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{owner}); err != nil {
			t.Fatal(err)
		}
	}
	mux := muxOf(s)
	call := func(method, path string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		path string
		auth bool
		want int
	}{
		{"/api/bots/u1/generated", false, 401},
		{"/api/bots/u2/generated", true, 404},
		{"/api/bots/missing/generated", true, 404},
		{"/api/bots/..%2Foutside/generated", true, 400},
	} {
		if w := call("GET", tc.path, tc.auth); w.Code != tc.want {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
	want := filepath.Join(work.BotDir(s.Root, "u1"), work.GeneratedDir)
	for i := 0; i < 2; i++ {
		w := call("GET", "/api/bots/u1/generated", true)
		var got struct {
			Path    string
			CanOpen bool `json:"can_open"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got.Path != want || got.CanOpen {
			t.Fatalf("browser path: %d %+v", w.Code, got)
		}
	}
	// The directory is the same destination used by hosted generation.
	image := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	rel, err := work.WriteGenerated(work.BotDir(s.Root, "u1"), image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(want, filepath.Base(rel))); err != nil {
		t.Fatal(err)
	}
	var opened string
	s.App = true
	s.OpenPath = func(path string) error { opened = path; return nil }
	if w := call("POST", "/api/bots/u1/open-workdir?folder=generated", true); w.Code != 200 || opened != want {
		t.Fatalf("desktop: %d %q", w.Code, opened)
	}
	for _, folder := range []string{"../outside", "/outside", "generated/.."} {
		opened = ""
		if w := call("POST", "/api/bots/u1/open-workdir?folder="+folder, true); w.Code != 400 || opened != "" {
			t.Fatalf("accepted folder %s: %d", folder, w.Code)
		}
	}
	// Refuse both escaped and internal symlinks before passing paths to the OS.
	if err := os.RemoveAll(want); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{t.TempDir(), work.BotDir(s.Root, "u1")} {
		if err := os.Symlink(target, want); err != nil {
			t.Fatal(err)
		}
		opened = ""
		if w := call("GET", "/api/bots/u1/generated", true); w.Code != 500 {
			t.Fatalf("symlink GET: %d", w.Code)
		}
		if w := call("POST", "/api/bots/u1/open-workdir?folder=generated", true); w.Code != 500 || opened != "" {
			t.Fatalf("symlink opened: %d %q", w.Code, opened)
		}
		if err := os.Remove(want); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionBuildTip(t *testing.T) {
	oldVersion, oldTip := version.Version, version.Tip
	t.Cleanup(func() { version.Version, version.Tip = oldVersion, oldTip })
	version.Version, version.Tip = "dev", "abc1234"
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/session", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	muxOf(s).ServeHTTP(w, r)
	var got struct{ Version, Tip string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.Version != "dev" || got.Tip != "abc1234" || version.Version != "dev" {
		t.Fatalf("session: %d %+v", w.Code, got)
	}
}

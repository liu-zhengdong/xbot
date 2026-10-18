package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postTheme posts a theme as the logged-in owner of a fresh server.
func postTheme(t *testing.T, app bool, theme string) (int, []string) {
	t.Helper()
	var got []string
	s := &Server{Root: t.TempDir(), Owner: "owner", App: app,
		AppTheme: func(v string) error { got = append(got, v); return nil }}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	code, err := s.NewLaunchCode()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/app/launch?code="+code, nil))
	cookie := w.Result().Cookies()[0]
	r := httptest.NewRequest("POST", "/api/app/theme", strings.NewReader(`{"theme":"`+theme+`"}`))
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, got
}

func TestAppThemeOnlyInTheDesktopApp(t *testing.T) {
	if code, got := postTheme(t, false, "dark"); code != http.StatusNotFound || len(got) != 0 {
		t.Fatalf("plain serve: %d %v, want 404 and no hook call", code, got)
	}
	if code, got := postTheme(t, true, "dark"); code != http.StatusNoContent || len(got) != 1 || got[0] != "dark" {
		t.Fatalf("app: %d %v, want 204 and the hook called with dark", code, got)
	}
	for _, theme := range []string{"light", "system"} {
		if code, got := postTheme(t, true, theme); code != http.StatusNoContent || len(got) != 1 || got[0] != theme {
			t.Fatalf("%s: %d %v", theme, code, got)
		}
	}
}

func TestAppThemeRejectsUnknownValues(t *testing.T) {
	for _, theme := range []string{"sepia", "Light", ""} {
		if code, got := postTheme(t, true, theme); code != http.StatusBadRequest || len(got) != 0 {
			t.Fatalf("%q: %d %v, want 400 and no hook call", theme, code, got)
		}
	}
	s := &Server{Root: t.TempDir(), Owner: "owner", App: true,
		AppTheme: func(string) error { return errors.New("shell unavailable") }}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	r := httptest.NewRequest("POST", "/api/app/theme", strings.NewReader(`{"theme":"dark"}`))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("hook error: %d, want 500", w.Code)
	}
}

// A nil hook (an app built without a window) still accepts a valid theme.
func TestAppThemeWithoutHook(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "owner", App: true}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	r := httptest.NewRequest("POST", "/api/app/theme", strings.NewReader(`{"theme":"light"}`))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("nil hook: %d, want 204", w.Code)
	}
}

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v0.3.1-beta.2":      "0.3.1-beta.2",
		"0.3.1-beta.2+abc12": "0.3.1-beta.2",
		"v1.0.0+meta":        "1.0.0",
		"dev":                "dev",
	} {
		if got := displayVersion(in); got != want {
			t.Errorf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

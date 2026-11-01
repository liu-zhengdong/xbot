package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDesktopAppKeepsItsSession pins issue #105 P2: on the old install-code
// path the desktop app's only way in is the launch code minted at start, so
// /api/logout refuses there and that path's UI never offers 退出. A plain serve
// still logs out. Accounts sign in again on their own, so they always may.
func TestDesktopAppKeepsItsSession(t *testing.T) {
	for _, app := range []bool{true, false} {
		s := &Server{Root: t.TempDir(), Owner: "owner", App: app}
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

		r := httptest.NewRequest("GET", "/api/session", nil)
		r.AddCookie(cookie)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if got := strings.Contains(w.Body.String(), `"app":true`); got != app {
			t.Fatalf("app=%v: session %s", app, w.Body.String())
		}

		r = httptest.NewRequest("POST", "/api/logout", nil)
		r.AddCookie(cookie)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		cleared := len(w.Result().Cookies()) > 0
		if app && (w.Code != http.StatusConflict || cleared) {
			t.Fatalf("app logout: %d cleared=%v", w.Code, cleared)
		}
		if !app && (w.Code != http.StatusOK || !cleared) {
			t.Fatalf("serve logout: %d cleared=%v", w.Code, cleared)
		}
	}

}

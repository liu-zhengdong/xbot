package web

import (
	"net/http"
	"net/http/httptest"
	"os"
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

	data, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		// The other #105 web items ride along in the same file.
		`if (state.sel && state.sel.type === "bot" && state.sel.id === name && !state.peer) return;`,
		`"xbot-draft:" + (selection.type === "group" ? "group:" : "") + selection.id`,
		`sessionStorage.getItem("xbot-draft:group:" + id)`,
		`(row.queued || row.keepFailed) && !queuedRecorded(row)`,
		`pending.baseline = ownerSends(text);`,
		`!unwrap(m.content || "").untrusted &&`,
		`state.replyErrors.forEach((row) => row.remove());`,
		`const stale = !row.accepted && row.querySelector(".send-failed") && row.dataset.sendText === text;`,
		`return "没能完成：" + msg;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing #105 fix: %s", want)
		}
	}
}

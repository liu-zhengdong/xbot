package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func appRestartTestMux(t *testing.T, app bool, restart func() error) (http.Handler, *http.Cookie) {
	t.Helper()
	s := &Server{Root: t.TempDir(), Owner: "owner", App: app, AppRestart: restart}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	return muxOf(s), &http.Cookie{Name: cookieName, Value: token}
}

func TestAppRestartAvailability(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  bool
		hook bool
		want bool
	}{
		{name: "desktop with callback", app: true, hook: true, want: true},
		{name: "desktop without callback", app: true},
		{name: "serve with callback", hook: true},
		{name: "serve without callback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var restart func() error
			if tc.hook {
				restart = func() error { calls++; return nil }
			}
			mux, cookie := appRestartTestMux(t, tc.app, restart)
			r := httptest.NewRequest(http.MethodGet, "/api/session", nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("session: %d %s", w.Code, w.Body)
			}
			var session struct {
				Authed     bool  `json:"authed"`
				App        bool  `json:"app"`
				CanRestart *bool `json:"can_restart"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
				t.Fatal(err)
			}
			if !session.Authed || session.App != tc.app || session.CanRestart == nil || *session.CanRestart != tc.want {
				t.Fatalf("session: %s, want authed=true, app=%t, can_restart=%t", w.Body, tc.app, tc.want)
			}
			if !tc.want {
				for _, authenticated := range []bool{true, false} {
					r := httptest.NewRequest(http.MethodPost, "/api/app/restart", nil)
					if authenticated {
						r.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					if w.Code != http.StatusNotFound {
						t.Errorf("unavailable restart (authenticated=%t): %d %s, want 404", authenticated, w.Code, w.Body)
					}
				}
			}
			if calls != 0 {
				t.Fatalf("session/unavailable route called restart %d times", calls)
			}
		})
	}
}

func TestAppRestartAccepted(t *testing.T) {
	calls := 0
	mux, cookie := appRestartTestMux(t, true, func() error { calls++; return nil })
	// The endpoint takes no arguments or JSON body: it only accepts the request.
	r := httptest.NewRequest(http.MethodPost, "/api/app/restart", nil)
	r.Header.Set("Origin", "http://"+r.Host)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("restart: %d %s, want 202", w.Code, w.Body)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "restarting" || calls != 1 {
		t.Fatalf("restart: status=%q, callback calls=%d, want restarting and exactly one call", out.Status, calls)
	}
}

func TestAppRestartCallbackFailure(t *testing.T) {
	calls := 0
	const reason = "desktop restart request unavailable"
	mux, cookie := appRestartTestMux(t, true, func() error {
		calls++
		return errors.New(reason)
	})
	r := httptest.NewRequest(http.MethodPost, "/api/app/restart", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("restart: %d %s, want 500", w.Code, w.Body)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Error, reason) || calls != 1 {
		t.Fatalf("restart: error=%q, callback calls=%d, want clear failure and exactly one call", out.Error, calls)
	}
}

func TestAppRestartRequiresOwnerAndSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cookie    string
		origin    string
		fetchSite string
		want      int
	}{
		{name: "no session", want: http.StatusUnauthorized},
		{name: "wrong session", cookie: "invalid", want: http.StatusUnauthorized},
		{name: "foreign origin", cookie: "owner", origin: "https://foreign.example", want: http.StatusForbidden},
		{name: "cross-site fetch", cookie: "owner", origin: "http://example.com", fetchSite: "cross-site", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			mux, cookie := appRestartTestMux(t, true, func() error { calls++; return nil })
			r := httptest.NewRequest(http.MethodPost, "http://example.com/api/app/restart", nil)
			switch tc.cookie {
			case "owner":
				r.AddCookie(cookie)
			case "invalid":
				r.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid"})
			}
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want || calls != 0 {
				t.Fatalf("restart: %d %s, callback calls=%d, want %d and no call", w.Code, w.Body, calls, tc.want)
			}
		})
	}
}

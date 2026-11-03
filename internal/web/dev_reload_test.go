package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func devReloadTestMux(t *testing.T, reload func() error) (http.Handler, *http.Cookie) {
	t.Helper()
	s := &Server{Root: t.TempDir(), Owner: "owner", DevReload: reload}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	return muxOf(s), &http.Cookie{Name: cookieName, Value: token}
}

// The button and route only exist on a dev instance: a nil callback means the
// session never advertises can_reload and the endpoint stays 404 for both
// authed and unauthed requests.
func TestDevReloadAvailability(t *testing.T) {
	for _, tc := range []struct {
		name string
		hook bool
		want bool
	}{
		{name: "dev instance", hook: true, want: true},
		{name: "production instance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var reload func() error
			if tc.hook {
				reload = func() error { calls++; return nil }
			}
			mux, cookie := devReloadTestMux(t, reload)
			r := httptest.NewRequest(http.MethodGet, "/api/session", nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("session: %d %s", w.Code, w.Body)
			}
			var session struct {
				Authed    bool  `json:"authed"`
				CanReload *bool `json:"can_reload"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
				t.Fatal(err)
			}
			if !session.Authed || session.CanReload == nil || *session.CanReload != tc.want {
				t.Fatalf("session: %s, want authed=true, can_reload=%t", w.Body, tc.want)
			}
			if !tc.want {
				for _, authenticated := range []bool{true, false} {
					r := httptest.NewRequest(http.MethodPost, "/api/dev/reload", nil)
					if authenticated {
						r.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					if w.Code != http.StatusNotFound {
						t.Errorf("unavailable reload (authenticated=%t): %d %s, want 404", authenticated, w.Code, w.Body)
					}
				}
			}
			if calls != 0 {
				t.Fatalf("session/unavailable route called reload %d times", calls)
			}
		})
	}
}

// A request that reaches the callback is accepted once: the handler only
// passes the answer through, the build itself runs after the response.
func TestDevReloadAccepted(t *testing.T) {
	calls := 0
	mux, cookie := devReloadTestMux(t, func() error { calls++; return nil })
	r := httptest.NewRequest(http.MethodPost, "/api/dev/reload", nil)
	r.Header.Set("Origin", "http://"+r.Host)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("reload: %d %s, want 202", w.Code, w.Body)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "reloading" || calls != 1 {
		t.Fatalf("reload: status=%q, callback calls=%d, want reloading and exactly one call", out.Status, calls)
	}
}

// A second request while a build is already running, or any callback failure,
// is a conflict the page can show and recover from.
func TestDevReloadConflict(t *testing.T) {
	calls := 0
	mux, cookie := devReloadTestMux(t, func() error {
		calls++
		return errors.New("已在编译")
	})
	r := httptest.NewRequest(http.MethodPost, "/api/dev/reload", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("reload: %d %s, want 409", w.Code, w.Body)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == "" || calls != 1 {
		t.Fatalf("reload: error=%q, callback calls=%d, want clear failure and exactly one call", out.Error, calls)
	}
}

func TestDevReloadRequiresOwnerAndSameOrigin(t *testing.T) {
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
			mux, cookie := devReloadTestMux(t, func() error { calls++; return nil })
			r := httptest.NewRequest(http.MethodPost, "http://example.com/api/dev/reload", nil)
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
				t.Fatalf("reload: %d %s, callback calls=%d, want %d and no call", w.Code, w.Body, calls, tc.want)
			}
		})
	}
}

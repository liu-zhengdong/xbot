package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLaunchLogin(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "owner"}
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
	if w.Code != 303 || w.Header().Get("Location") != "/" || len(w.Result().Cookies()) != 1 {
		t.Fatal("launch did not redirect with cookie")
	}
	r := httptest.NewRequest("GET", "/api/session", nil)
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"authed":true`) {
		t.Fatal("launch cookie did not authenticate")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/app/launch?code="+code, nil))
	if w.Code != 403 {
		t.Fatal("launch code reused")
	}
}

func TestLaunchExpired(t *testing.T) {
	now := time.Now()
	s := &Server{token: "initialized", launchNow: func() time.Time { return now }}
	code, err := s.NewLaunchCode()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	w := httptest.NewRecorder()
	s.launch(w, httptest.NewRequest("GET", "/app/launch?code="+code, nil))
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatal("expired launch accepted")
	}
}

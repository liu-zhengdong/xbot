package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalSessionBoundary(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, remote, host, forwarded, origin string
		want                                  bool
	}{
		{"loopback", "127.0.0.1:1234", "127.0.0.1:8788", "", "", true},
		{"localhost", "[::1]:1234", "localhost:8788", "", "", true},
		{"remote", "192.0.2.1:1234", "127.0.0.1:8788", "", "", false},
		{"proxy", "127.0.0.1:1234", "localhost:8788", "1.2.3.4", "", false},
		{"host", "127.0.0.1:1234", "evil.example:8788", "", "", false},
		{"origin", "127.0.0.1:1234", "localhost:8788", "", "https://evil.example", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/api/session", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			s.session(w, req)
			got := len(w.Result().Cookies()) > 0
			if got != tt.want {
				t.Fatalf("cookie issued: %v; body %s", got, w.Body.String())
			}
		})
	}
}

func TestLocalSessionWithoutAccountKeepsBotsAfterAccountLogout(t *testing.T) {
	e := newAcctEnv(t, false)
	c := e.browser()
	if _, session := e.do(c, "GET", "/api/session", nil, nil); session["authed"] != true {
		t.Fatalf("local session: %v", session)
	}
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != http.StatusOK {
		t.Fatalf("bots before login: %d", code)
	}
	if code, _ := e.do(c, "POST", "/api/account/register", map[string]string{"email": "local@example.com", "password": "password 12"}, nil); code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	if code, _ := e.do(c, "POST", "/api/account/logout", nil, nil); code != http.StatusOK {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != http.StatusOK {
		t.Fatalf("bots after logout: %d", code)
	}
}

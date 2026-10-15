package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestMCPAPI(t *testing.T) {
	t.Setenv("API_TOKEN", "super-secret-value")
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(root, "eve", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	call := func(method, path, body string, auth bool) (int, []byte) {
		t.Helper()
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rdr)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, raw
	}

	code, raw := call(http.MethodGet, "/api/bots/ada/mcp", "", false)
	if code != http.StatusUnauthorized {
		t.Fatalf("auth %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/eve/mcp", "", true)
	if code != http.StatusNotFound || !strings.Contains(string(raw), "not found") {
		t.Fatalf("eve %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/mcp", `{"name":"docs","command":"/bin/echo","token":"super-secret-value"}`, true)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), "bad request") {
		t.Fatalf("token %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/mcp", `{"name":"docs","command":"echo\nrm"}`, true)
	if code != http.StatusBadRequest {
		t.Fatalf("newline %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/mcp", `{"name":"docs","command":"/bin/echo","args":["--stdio"],"secret_envs":["API_TOKEN"]}`, true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"name":"docs"`) || strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("add %d %s", code, raw)
	}
	file, err := os.ReadFile(b.MCPPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "super-secret-value") || !strings.Contains(string(file), "API_TOKEN") {
		t.Fatalf("stored %s", file)
	}
	fi, err := os.Stat(b.MCPPath())
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/mcp", `{"name":"docs","command":"/bin/echo"}`, true)
	if code != http.StatusConflict || !strings.Contains(string(raw), "already exists") {
		t.Fatalf("dup %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/mcp", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"secret_envs":["API_TOKEN"]`) || strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("list %d %s", code, raw)
	}
	code, raw = call(http.MethodDelete, "/api/bots/ada/mcp/docs", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"name":"docs"`) {
		t.Fatalf("delete %d %s", code, raw)
	}
	code, raw = call(http.MethodDelete, "/api/bots/ada/mcp/docs", "", true)
	if code != http.StatusNotFound {
		t.Fatalf("delete again %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/mcp", "", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"servers":[]`) {
		t.Fatalf("empty %d %s", code, raw)
	}
}

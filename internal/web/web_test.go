package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestTokenFileMode(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, created, err := s.EnsureToken()
	if err != nil || !created || tok == "" {
		t.Fatalf("token %q created %v err %v", tok, created, err)
	}
	fi, err := os.Stat(filepath.Join(root, "web-token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	again, created, err := s.EnsureToken()
	if err != nil || created || again != tok {
		t.Fatalf("reload %q created %v err %v", again, created, err)
	}
}

func TestTranscriptEscapesHTML(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{
		Time:    time.Now(),
		Message: provider.Message{Role: provider.RoleUser, Content: `<script>alert(1)</script>`},
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bots/ada/transcript", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if bytes.Contains(body, []byte("<script>")) {
		t.Fatalf("raw html in json: %s", body)
	}
	if !bytes.Contains(body, []byte(`\u003cscript\u003e`)) {
		t.Fatalf("expected escaped script: %s", body)
	}
}

func TestRejectsCrossOriginPost(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", res.StatusCode)
	}
	var payload map[string]string
	_ = json.NewDecoder(res.Body).Decode(&payload)
	if payload["error"] != "cross-origin" {
		t.Fatalf("%v", payload)
	}
}

func muxOf(s *Server) http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

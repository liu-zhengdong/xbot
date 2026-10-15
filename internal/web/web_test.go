package web

import (
	"bytes"
	"context"
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
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
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

func TestForkBotAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NamePeers, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: "SECRET-DECISION"}}); err != nil {
		t.Fatal(err)
	}
	mem := memory.Open(root)
	s.NewKernel = func(bot *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: bot, Memory: mem, Compactor: handoffCompactor{text: "UI-HANDOFF keep the friday decision"}}, nil
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	body := bytes.NewBufferString(`{"name":"reviewer","role":"Review pull requests."}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/bots/ada/fork", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	child, err := bot.Load(root, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := child.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %d %v", len(entries), err)
	}
	facts, err := mem.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: "reviewer", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, f := range facts {
		joined.WriteString(f.Text)
		joined.WriteByte('\n')
	}
	if !strings.Contains(joined.String(), "UI-HANDOFF") || strings.Contains(joined.String(), "SECRET-DECISION") {
		t.Fatalf("memory %s", joined.String())
	}
	persona, err := child.ReadPersona()
	if err != nil || strings.TrimSpace(persona) != "Review pull requests." {
		t.Fatalf("persona %q %v", persona, err)
	}
}

type handoffCompactor struct{ text string }

func (h handoffCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	return h.text, nil
}

func TestBotListPinsPrimaryFirst(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ada", "bob", "cleo"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameWeb})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{"u1"}); err != nil {
			t.Fatal(err)
		}
		if name == "cleo" {
			if err := b.SetPinned(true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := bot.SetPrimary(root, "bob"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bots", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var payload struct {
		Bots []botItem `json:"bots"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range payload.Bots {
		names = append(names, b.Name)
		if b.Name == "bob" && !b.Primary {
			t.Fatal("bob is not marked primary")
		}
		if b.Name == "ada" && (b.Primary || b.Pinned) {
			t.Fatal("ada should be neither")
		}
	}
	if strings.Join(names, ",") != "bob,cleo,ada" {
		t.Fatalf("order %s", names)
	}
}

func muxOf(s *Server) http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

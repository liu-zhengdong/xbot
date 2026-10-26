package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
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
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(`<script>alert(1)</script>`)},
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
	if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("SECRET-DECISION")}}); err != nil {
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

func TestProfileAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "Be brief.", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	client := srv.Client()
	do := func(method, path string, body io.Reader, contentType string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, field := range []string{"tone", "length", "emoji", "language", "quiet_start", "quiet_end"} {
		res := do(http.MethodPatch, "/api/bots/ada", strings.NewReader(`{"`+field+`":"old"}`), "application/json")
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("removed %s accepted: %d", field, res.StatusCode)
		}
	}
	patch := `{"description":"项目笔记","proactivity":"quiet"}`
	res := do(http.MethodPatch, "/api/bots/ada", strings.NewReader(patch), "application/json")
	if res.StatusCode != 200 {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("patch %d %s", res.StatusCode, raw)
	}
	res.Body.Close()
	res = do(http.MethodGet, "/api/bots/ada", nil, "")
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	wantModel := config.FromEnv(os.Getenv).Model
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"description":"项目笔记"`)) || bytes.Contains(raw, []byte(`"proactivity"`)) || bytes.Contains(raw, []byte(`"quiet_start"`)) || bytes.Contains(raw, []byte(`"style"`)) || !bytes.Contains(raw, []byte(`"model":"`+wantModel+`"`)) {
		t.Fatalf("panel %d %s", res.StatusCode, raw)
	}
	res = do(http.MethodGet, "/api/bots", nil, "")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Contains(raw, []byte(`"description":"项目笔记"`)) || !bytes.Contains(raw, []byte(`"initial":"A"`)) || !bytes.Contains(raw, []byte(`"avatar_bg"`)) ||
		!bytes.Contains(raw, []byte(`"avatar":"generated"`)) || !bytes.Contains(raw, []byte(`"avatar_seed":"ada"`)) || bytes.Contains(raw, []byte(`"avatar_url"`)) {
		t.Fatalf("list %s", raw)
	}
	res = do(http.MethodGet, "/api/bots/ada", nil, "")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Contains(raw, []byte(`"avatar":"generated"`)) || !bytes.Contains(raw, []byte(`"avatar_seed":"ada"`)) {
		t.Fatalf("detail %s", raw)
	}
	// A generated bot has no avatar bytes to serve, even with a generator installed.
	restore := bot.SetGenerateAvatar(func(name, description string) ([]byte, error) {
		return []byte("generated-png"), nil
	})
	res = do(http.MethodGet, "/api/bots/ada/avatar", nil, "")
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || bytes.Contains(got, []byte("generated-png")) {
		t.Fatalf("generated avatar served: %d %q", res.StatusCode, got)
	}
	restore()
	res = do(http.MethodPatch, "/api/bots/ada", strings.NewReader(`{"avatar_seed":"<x>"}`), "application/json")
	got, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(got, []byte("avatar seed")) {
		t.Fatalf("bad seed %d %q", res.StatusCode, got)
	}
	res = do(http.MethodPatch, "/api/bots/ada", strings.NewReader(`{"avatar_seed":"writer#3"}`), "application/json")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("patch seed %d", res.StatusCode)
	}
	if b, err = bot.Load(root, "ada"); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != bot.AvatarGenerated || seed != "writer#3" {
		t.Fatalf("seed %q %q", av, seed)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	res = do(http.MethodPost, "/api/bots/ada/avatar", &buf, w.FormDataContentType())
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"avatar_seed":"writer#3"`)) {
		t.Fatalf("avatar %d %s", res.StatusCode, raw)
	}
	// The upload survives the earlier seed patch: uploading keeps the seed.
	if b, err = bot.Load(root, "ada"); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != bot.AvatarCustom || seed != "writer#3" {
		t.Fatalf("after upload %q %q", av, seed)
	}
	res = do(http.MethodGet, "/api/bots/ada/avatar", nil, "")
	got, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(got, png) || res.Header.Get("Content-Type") != "image/png" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("get avatar %d %q %v", res.StatusCode, got, res.Header)
	}
	res = do(http.MethodGet, "/api/bots", nil, "")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Contains(raw, []byte(`"avatar":"custom"`)) || !bytes.Contains(raw, []byte(`"avatar_url":"/api/bots/ada/avatar"`)) {
		t.Fatalf("custom list %s", raw)
	}
	res = do(http.MethodDelete, "/api/bots/ada/avatar", nil, "")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"avatar":"generated"`)) || !bytes.Contains(raw, []byte(`"avatar_seed":"writer#3"`)) {
		t.Fatalf("delete %d %s", res.StatusCode, raw)
	}
	res = do(http.MethodGet, "/api/bots/ada/avatar", nil, "")
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("cleared avatar %d", res.StatusCode)
	}
}

func TestSetPrimaryAndDeleteAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ada", "bob"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameWeb})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{"u1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := bot.SetPrimary(root, "ada"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	do := func(method, path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := do(http.MethodDelete, "/api/bots/ada")
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict || !bytes.Contains(raw, []byte("primary")) {
		t.Fatalf("delete primary %d %s", res.StatusCode, raw)
	}
	res = do(http.MethodPost, "/api/bots/bob/primary")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"primary":true`)) || !bytes.Contains(raw, []byte(`"pinned":true`)) {
		t.Fatalf("set primary %d %s", res.StatusCode, raw)
	}
	got, err := bot.Primary(root)
	if err != nil || got != "bob" {
		t.Fatalf("primary %q %v", got, err)
	}
	bob, err := bot.Load(root, "bob")
	if err != nil || !bob.Config().Pinned {
		t.Fatalf("bob pin %v", err)
	}
	res = do(http.MethodDelete, "/api/bots/ada")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("delete ada %d", res.StatusCode)
	}
	if _, err := bot.Load(root, "ada"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("ada still present: %v", err)
	}
	res = do(http.MethodDelete, "/api/bots/bob")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict || !bytes.Contains(raw, []byte("last bot")) {
		t.Fatalf("delete last %d %s", res.StatusCode, raw)
	}
}

func TestCreateBotAvatarSeed(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	post := func(body string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/bots", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, raw
	}
	res, raw := post(`{"name":"bad","avatar_seed":"<x>"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad seed %d %s", res.StatusCode, raw)
	}
	if _, err := bot.Load(root, "bad"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("bad seed left a bot: %v", err)
	}
	res, raw = post(`{"name":"ada","avatar_seed":"writer#3"}`)
	if res.StatusCode != 200 {
		t.Fatalf("create %d %s", res.StatusCode, raw)
	}
	b, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != bot.AvatarGenerated || seed != "writer#3" {
		t.Fatalf("seed %q %q", av, seed)
	}
	res, raw = post(`{"name":"bob"}`)
	if res.StatusCode != 200 {
		t.Fatalf("create plain %d %s", res.StatusCode, raw)
	}
	bob, err := bot.Load(root, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := bob.AvatarState(); av != bot.AvatarGenerated || seed != "bob" {
		t.Fatalf("default seed %q %q", av, seed)
	}
}

func TestExportAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ada, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if err := ada.Append(bot.Entry{Time: when, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hello export")}}); err != nil {
		t.Fatal(err)
	}
	if err := ada.Append(bot.Entry{Time: when.Add(time.Minute), Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c1", "TOOL-RESULT")}}); err != nil {
		t.Fatal(err)
	}
	if err := ada.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: "SUMMARY-TEXT"}}}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(root, "eve", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func(path string, cookie bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := get("/api/bots/ada/export?format=md", false)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon %d", res.StatusCode)
	}
	res = get("/api/bots/eve/export", true)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("other owner %d", res.StatusCode)
	}
	res = get("/api/bots/ada/export?format=md", true)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte("## seq 1")) || !bytes.Contains(raw, []byte("hello export")) || !bytes.Contains(raw, []byte("## Summary seq 1-1")) {
		t.Fatalf("md %d %s", res.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("TOOL-RESULT")) {
		t.Fatalf("tools default on\n%s", raw)
	}
	res = get("/api/bots/ada/export?format=jsonl&tools=1", true)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"seq":1`)) || !bytes.Contains(raw, []byte(`"seq":2`)) || !bytes.Contains(raw, []byte("TOOL-RESULT")) {
		t.Fatalf("jsonl %d %s", res.StatusCode, raw)
	}
	var entry bot.Entry
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if bytes.Contains(line, []byte(`"kind":"summary"`)) {
			continue
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(line, []byte(`"seq":1`)) && entry.Message.Text() != "hello export" {
			t.Fatalf("round trip %s", line)
		}
	}
}

func muxOf(s *Server) http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/skill"
)

func TestSkillAPIListReadDisable(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ada, err := bot.Create(root, "ada", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(root, "eve", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(root, "u1", "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	call := func(method, path string, auth bool) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
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

	if code, _ := call(http.MethodGet, "/api/bots/ada/skills", false); code != http.StatusUnauthorized {
		t.Fatalf("auth %d", code)
	}
	code, raw := call(http.MethodGet, "/api/bots/eve/skills", true)
	if code != http.StatusNotFound {
		t.Fatalf("eve %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/skills", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"slug":"ship"`) || !strings.Contains(string(raw), `"disabled":false`) || strings.Contains(string(raw), "SECRET_BODY") {
		t.Fatalf("list %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/skills/ship", true)
	if code != http.StatusOK || !strings.Contains(string(raw), "SECRET_BODY") || !strings.Contains(string(raw), `"description":"Pack a release"`) {
		t.Fatalf("read %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/skills/missing/disable", true)
	if code != http.StatusNotFound {
		t.Fatalf("missing %d %s", code, raw)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/skills/ship/disable", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"disabled":true`) {
		t.Fatalf("disable %d %s", code, raw)
	}
	code, raw = call(http.MethodGet, "/api/bots/ada/skills", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"disabled":true`) {
		t.Fatalf("listed %d %s", code, raw)
	}
	bob, err = bot.Load(root, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(bob.Config().DisabledSkills) != 0 {
		t.Fatalf("bob disabled %#v", bob.Config().DisabledSkills)
	}
	code, raw = call(http.MethodPost, "/api/bots/ada/skills/ship/enable", true)
	if code != http.StatusOK || !strings.Contains(string(raw), `"disabled":false`) {
		t.Fatalf("enable %d %s", code, raw)
	}
}

func skillTestServer(t *testing.T) (root, tok string, call func(method, path, body string, auth bool) (int, string)) {
	t.Helper()
	root = t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for name, owner := range map[string]string{"ada": "u1", "bob": "u1", "eve": "u2"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameSkills})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{owner}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(muxOf(s))
	t.Cleanup(srv.Close)
	call = func(method, path, body string, auth bool) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
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
		return res.StatusCode, string(raw)
	}
	return root, tok, call
}

func TestSkillLibraryListsOwnersSkillsWithPerBotState(t *testing.T) {
	root, _, call := skillTestServer(t)
	if code, raw := call(http.MethodGet, "/api/skills", "", true); code != http.StatusOK || !strings.Contains(raw, `"skills":[]`) {
		t.Fatalf("empty library %d %s", code, raw)
	}
	if err := skill.Write(root, "u1", "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(root, "u2", "theirs", "Not yours", "x"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/api/skills", "/api/skills/ship"} {
		if code, _ := call(http.MethodGet, p, "", false); code != http.StatusUnauthorized {
			t.Fatalf("%s without login: %d", p, code)
		}
	}
	code, raw := call(http.MethodGet, "/api/skills", "", true)
	if code != http.StatusOK || !strings.Contains(raw, `"slug":"ship"`) || strings.Contains(raw, "theirs") || strings.Contains(raw, "SECRET_BODY") {
		t.Fatalf("list %d %s", code, raw)
	}
	var detail struct {
		Name, Description string
		Bots              []struct {
			Name    string
			Enabled bool
		}
	}
	read := func() {
		t.Helper()
		code, raw := call(http.MethodGet, "/api/skills/ship", "", true)
		if code != http.StatusOK || strings.Contains(raw, "SECRET_BODY") || strings.Contains(raw, "eve") {
			t.Fatalf("detail %d %s", code, raw)
		}
		detail.Bots = nil
		if err := json.Unmarshal([]byte(raw), &detail); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if len(detail.Bots) != 2 || !detail.Bots[0].Enabled || !detail.Bots[1].Enabled {
		t.Fatalf("every bot starts with the skill on: %+v", detail.Bots)
	}
	// Switching one bot off through the per-bot endpoint shows up in the library view, and only for that bot.
	for i := 0; i < 2; i++ { // repeating is harmless
		if code, raw := call(http.MethodPost, "/api/bots/ada/skills/ship/disable", "", true); code != http.StatusOK {
			t.Fatalf("disable %d %s", code, raw)
		}
	}
	read()
	if detail.Bots[0].Name != "ada" || detail.Bots[0].Enabled || !detail.Bots[1].Enabled {
		t.Fatalf("only ada is off: %+v", detail.Bots)
	}
	ada, _ := bot.Load(root, "ada")
	if got := ada.Config().DisabledSkills; len(got) != 1 || got[0] != "ship" {
		t.Fatalf("duplicate disable stored twice: %#v", got)
	}
	for i := 0; i < 2; i++ {
		if code, raw := call(http.MethodPost, "/api/bots/ada/skills/ship/enable", "", true); code != http.StatusOK {
			t.Fatalf("enable %d %s", code, raw)
		}
	}
	read()
	if !detail.Bots[0].Enabled {
		t.Fatalf("enable did not stick: %+v", detail.Bots)
	}
	code, raw = call(http.MethodGet, "/api/skills/nope", "", true)
	if code != http.StatusNotFound || strings.Contains(raw, "slug") || strings.Contains(raw, "skill:") {
		t.Fatalf("missing skill must read as plain not-found: %d %s", code, raw)
	}
	if code, _ := call(http.MethodGet, "/api/skills/theirs", "", true); code != http.StatusNotFound {
		t.Fatalf("another owner's skill is not visible: %d", code)
	}
}

func TestSkillImportAsksBeforeOverwriting(t *testing.T) {
	root, _, call := skillTestServer(t)
	post := func(folder, content string, overwrite bool) (int, string) {
		b, _ := json.Marshal(map[string]any{"folder": folder, "content": content, "overwrite": overwrite})
		return call(http.MethodPost, "/api/skills/import", string(b), true)
	}
	v1 := "---\nname: 写周报\ndescription: one\n---\n\nBODY1\n"
	v2 := "---\nname: 写周报\ndescription: two\n---\n\nBODY2\n"
	if code, _ := call(http.MethodPost, "/api/skills/import", `{"folder":"weekly","content":"x"}`, false); code != http.StatusUnauthorized {
		t.Fatalf("import needs login: %d", code)
	}
	code, raw := post("weekly", v1, false)
	if code != http.StatusOK || !strings.Contains(raw, `"slug":"weekly"`) {
		t.Fatalf("import %d %s", code, raw)
	}
	code, raw = post("weekly", v2, false)
	if code != http.StatusConflict || !strings.Contains(raw, "库里已有同名技能，要覆盖吗？") || !strings.Contains(raw, `"conflict":true`) || !strings.Contains(raw, "写周报") {
		t.Fatalf("same name must ask: %d %s", code, raw)
	}
	got, _ := os.ReadFile(filepath.Join(skill.OwnerDir(root, "u1"), "weekly", "SKILL.md"))
	if !strings.Contains(string(got), "BODY1") {
		t.Fatalf("asking changed the library: %s", got)
	}
	if code, raw = post("weekly", v2, true); code != http.StatusOK {
		t.Fatalf("overwrite %d %s", code, raw)
	}
	got, _ = os.ReadFile(filepath.Join(skill.OwnerDir(root, "u1"), "weekly", "SKILL.md"))
	if !strings.Contains(string(got), "BODY2") {
		t.Fatalf("overwrite did not replace: %s", got)
	}
	// Per-bot switches survive an overwrite.
	call(http.MethodPost, "/api/bots/ada/skills/weekly/disable", "", true)
	post("weekly", v1, true)
	ada, _ := bot.Load(root, "ada")
	if len(ada.Config().DisabledSkills) != 1 {
		t.Fatalf("overwrite reset ada's switch: %#v", ada.Config().DisabledSkills)
	}
	for _, c := range []struct{ name, folder, content, want string }{
		{"no description", "weekly", "just text", "没找到技能说明"},
		{"name that cannot be a folder", "Bad Name", "---\nname: 写周报2\ndescription: x\n---\n", "名字不能用"},
		{"traversal in folder", "../../x", "---\nname: 写周报2\ndescription: x\n---\n", "名字不能用"},
	} {
		code, raw := post(c.folder, c.content, false)
		for _, banned := range []string{"slug", "SKILL.md", "/", "CLI", "skill:"} {
			if code != http.StatusBadRequest || strings.Contains(strings.ReplaceAll(raw, `{"error":"`, ""), banned) {
				t.Fatalf("%s: %d %s (leaks %q)", c.name, code, raw, banned)
			}
		}
		if !strings.Contains(raw, c.want) {
			t.Fatalf("%s: %d %s", c.name, code, raw)
		}
	}
	if code, _ := call(http.MethodPost, "/api/skills/import", `{"folder":"x","content":"y","extra":1}`, true); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
}

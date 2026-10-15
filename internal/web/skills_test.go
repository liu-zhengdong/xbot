package web

import (
	"io"
	"net/http"
	"net/http/httptest"
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

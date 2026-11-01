package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestProfileAvatarCombinationValidatesBeforeSaving(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatar([]byte("\x89PNG\r\n\x1a\nold-upload"), "image/png"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(muxOf(s))
	defer host.Close()
	cookie := loginCookie(t, host.URL, token)
	req, _ := http.NewRequest(http.MethodPatch, host.URL+"/api/bots/ada", strings.NewReader(`{"avatar_seed":"must-not-persist","description":"must-not-persist","timezone":"Invalid/Timezone"}`))
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatal(resp.StatusCode)
	}
	fresh, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if mode, seed := fresh.AvatarState(); mode != bot.AvatarCustom || seed != "ada" {
		t.Fatalf("invalid combination changed avatar: %s %s", mode, seed)
	}
	if fresh.Config().Description != "" {
		t.Fatal("invalid combination changed description")
	}
	if _, _, ok, err := fresh.AvatarBytes(); err != nil || !ok {
		t.Fatalf("invalid combination removed upload: %v %v", ok, err)
	}
}

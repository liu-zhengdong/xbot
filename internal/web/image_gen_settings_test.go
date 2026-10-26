package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/config"
	"xbot/internal/vault"
)

func TestImageGenSettingsAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "owner"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	call := func(method, body string, auth bool) (int, string) {
		r := httptest.NewRequest(method, "/api/image-gen", strings.NewReader(body))
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := call(method, `{}`, false); code != 401 {
			t.Fatal(code)
		}
	}
	if code, raw := call("GET", "", true); code != 200 || !strings.Contains(raw, `"enabled":false`) || !strings.Contains(raw, `"provider":"openai"`) || strings.Contains(raw, `"logged_in":true`) {
		t.Fatalf("default %d %s", code, raw)
	}
	// A configured API key is not an OAuth login.
	if err := config.Add(root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "IMAGE_FAKE_KEY"}, config.ModelSpec{Name: "gpt-5.5", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FAKE_KEY", "fake-image-key")
	if _, raw := call("GET", "", true); strings.Contains(raw, `"logged_in":true`) {
		t.Fatal("API key counted as login")
	}
	b, err := config.OAuthVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SetOAuth(b, "xai", vault.OAuthCredential{AccessToken: "fake-image-oauth"}, []string{"api.x.ai"}); err != nil {
		t.Fatal(err)
	}
	if code, raw := call("GET", "", true); code != 200 || !strings.Contains(raw, `"provider":"xai"`) || !strings.Contains(raw, `"logged_in":true`) || strings.Contains(raw, "fake-image") {
		t.Fatalf("login priority %d %s", code, raw)
	}
	for _, body := range []string{`{}`, `null`, `{"enabled":true,"provider":"bad"}`, `{"enabled":null,"provider":"openai"}`, `{"enabled":true,"provider":"xai","key":"secret"}`, `{"enabled":true,"provider":"openai"} {}`, `{"enabled":true,"provider":"openai"}` + strings.Repeat(" ", 4096)} {
		if code, _ := call("PUT", body, true); code != 400 {
			t.Fatalf("invalid input returned %d", code)
		}
	}
	for _, id := range []string{"openai", "xai"} {
		for _, enabled := range []bool{true, false} {
			body, _ := json.Marshal(map[string]any{"enabled": enabled, "provider": id})
			for i := 0; i < 2; i++ {
				code, raw := call("PUT", string(body), true)
				if code != 200 {
					t.Fatalf("PUT %d %s", code, raw)
				}
				f, err := config.LoadWeb(root)
				if err != nil || f.ImageGenOn() != enabled || f.ImageGenProvider != id {
					t.Fatalf("saved %+v %v", f, err)
				}
			}
		}
	}
	// Explicit choice survives a different account logging in.
	if err := config.SetWebImageGen(root, true, "openai"); err != nil {
		t.Fatal(err)
	}
	if _, raw := call("GET", "", true); !strings.Contains(raw, `"provider":"openai"`) {
		t.Fatal("choice changed")
	}
}

package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/providerauth"
	"xbot/internal/vault"
)

func TestOAuthWebRoutesBindingAndKeyIsolation(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	var random [32]byte
	_, _ = rand.Read(random[:])
	key := base64.RawURLEncoding.EncodeToString(random[:])
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
	}))
	defer up.Close()
	m := &providerauth.Manager{Root: root, Endpoints: map[string]providerauth.Endpoints{"openrouter": {Authorize: up.URL + "/auth", Token: up.URL + "/keys", API: up.URL + "/v1"}}}
	defer m.Close()
	s := &Server{Root: root, Owner: "u1", oauth: m}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	call := func(method, path, body string, auth bool) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, ui.URL+path, strings.NewReader(body))
		if auth {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("HTTP call failed")
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if strings.Contains(string(raw), key) {
			t.Fatal("credential exposed by API")
		}
		return res.StatusCode, raw
	}
	if code, _ := call("POST", "/api/providers/oauth/openrouter/start", "{}", false); code != 401 {
		t.Fatalf("unauthenticated %d", code)
	}
	if code, _ := call("POST", "/api/providers/oauth/anthropic/start", "{}", true); code != 400 {
		t.Fatalf("Claude login %d", code)
	}
	if code, _ := call("GET", "/api/providers/oauth/anthropic/status", "", true); code != 404 {
		t.Fatalf("Claude status %d", code)
	}
	code, raw := call("POST", "/api/providers/oauth/openrouter/start", "{}", true)
	if code != 200 {
		t.Fatalf("start %d %s", code, raw)
	}
	var start providerauth.Start
	if json.Unmarshal(raw, &start) != nil {
		t.Fatal("start JSON")
	}
	u, _ := url.Parse(start.URL)
	cb, _ := url.Parse(u.Query().Get("callback_url"))
	params := cb.Query()
	params.Set("code", "fake-single-use-code")
	cb.RawQuery = params.Encode()
	res, err := http.Get(cb.String())
	if err != nil {
		t.Fatal("callback failed")
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("callback %d", res.StatusCode)
	}
	if code, _ := call("PUT", "/api/bots/ada/binding", `{"provider":"openrouter","model":"openai/gpt-5.4"}`, true); code != 200 {
		t.Fatalf("OAuth binding %d", code)
	}
	if code, raw := call("GET", "/api/providers", "", true); code != 200 || !strings.Contains(string(raw), `"oauth":"openrouter"`) || !strings.Contains(string(raw), `"key_configured":true`) {
		t.Fatalf("catalog %d %s", code, raw)
	}
	t.Setenv("XBOT_OAUTH_KEY_REGRESSION", "test-api-key")
	if err := config.Add(root, config.Provider{Name: "key-only", Protocol: config.ChatCompletions, BaseURL: up.URL + "/v1", APIKeyEnv: "XBOT_OAUTH_KEY_REGRESSION"}, config.ModelSpec{Name: "fake", Context: 4096}); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("PUT", "/api/bots/ada/binding", `{"provider":"key-only","model":"fake"}`, true); code != 200 {
		t.Fatalf("API key binding %d", code)
	}
	ep, _, err := config.Resolve(root, "key-only", "fake", config.Getenv)
	if err != nil || ep.APIKey != "test-api-key" || ep.HTTPClient != nil {
		t.Fatal("OAuth contaminated API key path")
	}
	backend, _ := config.OAuthVault(root)
	info, err := vault.OAuthMetadata(context.Background(), backend, "openrouter")
	if err != nil || !info.Configured {
		t.Fatal("key binding damaged OAuth credential")
	}
	// An unavailable OAuth account must not hide working API-key bindings.
	if err := os.WriteFile(filepath.Join(root, "vault", "_provider_accounts", "secrets.enc"), []byte("corrupt test vault"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, raw := call("GET", "/api/providers", "", true); code != 200 || !strings.Contains(string(raw), `"auth_error"`) || !strings.Contains(string(raw), `"name":"key-only"`) {
		t.Fatalf("OAuth failure blocked key catalog: %d", code)
	}
	if code, _ := call("PUT", "/api/bots/ada/binding", `{"provider":"key-only","model":"fake"}`, true); code != 200 {
		t.Fatalf("OAuth failure blocked key binding: %d", code)
	}
}

func TestOAuthCancelRoute(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	// The xAI loopback port is fixed in production; tests take an ephemeral one
	// so parallel package binaries never fight over it.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	m := &providerauth.Manager{Root: root, CallbackPort: port}
	defer m.Close()
	s := &Server{Root: root, Owner: "u1", oauth: m}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	call := func(method, path string, auth bool) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, ui.URL+path, nil)
		if auth {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("HTTP call failed")
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, raw
	}
	if code, _ := call("POST", "/api/providers/oauth/xai/cancel", false); code != 401 {
		t.Fatalf("unauthenticated cancel %d", code)
	}
	if code, _ := call("POST", "/api/providers/oauth/anthropic/cancel", true); code != 404 {
		t.Fatalf("Claude cancel %d", code)
	}
	if code, _ := call("POST", "/api/providers/oauth/openrouter/cancel", true); code != 409 {
		t.Fatalf("idle cancel %d", code)
	}
	if code, raw := call("GET", "/api/providers/oauth/xai/status", true); code != 200 || !strings.Contains(string(raw), `"state":"idle"`) {
		t.Fatalf("xai status %d %s", code, raw)
	}
	if code, raw := call("POST", "/api/providers/oauth/xai/start", true); code != 200 {
		t.Fatalf("xai start %d %s", code, raw)
	}
	if code, raw := call("GET", "/api/providers/oauth/xai/status", true); code != 200 || !strings.Contains(string(raw), `"state":"pending"`) {
		t.Fatalf("xai pending %d %s", code, raw)
	}
	if code, raw := call("POST", "/api/providers/oauth/chatgpt/start", true); code != 400 || !strings.Contains(string(raw), "另一个登录") {
		t.Fatalf("cross-kind start %d %s", code, raw)
	}
	if code, raw := call("POST", "/api/providers/oauth/xai/cancel", true); code != 200 {
		t.Fatalf("xai cancel %d %s", code, raw)
	}
	if code, raw := call("GET", "/api/providers/oauth/xai/status", true); code != 200 || !strings.Contains(string(raw), `"state":"cancelled"`) {
		t.Fatalf("xai cancelled %d %s", code, raw)
	}
	// An immediate restart on the same port works after cancelling.
	if code, _ := call("POST", "/api/providers/oauth/xai/start", true); code != 200 {
		t.Fatalf("xai restart %d", code)
	}
	if code, _ := call("POST", "/api/providers/oauth/xai/cancel", true); code != 200 {
		t.Fatal("second cancel")
	}
	// The cancel capability is shared with the other kinds.
	if code, _ := call("POST", "/api/providers/oauth/chatgpt/start", true); code != 200 {
		t.Fatal("chatgpt start")
	}
	if code, _ := call("POST", "/api/providers/oauth/chatgpt/cancel", true); code != 200 {
		t.Fatal("chatgpt cancel")
	}
	if code, raw := call("GET", "/api/providers/oauth/chatgpt/status", true); code != 200 || !strings.Contains(string(raw), `"state":"cancelled"`) {
		t.Fatalf("chatgpt cancelled %d %s", code, raw)
	}
}

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
	"sync"
	"testing"
	"time"

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

// TestOAuthLogoutRoute: logging out of xAI removes its vault credential and
// OAuth binding, leaves other bindings alone, and can be repeated.
func TestOAuthLogoutRoute(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_VAULT_SOCKET", "")
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	backend, err := config.OAuthVault(root)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "xai-logout-regression-token"
	if err := vault.SetOAuth(backend, "xai", vault.OAuthCredential{AccessToken: secret}, []string{"api.x.ai"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveOAuthProvider(root, config.Provider{Name: "xai", OAuth: "xai", BaseURL: "https://api.x.ai/v1", Protocol: config.Responses, Models: []config.ModelSpec{{Name: "grok-4", Context: 256000}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_LOGOUT_KEY_REGRESSION", "test-api-key")
	if err := config.Add(root, config.Provider{Name: "key-only", Protocol: config.ChatCompletions, BaseURL: "https://example.invalid/v1", APIKeyEnv: "XBOT_LOGOUT_KEY_REGRESSION"}, config.ModelSpec{Name: "fake", Context: 4096}); err != nil {
		t.Fatal(err)
	}
	m := &providerauth.Manager{Root: root}
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
		if strings.Contains(string(raw), secret) {
			t.Fatal("credential exposed by API")
		}
		return res.StatusCode, raw
	}
	if code, raw := call("GET", "/api/providers", true); code != 200 || !strings.Contains(string(raw), `"oauth":"xai"`) {
		t.Fatalf("logged-in catalog %d %s", code, raw)
	}
	if code, _ := call("POST", "/api/providers/oauth/xai/logout", false); code != 401 {
		t.Fatalf("unauthenticated logout %d", code)
	}
	if code, _ := call("POST", "/api/providers/oauth/anthropic/logout", true); code != 404 {
		t.Fatalf("Claude logout %d", code)
	}
	if code, raw := call("POST", "/api/providers/oauth/xai/logout", true); code != 200 {
		t.Fatalf("xai logout %d %s", code, raw)
	}
	if info, err := vault.OAuthMetadata(context.Background(), backend, "xai"); err == nil && info.Configured {
		t.Fatal("xai credential survived logout")
	}
	f, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range f.Providers {
		if p.OAuth == "xai" {
			t.Fatal("xai OAuth binding survived logout")
		}
		names = append(names, p.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "key-only") {
		t.Fatalf("logout removed an unrelated binding: %v", names)
	}
	if code, raw := call("GET", "/api/providers", true); code != 200 || strings.Contains(string(raw), `"oauth":"xai"`) {
		t.Fatalf("catalog after logout %d %s", code, raw)
	}
	if code, raw := call("GET", "/api/providers/oauth/xai/status", true); code != 200 || !strings.Contains(string(raw), `"state":"idle"`) {
		t.Fatalf("status after logout %d %s", code, raw)
	}
	// Logging out again with nothing stored still succeeds.
	if code, raw := call("POST", "/api/providers/oauth/xai/logout", true); code != 200 {
		t.Fatalf("repeat logout %d %s", code, raw)
	}
}

// TestOAuthLogoutDuringRefresh: logout returns while a vault refresh is still
// waiting on the token endpoint; when the endpoint answers, the refresh must
// not bring the credential back. Bindings stay gone and status stays idle.
func TestOAuthLogoutDuringRefresh(t *testing.T) {
	for _, daemon := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "daemon"}[daemon], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XBOT_VAULT_SOCKET", "")
			if daemon {
				// Keep the Unix socket path below macOS's 104-byte limit.
				socketDir, err := os.MkdirTemp("", "xl-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(socketDir) })
				socket := filepath.Join(socketDir, "v.sock")
				ln, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				server := &vault.Server{DataDir: filepath.Join(root, "daemon"), AllowUIDs: []int{os.Getuid()}}
				stopped := make(chan struct{})
				go func() { defer close(stopped); _ = server.Serve(ln) }()
				t.Cleanup(func() { ln.Close(); <-stopped })
				t.Setenv("XBOT_VAULT_SOCKET", socket)
			}
			b, err := bot.Create(root, "ada", "persona", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			seen, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { close(seen) })
				<-release
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "logout-race-new-access", "refresh_token": "logout-race-new-refresh", "expires_in": 3600, "token_type": "Bearer"})
			}))
			defer up.Close()
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			backend, err := config.OAuthVault(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.SetOAuth(backend, "xai", vault.OAuthCredential{AccessToken: "logout-race-old-access", RefreshToken: "logout-race-old-refresh", ExpiresAt: time.Now().Add(-time.Hour), ClientID: "test", TokenURL: up.URL + "/token"}, []string{"127.0.0.1", "api.x.ai"}); err != nil {
				t.Fatal(err)
			}
			if err := config.SaveOAuthProvider(root, config.Provider{Name: "xai", OAuth: "xai", Protocol: config.Responses, BaseURL: "https://api.x.ai/v1", Models: []config.ModelSpec{{Name: "grok-4", Context: 256000}}}); err != nil {
				t.Fatal(err)
			}
			m := &providerauth.Manager{Root: root}
			defer m.Close()
			s := &Server{Root: root, Owner: "u1", oauth: m}
			token, _, err := s.EnsureToken()
			if err != nil {
				t.Fatal(err)
			}
			ui := httptest.NewServer(muxOf(s))
			defer func() { unblock(); ui.Close() }()
			cookie := loginCookie(t, ui.URL, token)
			done := make(chan error, 1)
			go func() { _, err := vault.EnsureOAuth(context.Background(), backend, "xai"); done <- err }()
			select {
			case <-seen:
			case <-time.After(3 * time.Second):
				t.Fatal("refresh did not start")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", ui.URL+"/api/providers/oauth/xai/logout", nil)
			req.AddCookie(cookie)
			started := time.Now()
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("logout did not finish while refresh was blocked: %v", err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatalf("logout status %d", res.StatusCode)
			}
			t.Logf("logout returned while token endpoint blocked: %s", time.Since(started))
			assertAbsent := func() {
				t.Helper()
				info, err := vault.OAuthMetadata(context.Background(), backend, "xai")
				if err != nil {
					t.Fatalf("read credential metadata: %v", err)
				}
				if info.Configured {
					t.Fatal("credential configured after logout")
				}
			}
			assertAbsent()
			unblock()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "login required") {
					t.Errorf("late refresh result %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("refresh did not finish")
			}
			assertAbsent()
			registry, err := config.Load(root)
			if err != nil || len(registry.Providers) != 0 {
				t.Fatal("binding came back after logout")
			}
			if st := m.Status("xai").State; st != "idle" {
				t.Fatalf("status after logout %s", st)
			}
		})
	}
}

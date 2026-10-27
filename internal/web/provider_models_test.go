package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/vault"
)

func TestProviderModelsRuntimeCatalog(t *testing.T) {
	for _, protocol := range []string{config.ChatCompletions, config.Responses, config.AnthropicMessages} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/v1/models" {
					t.Errorf("wrong catalog endpoint: %s %s", r.Method, r.URL.Path)
				}
				if protocol == config.AnthropicMessages {
					if r.Header.Get("x-api-key") != setupTestKey || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
						t.Error("wrong Messages authentication")
					}
					if r.URL.Query().Get("after_id") == "" {
						fmt.Fprint(w, `{"data":[{"id":"runtime-only"}],"has_more":true,"last_id":"runtime-only"}`)
						return
					}
					if r.URL.Query().Get("after_id") != "runtime-only" {
						t.Error("wrong cursor")
					}
				} else if r.Header.Get("Authorization") != "Bearer "+setupTestKey {
					t.Error("wrong compatible authentication")
				}
				fmt.Fprint(w, `{"data":[{"id":"runtime-only"},{"id":"configured"},{"id":"configured"}]}`)
			})
			config.SetHeld("XBOT_API_KEY", setupTestKey)
			yes := true
			spec := config.ModelSpec{Name: "configured", Context: 200000, Vision: &yes}
			if protocol != config.ChatCompletions {
				spec.HostedSearch = &yes
			}
			if err := config.Add(s.Root, config.Provider{Name: "vendor", Protocol: protocol, BaseURL: config.FromEnv(config.Getenv).BaseURL, APIKeyEnv: "XBOT_API_KEY"}, spec); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			code, out := setupCall(t, srv, tok, "GET", "/api/providers/models?provider=vendor", "")
			t.Logf("scripted %s catalog HTTP round trip: %s", protocol, time.Since(started))
			raw, _ := json.Marshal(out)
			var page struct {
				Models []config.ModelInfo `json:"models"`
			}
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatal(err)
			}
			if code != 200 || len(page.Models) != 2 {
				t.Fatalf("catalog: %d %s", code, raw)
			}
			if page.Models[0].Model != "configured" || page.Models[0].Context != 200000 || !page.Models[0].Vision || page.Models[1].Model != "runtime-only" || page.Models[1].Context != compact.DefaultWindow {
				t.Fatalf("wrong capabilities: %s", raw)
			}
			if page.Models[0].HostedSearch != (protocol != config.ChatCompletions) {
				t.Fatal("lost hosted override")
			}
			if protocol == config.AnthropicMessages && calls != 2 {
				t.Fatalf("pagination calls=%d", calls)
			}
			if strings.Contains(string(raw), setupTestKey) {
				t.Fatal("credential leaked")
			}
			saved, _ := config.Load(s.Root)
			if len(saved.Providers[0].Models) != 1 {
				t.Fatal("catalog read mutated configuration")
			}
		})
	}
}

func TestProviderModelsRejectsMissingKeyAndUnsafeResponses(t *testing.T) {
	for _, mode := range []string{"missing-key", "http-error", "redirect", "malformed", "oversize", "output-oversize", "pagination-loop", "empty"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "missing-key":
					t.Error("called upstream without a key")
				case "http-error":
					http.Error(w, setupTestKey, 401)
				case "redirect":
					http.Redirect(w, r, "/must-not-follow", 302)
				case "malformed":
					fmt.Fprint(w, `{"error":"`+setupTestKey+`"}`)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
				case "output-oversize":
					rows := make([]map[string]string, 2048)
					for i := range rows {
						rows[i] = map[string]string{"id": fmt.Sprintf("%04d-%s", i, strings.Repeat("a", 240))}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
				case "pagination-loop":
					fmt.Fprint(w, `{"data":[{"id":"loop"}],"has_more":true,"last_id":"loop"}`)
				case "empty":
					fmt.Fprint(w, `{"data":[]}`)
				}
			})
			if mode != "missing-key" {
				config.SetHeld("XBOT_API_KEY", setupTestKey)
			}
			if mode == "pagination-loop" {
				if err := config.Add(s.Root, config.Provider{Name: "vendor", Protocol: config.AnthropicMessages, BaseURL: config.FromEnv(config.Getenv).BaseURL, APIKeyEnv: "XBOT_API_KEY"}, config.ModelSpec{Name: "configured", Context: 128000}); err != nil {
					t.Fatal(err)
				}
			}
			// The deepseek entry is seeded from XBOT_BASE_URL, the mock upstream.
			if _, err := config.EnsureDefault(s.Root); err != nil {
				t.Fatal(err)
			}
			path := "/api/providers/models?provider=deepseek"
			if mode == "pagination-loop" {
				path = "/api/providers/models?provider=vendor"
			}
			code, out := setupCall(t, srv, tok, "GET", path, "")
			raw, _ := json.Marshal(out)
			want := 422
			if mode == "empty" {
				want = 200
			}
			if code != want || strings.Contains(string(raw), setupTestKey) {
				t.Fatalf("unsafe catalog: %d %s", code, raw)
			}
			if mode == "redirect" && calls != 1 {
				t.Fatal("followed redirect")
			}
			if mode == "pagination-loop" && calls != 2 {
				t.Fatalf("unbounded cursor: %d", calls)
			}
			res, err := srv.Client().Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 401 {
				t.Fatalf("unauthenticated: %d", res.StatusCode)
			}
		})
	}
}

// A configured subscription uses the SIWC catalog, never an API-key catalog.
func TestProviderModelsChatGPTSubscription(t *testing.T) {
	calls := 0
	names := []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol"}
	models := []config.ModelSpec{{Name: "gpt-5.4", Context: 1000000}}
	var rows []string
	for _, name := range names {
		// SIWC has no capability fields; existing configs have no overrides either.
		rows = append(rows, fmt.Sprintf(`{"slug":%q,"visibility":"list","context_window":272000}`, name))
		models = append(models, config.ModelSpec{Name: name, Context: 1000000})
	}
	// Also exercise a known model not yet stored in providers.json.
	rows = append(rows, `{"slug":"gpt-5.6","visibility":"list","context_window":272000}`)
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer scripted-account-token" {
			t.Error("wrong SIWC request")
		}
		fmt.Fprint(w, `{"models":[`+strings.Join(rows, ",")+`,{"slug":"runtime-only","visibility":"list","context_window":272000},{"slug":"gpt-5.4","visibility":"list","context_window":128000},{"slug":"hidden","visibility":"hidden"},{"slug":"runtime-only","visibility":"list"}]}`)
	})
	base := config.FromEnv(config.Getenv).BaseURL
	err := config.SaveOAuthProvider(s.Root, config.Provider{Name: "account", OAuth: "chatgpt", Protocol: config.Responses, BaseURL: base, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	code, out := setupCall(t, srv, tok, "GET", "/api/providers/models?provider=account", "")
	if code != 422 || !strings.Contains(fmt.Sprint(out["error"]), "重新登录") || calls != 0 {
		t.Fatalf("missing credential: %d %v calls=%d", code, out, calls)
	}
	backend, err := config.OAuthVault(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(base)
	err = vault.SetOAuth(backend, "chatgpt", vault.OAuthCredential{AccessToken: "scripted-account-token", RefreshToken: "scripted-refresh", ExpiresAt: time.Now().Add(time.Hour)}, []string{u.Hostname()})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	code, out = setupCall(t, srv, tok, "GET", "/api/providers/models?provider=account", "")
	t.Logf("scripted SIWC catalog HTTP round trip: %s", time.Since(started))
	raw, _ := json.Marshal(out)
	var page struct {
		Models []config.ModelInfo `json:"models"`
	}
	_ = json.Unmarshal(raw, &page)
	if code != 200 || len(page.Models) != len(names)+3 || calls != 1 {
		t.Fatalf("SIWC catalog: %d %s calls=%d", code, raw, calls)
	}
	if page.Models[0].Model != "gpt-5.4" || page.Models[0].Context != 1000000 || !page.Models[0].HostedSearch {
		t.Fatalf("SIWC capabilities: %s", raw)
	}
	for _, m := range page.Models {
		if m.Model == "runtime-only" {
			if m.Context != 272000 || m.Vision || m.HostedSearch {
				t.Fatalf("unknown SIWC model: %+v", m)
			}
			continue
		}
		if m.Protocol != config.Responses || !m.Vision || !m.HostedSearch || !m.Available {
			t.Errorf("SIWC model capabilities: %+v", m)
		}
		if m.Model == "gpt-5.6" {
			if m.Context != 272000 {
				t.Errorf("catalog-only model window: %d", m.Context)
			}
			continue
		}
		endpoint, _, err := config.Resolve(s.Root, "account", m.Model, config.Getenv)
		if err != nil || !endpoint.Vision || !endpoint.HostedSearch || endpoint.Protocol != m.Protocol {
			t.Errorf("SIWC runtime capability mismatch for %s: %v", m.Model, err)
		}
	}
	if strings.Contains(string(raw), "scripted-account-token") {
		t.Fatal("credential leaked")
	}
	saved, _ := config.Load(s.Root)
	if len(saved.Providers[0].Models) != len(models) {
		t.Fatal("catalog changed registry")
	}
	info, err := vault.OAuthMetadata(context.Background(), backend, "chatgpt")
	if err != nil || !info.Configured {
		t.Fatal("catalog changed credential")
	}
}

// TestProviderModelsRefreshRejectedAsksRelogin covers the xAI path where the
// stored credential is near expiry and the provider refuses the refresh: the
// catalog route must answer with a re-login error, not a network error, and
// must not leak the upstream body or tokens.
func TestProviderModelsRefreshRejectedAsksRelogin(t *testing.T) {
	up := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
				return
			}
			http.NotFound(w, r)
		}))
	}
	rejected := up(400, `{"error":"invalid_grant","refresh_token":"leaked-upstream-secret"}`)
	defer rejected.Close()
	for _, tc := range []struct {
		name, tokenURL, want string
		status               int
	}{
		{"rejected refresh asks for re-login", rejected.URL + "/token", "登录凭证已失效，请重新登录", 422},
		{"network failure stays generic", "http://127.0.0.1:1/token", "拉取模型失败", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
				t.Error("catalog endpoint should not be reached")
			})
			base := config.FromEnv(config.Getenv).BaseURL
			seed := config.Provider{Name: "xai", OAuth: "xai", Protocol: config.Responses, BaseURL: base, Models: []config.ModelSpec{{Name: "grok-4", Context: 256000}}}
			if err := config.SaveOAuthProvider(s.Root, seed); err != nil {
				t.Fatal(err)
			}
			backend, err := config.OAuthVault(s.Root)
			if err != nil {
				t.Fatal(err)
			}
			host := strings.TrimPrefix(base, "http://")
			if i := strings.LastIndex(host, ":"); i >= 0 {
				host = host[:i]
			}
			// Near expiry: the catalog fetch must refresh first.
			err = vault.SetOAuth(backend, "xai", vault.OAuthCredential{AccessToken: "expired-access", RefreshToken: "expired-refresh", ExpiresAt: time.Now().Add(-time.Minute), TokenURL: tc.tokenURL, ClientID: "grok-cli-public"}, []string{host})
			if err != nil {
				t.Fatal(err)
			}
			code, out := setupCall(t, srv, tok, "GET", "/api/providers/models?provider=xai", "")
			if code != tc.status {
				t.Fatalf("status %d", code)
			}
			msg, _ := out["error"].(string)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("error %q wants %q", msg, tc.want)
			}
			if strings.Contains(msg, "leaked-upstream-secret") || strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "expired-refresh") {
				t.Fatalf("upstream body or token leaked: %q", msg)
			}
		})
	}
}

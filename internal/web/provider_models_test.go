package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"xbot/internal/compact"
	"xbot/internal/config"
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
			path := "/api/providers/models"
			if mode == "pagination-loop" {
				path += "?provider=vendor"
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

// ChatGPT is a subscription placeholder in this batch. Keep the new vendor
// catalog API out of the existing subscription/OAuth path entirely.
func TestProviderModelsDefersChatGPTSubscription(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) { t.Error("subscription must not fetch a vendor catalog") })
	err := config.SaveOAuthProvider(s.Root, config.Provider{Name: "account", OAuth: "chatgpt", Protocol: config.Responses, BaseURL: config.FromEnv(config.Getenv).BaseURL, Models: []config.ModelSpec{{Name: "account-model", Context: 128000}}})
	if err != nil {
		t.Fatal(err)
	}
	code, out := setupCall(t, srv, tok, "GET", "/api/providers/models?provider=account", "")
	if code != 422 || !strings.Contains(fmt.Sprint(out["error"]), "后做") {
		t.Fatalf("subscription boundary: %d %v", code, out)
	}
}

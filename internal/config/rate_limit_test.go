package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"xbot/internal/provider"
)

// This tests the production config -> selector -> shared retry -> HTTP seam,
// including a fallback that itself has a configured fallback (never followed).
func TestConfiguredRateLimitSelector(t *testing.T) {
	for _, backupMode := range []string{"none", "success", "quota"} {
		t.Run(backupMode, func(t *testing.T) {
			calls := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				calls[req.Model]++
				if req.Model == "flash" && backupMode == "success" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"finished"},"finish_reason":"stop"}]}`)
					return
				}
				w.Header().Set("Retry-After", "7200")
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"code":"usage_limit_reached"}}`)
			}))
			defer server.Close()
			root := t.TempDir()
			p := Provider{Name: "primary", BaseURL: server.URL, APIKeyEnv: "TEST_KEY", RateLimitWaitSeconds: 1, Models: []ModelSpec{{Name: "main"}}}
			if backupMode != "none" {
				p.Fallback = &FallbackBinding{Provider: "backup", Model: "flash"}
			}
			b := Provider{Name: "backup", BaseURL: server.URL, APIKeyEnv: "TEST_KEY", Models: []ModelSpec{{Name: "flash"}}, Fallback: &FallbackBinding{Provider: "primary", Model: "main"}}
			if err := Save(root, File{Providers: []Provider{p, b}}); err != nil {
				t.Fatal(err)
			}
			getenv := func(string) string { return "test" }
			endpoint, _, err := Resolve(root, "primary", "main", getenv)
			if err != nil {
				t.Fatal(err)
			}
			primary, err := provider.New(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			selector, err := ModelSelector(root, "primary", primary, getenv)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := selector.Select(context.Background(), provider.Hint{Bot: "routine"})
			if err != nil {
				t.Fatal(err)
			}
			msg, err := selected.Chat(context.Background(), provider.Request{}, nil)
			if backupMode == "success" {
				if err != nil || msg.Text() != "finished" {
					t.Fatalf("%v %v", msg, err)
				}
				_, err = selected.Chat(context.Background(), provider.Request{}, nil)
				if err != nil || calls["flash"] != 2 {
					t.Fatalf("did not stay on backup: %v %v", calls, err)
				}
			} else if !errors.Is(err, provider.ErrRateLimitWait) {
				t.Fatalf("want terminal limit error: %v", err)
			}
			if calls["main"] != 1 || (backupMode == "none" && calls["flash"] != 0) || (backupMode == "quota" && calls["flash"] != 1) {
				t.Fatalf("unexpected routes: %v", calls)
			}
		})
	}
}

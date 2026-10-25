package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"xbot/internal/config"
)

var newBrands = []string{"gemini", "vertex", "xai", "kimi", "kimi-code", "zhipu", "zai", "minimax", "stepfun", "mimo", "qianfan", "tencent"}

func TestVendorIconsServed(t *testing.T) {
	_, srv, _ := setupServer(t, false, func(http.ResponseWriter, *http.Request) {})
	for _, b := range append([]string{"deepseek", "openai", "anthropic", "openrouter"}, newBrands...) {
		if raw, err := staticFS.ReadFile("static/provider-icons/" + b + ".svg"); err != nil || !strings.Contains(string(raw), "<svg") {
			t.Fatalf("%s: icon missing", b)
		}
		res, err := srv.Client().Get(srv.URL + "/provider-icons/" + b + ".svg")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/svg+xml" {
			t.Fatalf("%s: %d %s", b, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}

func TestVendorKeySaveListAndModels(t *testing.T) {
	const secret = "xai-test-secret-0123456789"
	var auth string
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"data":[{"id":"grok-4"},{"id":"grok-5"}]}`)
	})
	t.Setenv("XAI_API_KEY", "")
	os.Unsetenv("XAI_API_KEY")
	t.Cleanup(func() { config.ForgetKeys() })

	code, out := setupCall(t, srv, tok, "GET", "/api/providers", "")
	if code != 200 {
		t.Fatal(code)
	}
	vendors, _ := out["vendors"].([]any)
	if len(vendors) != len(newBrands) {
		t.Fatalf("vendors: %v", out["vendors"])
	}
	for i, v := range vendors {
		row := v.(map[string]any)
		if row["brand"] != newBrands[i] {
			t.Fatalf("order: %v", row)
		}
		if _, leaked := row["api_key_env"]; leaked || row["base_url"] != nil {
			t.Fatalf("vendor row exposes engineering fields: %v", row)
		}
	}
	for _, c := range []struct {
		brand, body string
		want        int
	}{
		{"nope", `{"key":"sk-0123456789"}`, 404},
		{"vertex", `{"key":"sk-0123456789"}`, 409},
		{"xai", `{"key":"short"}`, 400},
		{"xai", `{"key":"has space 0123456789"}`, 400},
		{"xai", `{`, 400},
	} {
		if code, _ := setupCall(t, srv, tok, "PUT", "/api/providers/"+c.brand+"/key", c.body); code != c.want {
			t.Errorf("%s %s: %d, want %d", c.brand, c.body, code, c.want)
		}
	}
	if f, _ := config.Load(s.Root); len(f.Providers) != 0 {
		t.Fatalf("refused saves wrote providers: %+v", f)
	}
	if res, _ := srv.Client().Do(mustReq(t, "PUT", srv.URL+"/api/providers/xai/key", `{"key":"`+secret+`"}`)); res.StatusCode != 401 {
		t.Fatalf("unauthenticated save: %d", res.StatusCode)
	}

	// Saving twice (a double click) leaves one entry and one env line.
	for i := 0; i < 2; i++ {
		if code, out := setupCall(t, srv, tok, "PUT", "/api/providers/xai/key", `{"key":"`+secret+`"}`); code != 200 || out["provider"] != "xai" {
			t.Fatalf("save: %d %v", code, out)
		}
	}
	f, _ := config.Load(s.Root)
	if len(f.Providers) != 1 || f.Providers[0].BaseURL != "https://api.x.ai/v1" || f.Providers[0].APIKeyEnv != "XAI_API_KEY" {
		t.Fatalf("registry: %+v", f)
	}
	reg, _ := os.ReadFile(config.Path(s.Root))
	env, _ := os.ReadFile(config.EnvFile(s.Root))
	if strings.Contains(string(reg), secret) || strings.Count(string(env), "XAI_API_KEY=") != 1 || !strings.Contains(string(env), "XAI_API_KEY="+secret) {
		t.Fatalf("key placement wrong:\nreg=%s\nenv has key=%v", reg, strings.Contains(string(env), secret))
	}

	code, out = setupCall(t, srv, tok, "GET", "/api/providers", "")
	var row map[string]any
	for _, p := range out["providers"].([]any) {
		if p.(map[string]any)["name"] == "xai" {
			row = p.(map[string]any)
		}
	}
	if row == nil || row["brand"] != "xai" || row["key_configured"] != true {
		t.Fatalf("configured row: %v", row)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), secret) {
		t.Fatal("key leaked into the provider list")
	}

	// Point the saved entry at the scripted upstream and refresh its models.
	f.Providers[0].BaseURL = strings.TrimSuffix(os.Getenv("XBOT_BASE_URL"), "/v1") + "/v1"
	if err := config.Save(s.Root, f); err != nil {
		t.Fatal(err)
	}
	code, out = setupCall(t, srv, tok, "GET", "/api/providers/models?provider=xai", "")
	raw, _ = json.Marshal(out)
	if code != 200 || auth != "Bearer "+secret || strings.Contains(string(raw), secret) || strings.Count(string(raw), `"model":"grok-`) != 2 {
		t.Fatalf("models: %d auth=%q %s", code, auth, raw)
	}
}

func TestProviderBrandFollowsHost(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(http.ResponseWriter, *http.Request) {})
	t.Setenv("MY_KEY", "k-0123456789")
	for name, base := range map[string]string{"mine": "https://api.minimax.io/v1", "other": "https://llm.example.com/v1", "oai": "https://api.openai.com/v1"} {
		if err := config.Add(s.Root, config.Provider{Name: name, BaseURL: base, APIKeyEnv: "MY_KEY"}, config.ModelSpec{Name: "m", Context: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	_, out := setupCall(t, srv, tok, "GET", "/api/providers", "")
	got := map[string]any{}
	for _, p := range out["providers"].([]any) {
		row := p.(map[string]any)
		got[row["name"].(string)] = row["brand"]
	}
	if got["mine"] != "minimax" || got["oai"] != "openai" || got["other"] != nil {
		t.Fatalf("brands: %v", got)
	}
}

func mustReq(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

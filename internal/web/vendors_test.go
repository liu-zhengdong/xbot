package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/provider"
)

var newBrands = []string{"gemini", "vertex", "xai", "kimi", "kimi-code", "zhipu", "zai", "minimax", "stepfun", "mimo", "qianfan", "tencent", "opencode-go"}

func TestXAIKeySaveWhileOAuthOwnsName(t *testing.T) {
	for _, scenario := range []string{"new", "existing", "numbered", "numbered-new"} {
		t.Run(scenario, func(t *testing.T) {
			s, srv, tok := setupServer(t, false, func(http.ResponseWriter, *http.Request) {})
			t.Cleanup(config.ForgetKeys)
			oauth := config.Provider{Name: "xai", OAuth: "xai", Protocol: config.Responses, BaseURL: "https://api.x.ai/v1", Models: []config.ModelSpec{{Name: "grok-oauth", Context: 12345}}}
			if err := config.SaveOAuthProvider(s.Root, oauth); err != nil {
				t.Fatal(err)
			}
			want := "xai-key"
			if scenario == "numbered" || scenario == "numbered-new" {
				if err := config.Add(s.Root, config.Provider{Name: "xai-key", BaseURL: "https://example.com/v1", APIKeyEnv: "OTHER_KEY"}, config.ModelSpec{Name: "other", Context: 12345}); err != nil {
					t.Fatal(err)
				}
				want = "xai-key-2"
			}
			if scenario == "existing" || scenario == "numbered" {
				if err := config.Add(s.Root, config.Provider{Name: want, Protocol: config.ChatCompletions, BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY"}, config.ModelSpec{Name: "custom-model", Context: 12345}); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				code, out := setupCall(t, srv, tok, "PUT", "/api/providers/xai/key", `{"key":"scripted-test-key"}`)
				if code != 200 || out["provider"] != want {
					t.Fatalf("save: %d %v", code, out)
				}
			}
			f, err := config.Load(s.Root)
			if err != nil {
				t.Fatal(err)
			}
			var key, account *config.Provider
			for i := range f.Providers {
				p := &f.Providers[i]
				if p.Name == want {
					key = p
				}
				if p.Name == "xai" {
					account = p
				}
			}
			if key == nil || key.APIKeyEnv != "XAI_API_KEY" || account == nil || account.OAuth != "xai" || ((scenario == "existing" || scenario == "numbered") && key.Models[0].Name != "custom-model") {
				t.Fatal("bindings changed or missing")
			}
			env, err := os.ReadFile(config.EnvFile(s.Root))
			if err != nil || !strings.Contains(string(env), "XAI_API_KEY=scripted-test-key") {
				t.Fatal("key not saved")
			}
			if err := config.RemoveOAuthProvider(s.Root, "xai"); err != nil {
				t.Fatal(err)
			}
			f, err = config.Load(s.Root)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range f.Providers {
				if p.Name == want && p.APIKeyEnv == "XAI_API_KEY" {
					found = true
				}
			}
			if !found {
				t.Fatal("logout removed key binding")
			}
			code, out := setupCall(t, srv, tok, "PUT", "/api/providers/xai/key", `{"key":"scripted-test-key"}`)
			if code != 200 || out["provider"] != want {
				t.Fatalf("save after logout: %d %v", code, out)
			}
		})
	}
}

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

// Claude 星芒只给技能导入来源行用，不是供应商品牌，路由单独注册。
func TestSkillSourceClaudeIconServed(t *testing.T) {
	_, srv, _ := setupServer(t, false, func(http.ResponseWriter, *http.Request) {})
	for _, b := range providerBrands() {
		if b == "claude" {
			t.Fatal("claude must not be a provider brand")
		}
	}
	raw, err := staticFS.ReadFile("static/provider-icons/claude.svg")
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Get(srv.URL + "/provider-icons/claude.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if string(body) != string(raw) || !strings.Contains(string(body), "<svg") || !strings.Contains(string(body), "<title>Claude</title>") || !strings.Contains(string(body), "M4.709 15.955") {
		t.Fatalf("body is not the Claude starburst: %.80s", body)
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

// An existing entry that only shares the vendor's name must neither take the
// vendor's key nor be sent it.
func TestVendorKeyRefusesSameNameOtherEndpoint(t *testing.T) {
	const oldKey, newKey = "minimax-old-key-0123456789", "xai-new-key-0123456789"
	var seen []string
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
	})
	t.Setenv("XAI_API_KEY", "")
	os.Unsetenv("XAI_API_KEY")
	t.Setenv("MM_KEY", oldKey)
	if err := config.Add(s.Root, config.Provider{Name: "xai", Protocol: config.ChatCompletions, BaseURL: "https://api.minimax.io/v1", APIKeyEnv: "MM_KEY"}, config.ModelSpec{Name: "m", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	// Point it at the scripted upstream so a leak would be visible.
	f, _ := config.Load(s.Root)
	f.Providers[0].BaseURL = os.Getenv("XBOT_BASE_URL")
	if err := config.Save(s.Root, f); err != nil {
		t.Fatal(err)
	}
	code, out := setupCall(t, srv, tok, "PUT", "/api/providers/xai/key", `{"key":"`+newKey+`"}`)
	if code != 409 || strings.Contains(fmt.Sprint(out), newKey) {
		t.Fatalf("save: %d %v", code, out)
	}
	if code, _ := setupCall(t, srv, tok, "POST", "/api/providers/xai/sync", ""); code != 409 {
		t.Fatalf("sync: %d", code)
	}
	env, _ := os.ReadFile(config.EnvFile(s.Root))
	after, _ := config.Load(s.Root)
	if strings.Contains(string(env), newKey) || os.Getenv("MM_KEY") != oldKey || len(after.Providers) != 1 || after.Providers[0].APIKeyEnv != "MM_KEY" || after.Providers[0].BaseURL != f.Providers[0].BaseURL {
		t.Fatalf("existing entry changed: %+v env=%q", after, env)
	}
	for _, a := range seen {
		if strings.Contains(a, newKey) {
			t.Fatal("new key sent to the other endpoint")
		}
	}
	if len(seen) != 0 {
		t.Fatalf("upstream contacted %d times", len(seen))
	}
}

// Save a key, fetch models beyond the seed, bind one, and chat with it.
func TestVendorFetchedModelCanBeBoundAndChatted(t *testing.T) {
	for _, brand := range []string{"xai", "opencode-go"} {
		t.Run(brand, func(t *testing.T) { testVendorFetchedModelCanBeBoundAndChatted(t, brand) })
	}
}

func testVendorFetchedModelCanBeBoundAndChatted(t *testing.T, brand string) {
	v, ok := config.VendorFor(brand)
	if !ok {
		t.Fatalf("vendor %s not registered", brand)
	}
	if brand == "opencode-go" && (v.Title != "OpenCode Go" || v.Protocol != config.ChatCompletions || v.BaseURL != "https://opencode.ai/zen/go/v1" || v.KeyEnv != "OPENCODE_API_KEY" || v.KeyLogin || config.BrandForBase(v.BaseURL) != brand) {
		t.Fatal("OpenCode Go must use its built-in Bearer key route")
	}
	const secret = "xai-test-secret-0123456789"
	var auth, chatModel string
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer "+secret {
				t.Error("model catalog request missing vendor Bearer key")
			}
			fmt.Fprint(w, `{"data":[{"id":"grok-5","context_length":1048576},{"id":"grok-5-mini"}]}`)
		case "/v1/chat/completions":
			var in struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			auth, chatModel = r.Header.Get("Authorization"), in.Model
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	})
	t.Setenv(v.KeyEnv, "")
	os.Unsetenv(v.KeyEnv)
	t.Cleanup(config.ForgetKeys)
	for i := range config.Vendors {
		if config.Vendors[i].Brand == brand {
			saved := config.Vendors[i].BaseURL
			config.Vendors[i].BaseURL = os.Getenv("XBOT_BASE_URL")
			t.Cleanup(func() { config.Vendors[i].BaseURL = saved })
		}
	}
	b, err := bot.Create(s.Root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := setupCall(t, srv, tok, "PUT", "/api/providers/"+brand+"/key", `{"key":"`+secret+`"}`); code != 200 {
		t.Fatalf("save: %d", code)
	}
	registered, _, err := config.Lookup(s.Root, brand, v.Models[0].Name)
	if err != nil || registered.APIKeyEnv != v.KeyEnv || registered.OAuth != "" || registered.WireProtocol() != v.Protocol || registered.BaseURL != os.Getenv("XBOT_BASE_URL") {
		t.Fatal("key save did not register the vendor defaults")
	}
	if code, out := setupCall(t, srv, tok, "GET", "/api/providers/models?provider="+brand, ""); code != 200 || len(out["models"].([]any)) != 2 {
		t.Fatalf("live models: %d %v", code, out)
	}
	if code, _ := setupCall(t, srv, tok, "PUT", "/api/bots/ada/binding", `{"provider":"`+brand+`","model":"grok-9"}`); code != 400 {
		t.Fatalf("model the vendor does not serve was bound: %d", code)
	}
	if code, out := setupCall(t, srv, tok, "POST", "/api/providers/"+brand+"/sync", ""); code != 200 || len(out["models"].([]any)) != 2 {
		t.Fatalf("sync: %d %v", code, out)
	}
	if _, m, err := config.Lookup(s.Root, brand, "grok-5"); err != nil || m.Context != 1_048_576 {
		t.Fatalf("synced context=%d err=%v", m.Context, err)
	}
	if code, out := setupCall(t, srv, tok, "PUT", "/api/bots/ada/binding", `{"provider":"`+brand+`","model":"grok-5"}`); code != 200 {
		t.Fatalf("bind fetched model: %d %v", code, out)
	}
	visible, _ := config.VisibleModels(s.Root, true)
	found := false
	for _, m := range visible {
		found = found || m.Provider == brand && m.Model == "grok-5"
	}
	if !found {
		t.Fatal("fetched model missing from the chat picker")
	}
	cfg, err := bot.Load(s.Root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	ep, _, err := config.Resolve(s.Root, cfg.Config().Provider, cfg.Config().Model, config.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	p, err := provider.New(provider.Endpoint{Protocol: config.ChatCompletions, APIKey: ep.APIKey, BaseURL: ep.BaseURL, Model: ep.Model})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := p.Chat(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: provider.TextContent("hi")}}}, nil)
	if err != nil || chatModel != "grok-5" || auth != "Bearer "+secret || !strings.Contains(fmt.Sprint(reply.Content), "hello") {
		t.Fatalf("chat: %v model=%q auth=%q %v", err, chatModel, auth, reply)
	}
}

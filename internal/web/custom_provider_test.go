package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
)

func customJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func customRow(t *testing.T, out map[string]any, name string) map[string]any {
	t.Helper()
	rows, _ := out["providers"].([]any)
	for _, value := range rows {
		row, _ := value.(map[string]any)
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("registered provider %q missing from API", name)
	return nil
}

// Exercise the registration seam through HTTP: saving an endpoint without a
// model must be enough to fetch a live-only model and bind a real stored Bot.
func TestCustomProviderAPIRegistrationReadBindUpdateDelete(t *testing.T) {
	const firstKey = "scripted-custom-first-0123456789"
	const nextKey = "scripted-custom-next-0123456789"
	calls := 0
	var auth, path string
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		fmt.Fprint(w, `{"data":[{"id":"runtime-custom-only","context_length":320000}]}`)
	})
	base := os.Getenv("XBOT_BASE_URL")
	started := time.Now()
	code, out := setupCall(t, srv, tok, "POST", "/api/providers/custom", customJSON(t, map[string]any{"base_url": base, "key": firstKey, "display_name": "我的模型站"}))
	if code != 200 && code != 201 {
		t.Fatalf("custom creation status=%d", code)
	}
	name, _ := out["provider"].(string)
	if name == "" {
		t.Fatal("creation returned no registered identity")
	}
	if calls != 0 {
		t.Fatal("saving unexpectedly requested models before the caller asked")
	}
	f, err := config.Load(s.Root)
	if err != nil || len(f.Providers) != 1 || f.Providers[0].Name != name || f.Providers[0].BaseURL != base || len(f.Providers[0].Models) != 0 {
		t.Fatalf("save did not register a model-free endpoint: error=%v count=%d", err, len(f.Providers))
	}
	keyEnv := f.Providers[0].APIKeyEnv
	if keyEnv == "" || config.Getenv(keyEnv) != firstKey || os.Getenv(keyEnv) != "" {
		t.Fatal("custom key did not use the held environment-name path")
	}
	reg, _ := os.ReadFile(config.Path(s.Root))
	env, _ := os.ReadFile(config.EnvFile(s.Root))
	if strings.Contains(string(reg), firstKey) || !strings.Contains(string(env), keyEnv+"="+firstKey) {
		t.Fatal("custom key placement differs from existing provider key storage")
	}
	if st, err := os.Stat(config.EnvFile(s.Root)); err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("custom credential file is not mode 0600")
	}
	code, out = setupCall(t, srv, tok, "GET", "/api/providers", "")
	row := customRow(t, out, name)
	if code != 200 || row["custom"] != true || row["display_name"] != "我的模型站" || row["title"] != "我的模型站" || row["base_url"] != base || row["key_configured"] != true || row["protocol"] != config.ChatCompletions {
		t.Fatal("saved custom endpoint is not readable by the grid and picker")
	}
	if strings.Contains(customJSON(t, out), firstKey) {
		t.Fatal("provider list leaked key")
	}
	code, out = setupCall(t, srv, tok, "GET", "/api/providers/models?provider="+url.QueryEscape(name), "")
	models, _ := out["models"].([]any)
	if code != 200 || len(models) != 1 || models[0].(map[string]any)["model"] != "runtime-custom-only" || auth != "Bearer "+firstKey || path != "/v1/models" {
		t.Fatal("saved custom registration could not read its authenticated live catalog")
	}
	b, err := bot.Create(s.Root, "custom-smoke", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{s.Owner}); err != nil {
		t.Fatal(err)
	}
	code, _ = setupCall(t, srv, tok, "PUT", "/api/bots/custom-smoke/binding", customJSON(t, map[string]string{"provider": name, "model": "runtime-custom-only"}))
	if code != 200 {
		t.Fatalf("live-only custom model binding status=%d", code)
	}
	bound, err := bot.Load(s.Root, "custom-smoke")
	if err != nil || bound.Config().Provider != name || bound.Config().Model != "runtime-custom-only" {
		t.Fatal("binding did not persist custom provider/model identity")
	}
	endpoint, window, err := config.Resolve(s.Root, name, "runtime-custom-only", config.Getenv)
	if err != nil || endpoint.BaseURL != base || endpoint.APIKey != firstKey || window != 320000 {
		t.Fatal("custom model did not resolve through the existing registry")
	}
	// Clearing the optional title and retaining the masked key must work.
	code, _ = setupCall(t, srv, tok, "PUT", "/api/providers/custom/"+name+"/config", customJSON(t, map[string]string{"base_url": base, "display_name": "", "key": "", "protocol": config.Responses}))
	if code != 200 || config.Getenv(keyEnv) != firstKey {
		t.Fatal("editing custom metadata lost the stored credential")
	}
	// Replacing address, display name and key keeps the stable binding identity.
	nextBase := strings.TrimSuffix(base, "/v1") + "/proxy"
	code, _ = setupCall(t, srv, tok, "PUT", "/api/providers/custom/"+name+"/config", customJSON(t, map[string]string{"base_url": nextBase, "display_name": "新的模型站", "key": nextKey, "protocol": config.ChatCompletions}))
	if code != 200 {
		t.Fatalf("custom update status=%d", code)
	}
	endpoint, _, err = config.Resolve(s.Root, name, "runtime-custom-only", config.Getenv)
	if err != nil || endpoint.BaseURL != nextBase || endpoint.APIKey != nextKey {
		t.Fatal("custom update bypassed the shared resolution path")
	}
	code, _ = setupCall(t, srv, tok, "GET", "/api/providers/models?provider="+url.QueryEscape(name), "")
	if code != 200 || auth != "Bearer "+nextKey || path != "/proxy/models" {
		t.Fatal("updated custom endpoint did not serve the live catalog")
	}
	code, _ = setupCall(t, srv, tok, "DELETE", "/api/providers/custom/"+name, "")
	if code != 200 && code != 204 {
		t.Fatalf("custom deletion status=%d", code)
	}
	if _, _, err := config.Resolve(s.Root, name, "runtime-custom-only", config.Getenv); err == nil {
		t.Fatal("deleted endpoint still resolves")
	}
	env, _ = os.ReadFile(config.EnvFile(s.Root))
	if config.Getenv(keyEnv) != "" || strings.Contains(string(env), keyEnv+"=") {
		t.Fatal("deleting a custom endpoint retained its owned key")
	}
	t.Logf("scripted HTTP registration → catalog → binding → edit → delete: %s", time.Since(started))
}

func TestCustomProviderHostInferenceAndIndependentRegistration(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(http.ResponseWriter, *http.Request) {
		t.Error("host inference/registration must not call upstream")
	})
	for _, tc := range []struct{ base, brand, title, protocol string }{
		{"https://API.OPENAI.COM:443/v1", "openai", "OpenAI", config.Responses},
		{"https://api.anthropic.com/v1", "anthropic", "Anthropic", config.AnthropicMessages},
		{"https://api.deepseek.com/v1", "deepseek", "DeepSeek", config.ChatCompletions},
		{"https://unknown.example/v1", "", "自定义供应商", config.ChatCompletions},
		{"https://api.openai.com.evil.example/v1", "", "自定义供应商", config.ChatCompletions},
	} {
		code, out := setupCall(t, srv, tok, "GET", "/api/providers/infer?base_url="+url.QueryEscape(tc.base), "")
		if code != 200 || out["protocol"] != tc.protocol || out["title"] != tc.title || fmt.Sprint(out["brand"]) != tc.brand && !(tc.brand == "" && out["brand"] == nil) {
			t.Errorf("infer %s did not match the protocol/title table", tc.base)
		}
	}
	for _, v := range config.Vendors {
		if len(v.Hosts) == 0 {
			continue
		}
		host := v.Hosts[0]
		if strings.HasPrefix(host, ".") {
			host = "test" + host
		}
		code, out := setupCall(t, srv, tok, "GET", "/api/providers/infer?base_url="+url.QueryEscape("https://"+host+"/v1"), "")
		if code != 200 || out["brand"] != v.Brand || out["title"] != v.Title || out["protocol"] != v.Protocol {
			t.Errorf("infer table drifted from built-in vendor %s", v.Brand)
		}
	}
	if err := config.Add(s.Root, config.Provider{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "EXISTING_OPENAI_KEY"}, config.ModelSpec{Name: "existing", Context: 128000}); err != nil {
		t.Fatal(err)
	}
	var customNames []string
	for _, protocol := range []string{"", config.ChatCompletions} {
		code, out := setupCall(t, srv, tok, "POST", "/api/providers/custom", customJSON(t, map[string]string{"base_url": "https://api.openai.com/v1", "key": "scripted-independent-custom-key", "protocol": protocol}))
		if code != 200 && code != 201 {
			t.Fatalf("known-host custom create status=%d", code)
		}
		name, _ := out["provider"].(string)
		if name == "" || name == "openai" {
			t.Fatal("known-host creation merged into built-in identity")
		}
		customNames = append(customNames, name)
		_, list := setupCall(t, srv, tok, "GET", "/api/providers", "")
		row := customRow(t, list, name)
		wantProtocol := config.Responses
		if protocol != "" {
			wantProtocol = protocol
		}
		if row["custom"] != true || row["title"] != "OpenAI" || row["protocol"] != wantProtocol {
			t.Fatal("known-host custom display or manual protocol lost")
		}
		if customRow(t, list, "openai")["api_key_env"] != "EXISTING_OPENAI_KEY" {
			t.Fatal("custom create changed built-in credential path")
		}
	}
	if customNames[0] == customNames[1] {
		t.Fatal("multiple custom endpoints collapsed into one identity")
	}
}

func TestCustomProviderLegacyConfigRegistrationIsReadableBindableAndEditable(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"id":"cli-only"}]}`) })
	const legacyKeyName = "CUSTOM_CLI_SHARED_KEY"
	const legacyKey = "scripted-cli-key-0123456789"
	t.Setenv(legacyKeyName, legacyKey)
	config.HoldKeys([]string{legacyKeyName})
	// This is the same config.Add call shape used by `xbot provider add`.
	for _, name := range []string{"cli-alias", "shared-key-sibling"} {
		if err := config.Add(s.Root, config.Provider{Name: name, BaseURL: os.Getenv("XBOT_BASE_URL"), APIKeyEnv: legacyKeyName}, config.ModelSpec{Name: "cli-only", Context: 128000}); err != nil {
			t.Fatal(err)
		}
	}
	b, err := bot.Create(s.Root, "cli-smoke", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{s.Owner}); err != nil {
		t.Fatal(err)
	}
	code, out := setupCall(t, srv, tok, "GET", "/api/providers", "")
	if code != 200 || customRow(t, out, "cli-alias")["custom"] != true {
		t.Fatal("CLI/config-registered custom endpoint missing from editable list")
	}
	code, _ = setupCall(t, srv, tok, "PUT", "/api/bots/cli-smoke/binding", `{"provider":"cli-alias","model":"cli-only"}`)
	if code != 200 {
		t.Fatalf("CLI custom binding status=%d", code)
	}
	code, _ = setupCall(t, srv, tok, "PUT", "/api/providers/custom/cli-alias/config", customJSON(t, map[string]string{"base_url": os.Getenv("XBOT_BASE_URL"), "display_name": "CLI 模型站"}))
	if code != 200 {
		t.Fatalf("CLI custom edit status=%d", code)
	}
	code, _ = setupCall(t, srv, tok, "DELETE", "/api/providers/custom/cli-alias", "")
	if code != 200 && code != 204 {
		t.Fatalf("CLI custom delete status=%d", code)
	}
	if config.Getenv(legacyKeyName) != legacyKey {
		t.Fatal("deleting one custom endpoint removed a shared CLI key")
	}
	if _, _, err := config.Resolve(s.Root, "shared-key-sibling", "cli-only", config.Getenv); err != nil {
		t.Fatal("custom deletion broke sibling's existing configuration")
	}
}

func TestCustomProviderValidationAndModelSelection(t *testing.T) {
	s, srv, tok := setupServer(t, false, func(http.ResponseWriter, *http.Request) {
		t.Error("validation/model registration must not probe upstream")
	})
	for _, in := range []map[string]string{
		{"base_url": "not a URL", "key": "scripted-key-0123456789"},
		{"base_url": "ftp://example.com", "key": "scripted-key-0123456789"},
		{"base_url": "https://user:pass@example.com", "key": "scripted-key-0123456789"},
		{"base_url": "https://example.com/?key=private", "key": "scripted-key-0123456789"},
		{"base_url": "https://example.com", "key": ""},
		{"base_url": "https://example.com", "key": "scripted-key-0123456789", "protocol": "invalid"},
	} {
		code, _ := setupCall(t, srv, tok, "POST", "/api/providers/custom", customJSON(t, in))
		if code < 400 || code >= 500 {
			t.Errorf("invalid custom input status=%d", code)
		}
	}
	if f, _ := config.Load(s.Root); len(f.Providers) != 0 {
		t.Fatal("invalid input registered an endpoint")
	}
	body := customJSON(t, map[string]string{"base_url": "http://public-proxy.example/v1", "key": "scripted-http-key-0123456789"})
	res, err := srv.Client().Do(mustReq(t, "POST", srv.URL+"/api/providers/custom", body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauthenticated creation status=%d", res.StatusCode)
	}
	code, out := setupCall(t, srv, tok, "POST", "/api/providers/custom", body)
	if code != 200 && code != 201 {
		t.Fatalf("explicit public HTTP endpoint rejected, status=%d", code)
	}
	name, _ := out["provider"].(string)
	if name == "" {
		t.Fatal("HTTP registration returned no identity")
	}
	for _, enabled := range []bool{true, false} {
		code, _ := setupCall(t, srv, tok, "PUT", "/api/providers/custom/"+name+"/models", customJSON(t, map[string]any{"model": "manual-model", "enabled": enabled, "context": 64000}))
		if code != 200 {
			t.Fatalf("custom model selected=%v status=%d", enabled, code)
		}
		f, err := config.Load(s.Root)
		if err != nil || len(f.Providers) != 1 {
			t.Fatal("unselecting final model deleted its custom endpoint")
		}
		wantCount := 0
		if enabled {
			wantCount = 1
		}
		if len(f.Providers[0].Models) != wantCount {
			t.Fatal("model checkbox did not persist registration state")
		}
		if enabled {
			ep, window, err := config.Resolve(s.Root, name, "manual-model", config.Getenv)
			if err != nil || ep.BaseURL != "http://public-proxy.example/v1" || window != 64000 {
				t.Fatal("manually added model is not resolvable through config")
			}
		}
	}
}

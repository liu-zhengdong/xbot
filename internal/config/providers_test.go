package config

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestProviderAddListRemoveAndBind(t *testing.T) {
	root := t.TempDir()
	err := Add(root, Provider{
		Name: "deepseek", BaseURL: "https://api.deepseek.com/v1/", APIKeyEnv: "DEEPSEEK_API_KEY",
	}, ModelSpec{Name: "deepseek-chat", Context: 64000})
	if err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-reasoner", Context: 64000}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") || strings.Contains(string(raw), "super-secret") {
		t.Fatalf("registry stored a raw key:\n%s", raw)
	}
	if !strings.Contains(string(raw), "DEEPSEEK_API_KEY") {
		t.Fatalf("missing env reference:\n%s", raw)
	}
	f, err := Load(root)
	if err != nil || len(f.Providers) != 1 || len(f.Providers[0].Models) != 2 {
		t.Fatalf("%+v %v", f, err)
	}
	if strings.HasSuffix(f.Providers[0].BaseURL, "/") {
		t.Fatal("base url should be trimmed")
	}
	names := KeyEnvNames(root)
	if !strings.Contains(strings.Join(names, " "), "XBOT_API_KEY") || !strings.Contains(strings.Join(names, " "), "DEEPSEEK_API_KEY") {
		t.Fatalf("key env %v", names)
	}

	env := func(k string) string {
		if k == "DEEPSEEK_API_KEY" {
			return "super-secret"
		}
		return ""
	}
	got, n, err := Resolve(root, "deepseek", "deepseek-chat", env)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "super-secret" || got.Model != "deepseek-chat" || got.KeyEnv != "DEEPSEEK_API_KEY" || n != 64000 {
		t.Fatalf("%+v context %d", got, n)
	}
	if _, _, err := Resolve(root, "deepseek", "deepseek-chat", func(string) string { return "" }); err == nil {
		t.Fatal("missing key should fail at resolve time")
	}

	b, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("deepseek", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	loaded, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config()
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" {
		t.Fatalf("%+v", cfg)
	}
	botRaw, err := os.ReadFile(root + "/bots/ada/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(botRaw), "super-secret") || strings.Contains(string(botRaw), "DEEPSEEK_API_KEY") {
		t.Fatalf("bot file should store only the binding:\n%s", botRaw)
	}

	fallback, n, err := Resolve(root, "", "", func(string) string { return "" })
	if err != nil || fallback.Model != defaultModel || n != 0 {
		t.Fatalf("fallback %+v %d %v", fallback, n, err)
	}

	if err := Remove(root, "deepseek", "deepseek-reasoner"); err != nil {
		t.Fatal(err)
	}
	f, err = Load(root)
	if err != nil || len(f.Providers[0].Models) != 1 {
		t.Fatalf("%+v %v", f, err)
	}
	if err := Remove(root, "deepseek", ""); err != nil {
		t.Fatal(err)
	}
	f, err = Load(root)
	if err != nil || len(f.Providers) != 0 {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestModelTodoSwitchRoundTrip(t *testing.T) {
	root := t.TempDir()
	off := false
	err := Add(root, Provider{
		Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY",
	}, ModelSpec{Name: "deepseek-chat", Context: 64000, Todo: &off, TodoSteps: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-chat", Context: 32000}); err != nil {
		t.Fatal(err)
	}
	_, spec, err := Lookup(root, "deepseek", "deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Context != 32000 || spec.Todo == nil || *spec.Todo || spec.TodoSteps != 8 {
		t.Fatalf("update dropped the switch: %+v", spec)
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") {
		t.Fatalf("registry stored a raw key:\n%s", raw)
	}
}

func TestProviderRejectsRawKeyField(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(Path(root), []byte(`{"providers":[{"name":"x","base_url":"http://localhost","api_key":"secret","api_key_env":"X","models":[{"name":"m","context":1}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("api_key field must be rejected")
	}
}

func TestModelThinkingRoundTripAndValidation(t *testing.T) {
	root := t.TempDir()
	p := Provider{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"}
	if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 1000000, Thinking: "omit"}); err != nil {
		t.Fatal(err)
	}
	// A context-length update keeps the explicit thinking setting.
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 900000}); err != nil {
		t.Fatal(err)
	}
	env := func(string) string { return "k" }
	got, n, err := Resolve(root, "deepseek", "deepseek-flash", env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Thinking != "omit" || n != 900000 {
		t.Fatalf("%+v context %d", got, n)
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 1000, Thinking: "yes"}); err == nil {
		t.Fatal("thinking yes should be rejected")
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 1000, Thinking: "on"}); err != nil {
		t.Fatalf("thinking on rejected: %v", err)
	}
	if got, _, err := Resolve(root, "deepseek", "deepseek-flash", env); err != nil || got.Thinking != "on" {
		t.Fatalf("thinking on did not round-trip: %+v %v", got, err)
	}
	// A hand-edited bad value fails at resolve time rather than being guessed.
	f, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	f.Providers[0].Models[0].Thinking = "enabled"
	if err := Save(root, f); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(root, "deepseek", "deepseek-flash", env); err == nil {
		t.Fatal("bad thinking value should fail at resolve time")
	}
}

func TestFromEnvDefaultIsDeepSeekFlash(t *testing.T) {
	if m := FromEnv(func(string) string { return "" }); m.Model != "deepseek-flash" {
		t.Fatalf("default model %q", m.Model)
	}
}

// The provider key goes wherever base_url says, so a base_url that would leak
// it is refused on add and again on resolve, for a hand-edited file (#34).
func TestProviderBaseURLChecked(t *testing.T) {
	for base, ok := range map[string]bool{
		"https://api.deepseek.com/v1":        true,
		"https://gateway.example/v1":         true,
		"http://127.0.0.1:8080/v1":           true,
		"http://localhost:11434/v1":          true,
		"http://192.168.1.20/v1":             true,
		"http://[::1]:8080/v1":               true,
		"http://gateway.example/v1":          false,
		"http://8.8.8.8/v1":                  false,
		"https://user:pw@gateway.example/v1": false,
		"https://gateway.example/v1?k=1":     false,
		"https://gateway.example/v1#x":       false,
		"ftp://gateway.example/v1":           false,
		"gateway.example/v1":                 false,
	} {
		err := Add(t.TempDir(), Provider{Name: "p", BaseURL: base, APIKeyEnv: "P_KEY"}, ModelSpec{Name: "m", Context: 1})
		if (err == nil) != ok {
			t.Errorf("add %q: err %v, want ok=%v", base, err, ok)
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(Path(root), []byte(`{"providers":[{"name":"p","base_url":"http://evil.example/v1","api_key_env":"P_KEY","models":[{"name":"m","context":1}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Resolve(root, "p", "m", func(string) string { return "key-value" })
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("resolve of a hand-edited http base_url: %v", err)
	}
}

func TestReasoningEffortRoundTripAndValidation(t *testing.T) {
	root := t.TempDir()
	p := Provider{Name: "deepseek", BaseURL: "https://api.deepseek.com", APIKeyEnv: "TEST_KEY"}
	env := func(string) string { return "fake" }
	for _, effort := range []string{"low", "high", "max"} {
		if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 100000, Thinking: "on", ReasoningEffort: effort}); err != nil {
			t.Fatal(err)
		}
		// An unrelated update preserves the explicit effort.
		if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 200000}); err != nil {
			t.Fatal(err)
		}
		got, _, err := Resolve(root, "deepseek", "deepseek-flash", env)
		if err != nil || got.ReasoningEffort != effort {
			t.Fatalf("effort=%s got=%s err=%v", effort, got.ReasoningEffort, err)
		}
	}
	for _, bad := range []string{"medium", "minimal", "xhigh", "ultra"} {
		if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 1000, ReasoningEffort: bad}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	f, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	f.Providers[0].Models[0].ReasoningEffort = "medium"
	if err := Save(root, f); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(root, "deepseek", "deepseek-flash", env); err == nil {
		t.Fatal("hand-edited invalid effort accepted")
	}
}

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

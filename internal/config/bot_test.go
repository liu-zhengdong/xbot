package config_test

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/config"
)

func TestOAuthNamespaceCannotBeABot(t *testing.T) {
	if bot.ValidName(config.OAuthNamespace) {
		t.Fatal("OAuth namespace can collide with a Bot and lose its shared key")
	}
}

func TestBotFileStoresOnlyTheBinding(t *testing.T) {
	root := t.TempDir()
	if err := config.Add(root, config.Provider{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"}, config.ModelSpec{Name: "deepseek-chat", Context: 64000}); err != nil {
		t.Fatal(err)
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
	if cfg := loaded.Config(); cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" {
		t.Fatalf("%+v", cfg)
	}
	raw, err := os.ReadFile(root + "/bots/ada/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "DEEPSEEK_API_KEY") {
		t.Fatalf("bot file should store only the binding:\n%s", raw)
	}
}

func TestEnsureDefaultSeedsDeepSeekOnce(t *testing.T) {
	t.Setenv("XBOT_BASE_URL", "")
	t.Setenv("XBOT_MODEL", "")
	root := t.TempDir()
	p, err := config.EnsureDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "deepseek" || p.BaseURL != "https://api.deepseek.com/v1" || p.APIKeyEnv != "XBOT_API_KEY" || len(p.Models) != 1 || p.Models[0].Name != "deepseek-flash" {
		t.Fatalf("seed %+v", p)
	}
	before, err := os.ReadFile(config.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureDefault(root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(config.Path(root))
	if string(before) != string(after) {
		t.Fatalf("second call rewrote the registry:\n%s", after)
	}
	if p, err = config.EnsureDefault(root, "deepseek-chat"); err != nil || len(p.Models) != 2 {
		t.Fatalf("model not added %+v %v", p, err)
	}
	f, _ := config.Load(root)
	if len(f.Providers) != 1 {
		t.Fatalf("duplicate entries %+v", f)
	}
}

func TestNewBotBindsTheDeepSeekEntry(t *testing.T) {
	t.Setenv("XBOT_MODEL", "")
	t.Setenv("XBOT_API_KEY", "fake-ds")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := b.Config(); cfg.Provider != "deepseek" || cfg.Model != "deepseek-flash" {
		t.Fatalf("new bot binding %+v", cfg)
	}
	got, _, err := config.Resolve(root, "deepseek", "deepseek-flash", config.Getenv)
	if err != nil || got.BaseURL != "https://api.deepseek.com/v1" || got.KeyEnv != "XBOT_API_KEY" {
		t.Fatalf("%+v %v", got, err)
	}
	models, err := config.VisibleModels(root, true)
	if err != nil || len(models) != 1 || models[0].ID != "deepseek/deepseek-flash" {
		t.Fatalf("list %+v %v", models, err)
	}
}

func TestBindLegacyMovesUnboundBotsOntoDeepSeek(t *testing.T) {
	t.Setenv("XBOT_MODEL", "")
	t.Setenv("XBOT_API_KEY", "fake-ds")
	root := t.TempDir()
	for name, model := range map[string]string{"ada": "", "bo": "deepseek-chat"} {
		b, err := bot.Create(root, name, "Be brief.", nil)
		if err != nil {
			t.Fatal(err)
		}
		// A config.json written before providers.json was the only source.
		if err := b.SetBinding("", model); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Remove(root, "deepseek", ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := bot.BindLegacy(root); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"ada": "deepseek-flash", "bo": "deepseek-chat"} {
		b, err := bot.Load(root, name)
		if err != nil {
			t.Fatal(err)
		}
		cfg := b.Config()
		if cfg.Provider != "deepseek" || cfg.Model != want {
			t.Fatalf("%s bound %s/%s", name, cfg.Provider, cfg.Model)
		}
		if _, _, err := config.Resolve(root, cfg.Provider, cfg.Model, config.Getenv); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	f, _ := config.Load(root)
	if len(f.Providers) != 1 || len(f.Providers[0].Models) != 2 {
		t.Fatalf("registry %+v", f)
	}
}

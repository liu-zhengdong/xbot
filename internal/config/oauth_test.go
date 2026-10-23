package config

import (
	"testing"
	"xbot/internal/bot"
)

func TestOAuthNamespaceCannotBeABot(t *testing.T) {
	if bot.ValidName(oauthNamespace) {
		t.Fatal("OAuth namespace can collide with a Bot and lose its shared key")
	}
}

func TestOAuthReloginPreservesModelSettings(t *testing.T) {
	for _, kind := range []string{"openrouter", "chatgpt"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			protocol, base, model := ChatCompletions, "https://openrouter.ai/api/v1", "openai/gpt-5.4"
			if kind == "chatgpt" {
				protocol, base, model = Responses, "https://api.openai.com/v1", "gpt-5.4"
			}
			p := Provider{Name: kind, OAuth: kind, Protocol: protocol, BaseURL: base, Models: []ModelSpec{{Name: model, Context: 1000000}}}
			if err := SaveOAuthProvider(root, p); err != nil {
				t.Fatal(err)
			}
			f, _ := Load(root)
			yes, no := true, false
			f.Providers[0].Models[0].Vision = &yes
			f.Providers[0].Models[0].HostedSearch = &no
			f.Providers[0].Models[0].Thinking = "omit"
			if kind == "openrouter" {
				f.Providers[0].Models = append(f.Providers[0].Models, ModelSpec{Name: "another-scripted-model", Context: 50000})
			}
			if err := Save(root, f); err != nil {
				t.Fatal(err)
			}
			if err := SaveOAuthProvider(root, p); err != nil {
				t.Fatal(err)
			}
			got, _ := Load(root)
			m := got.Providers[0].Models[0]
			if m.Vision == nil || !*m.Vision || m.HostedSearch == nil || *m.HostedSearch || m.Thinking != "omit" {
				t.Fatal("relogin discarded model settings")
			}
			if kind == "openrouter" && len(got.Providers[0].Models) != 2 {
				t.Fatal("relogin removed a configured model")
			}
		})
	}
}

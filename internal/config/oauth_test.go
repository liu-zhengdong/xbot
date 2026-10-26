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

func TestValidateOAuthXAI(t *testing.T) {
	root := t.TempDir()
	p := Provider{Name: "xai", OAuth: "xai", Protocol: Responses, BaseURL: "https://api.x.ai/v1", Models: []ModelSpec{{Name: "grok-4", Context: 256000}}}
	if err := validateOAuth(p); err != nil {
		t.Fatal(err)
	}
	if err := SaveOAuthProvider(root, p); err != nil {
		t.Fatal(err)
	}
	// Loopback bases stay testable, like the other OAuth kinds.
	loopback := p
	loopback.BaseURL = "http://127.0.0.1:9/v1"
	if err := validateOAuth(loopback); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Provider{
		{Name: "xai", OAuth: "xai", Protocol: ChatCompletions, BaseURL: "https://api.x.ai/v1"},
		{Name: "xai", OAuth: "xai", Protocol: Responses, BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY"},
		{Name: "xai", OAuth: "xai", Protocol: Responses, BaseURL: "https://api.example.com/v1"},
	} {
		if err := validateOAuth(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	// grok models on a Responses endpoint default to hosted search; the API-key
	// Completions path stays off unless configured.
	if !(ModelSpec{Name: "grok-4"}).HasHostedSearch(Responses) {
		t.Fatal("grok lacks default hosted search on responses")
	}
	if (ModelSpec{Name: "grok-4"}).HasHostedSearch(ChatCompletions) {
		t.Fatal("grok hosted search leaked into completions")
	}
	off := false
	if (ModelSpec{Name: "grok-4", HostedSearch: &off}).HasHostedSearch(Responses) {
		t.Fatal("explicit override ignored")
	}
}

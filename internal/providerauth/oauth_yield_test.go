package providerauth

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/provider"
)

func TestOAuthBeginYieldsAPIKey(t *testing.T) {
	for _, kind := range []string{"xai", "openrouter"} {
		t.Run(kind, func(t *testing.T) {
			f := newFakeAuth(t)
			root := t.TempDir()
			key := config.Provider{Name: kind, APIKeyEnv: "EXISTING_API_KEY", Protocol: config.ChatCompletions, BaseURL: f.srv.URL + "/v1", Models: []config.ModelSpec{{Name: "fake/scripted", Context: 123456}, {Name: "extra-model", Context: 45678}}}
			occupied := key
			occupied.Name = kind + "-key"
			if err := config.Save(root, config.File{Providers: []config.Provider{key, occupied}}); err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			ln.Close()
			m := &Manager{Root: root, CallbackPort: port, Endpoints: map[string]Endpoints{kind: f.endpoints(kind)}}
			t.Cleanup(m.Close)
			beginAt := time.Now()
			start, err := m.Begin(kind)
			t.Logf("Begin with key migration: %s", time.Since(beginAt))
			if err != nil || start.URL == "" {
				t.Fatalf("Begin with same-name API key: %v", err)
			}
			key.Name = kind + "-key-2"
			checkKeys := func(wantCount int) {
				t.Helper()
				got, err := config.Load(root)
				if err != nil || len(got.Providers) != wantCount {
					t.Fatalf("unexpected registry after migration: %v", err)
				}
				if !reflect.DeepEqual(got.Providers[:2], []config.Provider{key, occupied}) {
					t.Fatal("migration changed key fields or occupied binding")
				}
			}
			checkKeys(2)
			second, err := m.Begin(kind)
			if err != nil || second.URL != start.URL {
				t.Fatalf("repeated Begin did not reuse pending login: %v", err)
			}
			checkKeys(2)
			if err := m.Cancel(kind); err != nil {
				t.Fatal(err)
			}
			checkKeys(2)
			if body := f.signIn(m, kind); !strings.Contains(body, "登录成功") {
				t.Fatal("scripted OAuth callback failed")
			}
			checkKeys(3)
			got, _ := config.Load(root)
			oauth := got.Providers[2]
			wantProtocol := config.Responses
			if kind == "openrouter" {
				wantProtocol = config.ChatCompletions
			}
			if oauth.Name != kind || oauth.OAuth != kind || oauth.APIKeyEnv != "" || oauth.Protocol != wantProtocol || len(oauth.Models) == 0 {
				t.Fatal("OAuth binding did not use its own credentials and protocol")
			}
			// Actually call the scripted Completions endpoint through the renamed key.
			ep, _, err := config.Resolve(root, key.Name, "fake/scripted", func(env string) string {
				if env == key.APIKeyEnv {
					return f.orKey
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			client, err := provider.New(ep)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := client.Chat(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}}, nil)
			if err != nil || msg.Text() != "oauth-ok" {
				t.Fatalf("renamed key cannot call scripted Completions: %v", err)
			}
			if err := m.Logout(kind); err != nil {
				t.Fatal(err)
			}
			checkKeys(2)
		})
	}
}

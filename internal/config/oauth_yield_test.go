package config

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"xbot/internal/fsutil"
)

func TestSaveOAuthYieldsAPIKey(t *testing.T) {
	for _, kind := range []string{"xai", "openrouter", "chatgpt"} {
		for occupied := 0; occupied <= 2; occupied++ {
			t.Run(fmt.Sprintf("%s/occupied-%d", kind, occupied), func(t *testing.T) {
				root := t.TempDir()
				yes, no := true, false
				key := Provider{Name: kind, APIKeyEnv: "EXISTING_API_KEY", Protocol: ChatCompletions, BaseURL: "http://127.0.0.1:9/custom", Models: []ModelSpec{
					{Name: "fake/scripted", Context: 123456, Todo: &yes, TodoSteps: 9, Thinking: "omit", ReasoningEffort: "high", Vision: &yes, HostedSearch: &no, HostedImageGen: &no, ImageGenModel: "custom-image"},
					{Name: "extra-model", Context: 34567},
				}}
				initial := File{Providers: []Provider{key}}
				wantName := kind + "-key"
				for n := 1; n <= occupied; n++ {
					other := key
					other.Name = wantName
					initial.Providers = append(initial.Providers, other)
					wantName = fmt.Sprintf("%s-key-%d", kind, n+1)
				}
				if err := Save(root, initial); err != nil {
					t.Fatal(err)
				}
				base, protocol := "https://api.x.ai/v1", Responses
				if kind == "openrouter" {
					base, protocol = "https://openrouter.ai/api/v1", ChatCompletions
				} else if kind == "chatgpt" {
					base = "https://api.openai.com/v1"
				}
				oauth := Provider{Name: kind, OAuth: kind, BaseURL: base, Protocol: protocol, Models: []ModelSpec{{Name: "fake/scripted", Context: 99999}}}
				for attempt := 0; attempt < 2; attempt++ {
					if err := SaveOAuthProvider(root, oauth); err != nil {
						t.Fatal(err)
					}
				}
				got, err := Load(root)
				if err != nil {
					t.Fatal(err)
				}
				initial.Providers[0].Name = wantName
				want := append(initial.Providers, oauth)
				if !reflect.DeepEqual(got.Providers, want) {
					t.Fatal("migration or repeated save changed provider fields, lost a binding, or duplicated a name")
				}
				ep, window, err := Resolve(root, wantName, "fake/scripted", func(env string) string {
					if env == key.APIKeyEnv {
						return "scripted-test-value"
					}
					return ""
				})
				if err != nil || ep.KeyEnv != key.APIKeyEnv || ep.Protocol != key.Protocol || ep.BaseURL != key.BaseURL || window != key.Models[0].Context {
					t.Fatalf("renamed key binding no longer resolves: %v", err)
				}
			})
		}
	}
}

func TestSaveOAuthConcurrentYield(t *testing.T) {
	root := t.TempDir()
	key := Provider{Name: "xai", APIKeyEnv: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1", Models: []ModelSpec{{Name: "key-model", Context: 12345}}}
	if err := Save(root, File{Providers: []Provider{key}}); err != nil {
		t.Fatal(err)
	}
	const count = 12
	var wg sync.WaitGroup
	for n := 0; n < count; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := Provider{Name: "xai", OAuth: "xai", Protocol: Responses, BaseURL: "https://api.x.ai/v1", Models: []ModelSpec{{Name: fmt.Sprintf("model-%d", n), Context: 12345}}}
			if err := SaveOAuthProvider(root, p); err != nil {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	f, err := Load(root)
	if err != nil || len(f.Providers) != 2 {
		t.Fatalf("concurrent save lost or duplicated bindings: %v", err)
	}
	key.Name = "xai-key"
	if !reflect.DeepEqual(f.Providers[0], key) || f.Providers[1].OAuth != "xai" || len(f.Providers[1].Models) != count {
		t.Fatal("concurrent save lost key fields or OAuth models")
	}
}

func TestOAuthYieldUsesRegistryLock(t *testing.T) {
	for _, save := range []bool{false, true} {
		t.Run(fmt.Sprintf("save-%t", save), func(t *testing.T) {
			root := t.TempDir()
			lock, err := fsutil.Lock(Path(root) + ".lock")
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()
			started, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				if save {
					done <- SaveOAuthProvider(root, Provider{Name: "xai", OAuth: "xai", Protocol: Responses, BaseURL: "https://api.x.ai/v1", Models: []ModelSpec{{Name: "grok-test", Context: 12345}}})
				} else {
					done <- PrepareOAuthProvider(root, "xai")
				}
			}()
			<-started
			select {
			case err := <-done:
				t.Fatalf("migration did not wait for registry lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			// Another writer installs both names while holding the lock. The
			// waiting operation must read these entries after acquiring it.
			key := Provider{Name: "xai", APIKeyEnv: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1", Models: []ModelSpec{{Name: "key-model", Context: 12345}}}
			occupied := key
			occupied.Name = "xai-key"
			if err := Save(root, File{Providers: []Provider{key, occupied}}); err != nil {
				t.Fatal(err)
			}
			if err := lock.Release(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("migration did not finish after registry lock released")
			}
			f, err := Load(root)
			wantCount := 2
			if save {
				wantCount++
			}
			if err != nil || len(f.Providers) != wantCount {
				t.Fatalf("migration lost concurrent writer's bindings: %v", err)
			}
			key.Name = "xai-key-2"
			if !reflect.DeepEqual(f.Providers[:2], []Provider{key, occupied}) {
				t.Fatal("migration used stale names or changed key settings")
			}
		})
	}
}

package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResolveCatalogWindow(t *testing.T) {
	for _, tc := range []struct {
		name         string
		window, want int
	}{
		{"deepseek-flash", 1_048_576, 1_048_576},
		{"deepseek-flash", 0, 256_000},
		{"unknown", 0, 256_000},
		{"unknown", -1, 256_000},
		{"known-small", 64_000, 64_000},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.name, tc.window), func(t *testing.T) {
			root := t.TempDir()
			p := Provider{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKeyEnv: "FAKE_CONTEXT_KEY", Models: []ModelSpec{{Name: tc.name, Context: tc.window}}}
			if err := Save(root, File{Providers: []Provider{p}}); err != nil {
				t.Fatal(err)
			}
			_, got, err := Resolve(root, p.Name, tc.name, func(string) string { return "scripted" })
			if err != nil || got != tc.want {
				t.Fatalf("window=%d want=%d err=%v", got, tc.want, err)
			}
		})
	}
}

func TestCatalogContextRefresh(t *testing.T) {
	for _, protocol := range []string{ChatCompletions, Responses, AnthropicMessages} {
		t.Run(protocol, func(t *testing.T) {
			root := t.TempDir()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/models" {
					t.Error("unexpected request path")
				}
				fmt.Fprint(w, `{"data":[{"id":"deepseek-flash","context_length":1048576},{"id":"runtime","context_length":524288},{"id":"no-metadata"}]}`)
			}))
			defer server.Close()
			t.Setenv("FAKE_CONTEXT_KEY", "scripted")
			on, off := true, false
			original := ModelSpec{Name: "deepseek-flash", Context: 128_000, Vision: &off, Todo: &on, TodoSteps: 9, Thinking: "off", HostedSearch: &off}
			p := Provider{Name: "fake", Protocol: protocol, BaseURL: server.URL, APIKeyEnv: "FAKE_CONTEXT_KEY", Models: []ModelSpec{original, {Name: "no-metadata", Context: 64_000}}}
			if err := Save(root, File{Providers: []Provider{p}}); err != nil {
				t.Fatal(err)
			}
			models, err := ProviderModels(context.Background(), root, p.Name)
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 3 || models[0].Context != 1_048_576 || models[1].Context != 64_000 || models[2].Context != 524288 || models[2].Registered {
				t.Fatalf("models=%+v", models)
			}
			saved, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			original.Context = 1_048_576
			if len(saved.Providers[0].Models) != 2 || !reflect.DeepEqual(saved.Providers[0].Models[0], original) {
				t.Fatal("refresh lost overrides or registered new models")
			}
			for i := 0; i < 3; i++ {
				_, window, err := Resolve(root, p.Name, "deepseek-flash", Getenv)
				if err != nil || window != 1_048_576 {
					t.Fatalf("resolve window=%d err=%v", window, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("Resolve fetched metadata")
			}
			before, _ := os.Stat(Path(root))
			if _, err := ProviderModels(context.Background(), root, p.Name); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(Path(root))
			if !os.SameFile(before, after) {
				t.Fatal("unchanged metadata rewrote registry")
			}
		})
	}
}

func TestCatalogInvalidContextDoesNotReplaceDirectory(t *testing.T) {
	for _, raw := range []string{"null", "0", "-1", "10000001", "9223372036854775808", "1.5", `"1048576"`, `{}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[{"id":"bad","context_length":%s},{"id":"good","context_length":1048576}]}`, raw)
			}))
			defer server.Close()
			t.Setenv("FAKE_CONTEXT_KEY", "scripted")
			p := Provider{Name: "fake", BaseURL: server.URL, APIKeyEnv: "FAKE_CONTEXT_KEY", Models: []ModelSpec{{Name: "bad", Context: 64000}, {Name: "good", Context: 128000}}}
			if err := Save(root, File{Providers: []Provider{p}}); err != nil {
				t.Fatal(err)
			}
			models, err := ProviderModels(context.Background(), root, p.Name)
			if err != nil || len(models) != 2 || models[0].Context != 64000 || models[1].Context != 1048576 {
				t.Fatalf("models=%+v err=%v", models, err)
			}
			saved, _ := Load(root)
			if saved.Providers[0].Models[0].Context != 64000 || saved.Providers[0].Models[1].Context != 1048576 {
				t.Fatal("incorrect persisted windows")
			}
		})
	}
}

func TestCatalogFailureDoesNotPartiallyRefresh(t *testing.T) {
	for _, mode := range []string{"second-page", "too-many", "oversize", "redirect", "invalid-id"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "second-page":
					if r.URL.Query().Get("after_id") == "" {
						fmt.Fprint(w, `{"data":[{"id":"deepseek-flash","context_length":1048576}],"has_more":true,"last_id":"deepseek-flash"}`)
						return
					}
					http.Error(w, "private-upstream-body", 500)
				case "too-many":
					fmt.Fprint(w, `{"data":[{"id":"deepseek-flash","context_length":1048576}`)
					for i := 0; i < 2048; i++ {
						fmt.Fprintf(w, `,{"id":"m%d"}`, i)
					}
					fmt.Fprint(w, `]}`)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
				case "redirect":
					http.Redirect(w, r, "/must-not-follow", 302)
				case "invalid-id":
					fmt.Fprint(w, `{"data":[{"id":"deepseek-flash","context_length":1048576},{"id":""}]}`)
				}
			}))
			defer server.Close()
			t.Setenv("FAKE_CONTEXT_KEY", "scripted")
			p := Provider{Name: "fake", Protocol: AnthropicMessages, BaseURL: server.URL, APIKeyEnv: "FAKE_CONTEXT_KEY", Models: []ModelSpec{{Name: "deepseek-flash", Context: 128000}}}
			if err := Save(root, File{Providers: []Provider{p}}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(Path(root))
			_, err := ProviderModels(context.Background(), root, p.Name)
			if err == nil || strings.Contains(err.Error(), "private-upstream-body") {
				t.Fatalf("error=%v", err)
			}
			after, _ := os.ReadFile(Path(root))
			if string(before) != string(after) {
				t.Fatal("failed catalog partially changed registry")
			}
		})
	}
}

func TestCatalogRefreshConcurrentEdits(t *testing.T) {
	for _, action := range []string{"edit", "replace-endpoint", "remove"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { close(entered) })
				<-release
				fmt.Fprint(w, `{"data":[{"id":"deepseek-flash","context_length":1048576}]}`)
			}))
			defer server.Close()
			defer close(release)
			t.Setenv("FAKE_CONTEXT_KEY", "scripted")
			p := Provider{Name: "fake", Protocol: Responses, BaseURL: server.URL, APIKeyEnv: "FAKE_CONTEXT_KEY"}
			if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 128000}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := ProviderModels(context.Background(), root, p.Name); done <- err }()
			<-entered
			switch action {
			case "edit":
				if err := SetImageGen(root, p.Name, "deepseek-flash", true, "custom-image"); err != nil {
					t.Fatal(err)
				}
				// Concurrent catalog-independent upserts use the same registry lock.
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						if err := Add(root, Provider{Name: p.Name}, ModelSpec{Name: fmt.Sprintf("extra-%d", i), Context: 64000}); err != nil {
							t.Error(err)
						}
					}(i)
				}
				wg.Wait()
			case "replace-endpoint":
				if err := Add(root, Provider{Name: p.Name, BaseURL: "http://127.0.0.1:9"}, ModelSpec{Name: "deepseek-flash", Context: 64000}); err != nil {
					t.Fatal(err)
				}
			case "remove":
				if err := Remove(root, p.Name, ""); err != nil {
					t.Fatal(err)
				}
			}
			release <- struct{}{}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			saved, _ := Load(root)
			if action == "remove" {
				if len(saved.Providers) != 0 {
					t.Fatal("resurrected removed provider")
				}
				return
			}
			m := saved.Providers[0].Models[0]
			if action == "edit" && (m.Context != 1048576 || m.HostedImageGen == nil || !*m.HostedImageGen || m.ImageGenModel != "custom-image" || len(saved.Providers[0].Models) != 9) {
				t.Fatal("lost concurrent edits")
			}
			if action == "replace-endpoint" && m.Context != 64000 {
				t.Fatal("old endpoint refreshed new endpoint")
			}
		})
	}
}

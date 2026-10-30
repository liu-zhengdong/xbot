package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/vault"
)

func TestImageGenProxyFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts []provider.ContentPart
		err   error
	}{
		{"no image", provider.TextContent("我画好了"), nil},
		{"failed save", []provider.ContentPart{{Type: provider.PartHosted, Text: "[厂商生图失败: 保存失败]", Hosted: &provider.HostedStep{Name: "image_generation", Status: "failed"}}}, nil},
		{"vendor denied", nil, errors.New("HTTP 403")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := generateImageTool{cap: imageGenCapability{proxy: providerFunc(func(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
				if !req.ImageGeneration || len(req.Tools) != 0 || req.HostedSearch {
					t.Fatalf("wrong image-only request: %+v", req)
				}
				return provider.Message{Content: tc.parts}, tc.err
			})}}
			out, err := tool.Run(context.Background(), json.RawMessage(`{"prompt":"猫"}`))
			if err == nil || strings.Contains(out, "已保存到工作区") {
				t.Fatalf("false success: %q %v", out, err)
			}
		})
	}
}

func TestImageGenResolution(t *testing.T) {
	for _, tc := range []struct {
		name, mode, binding, selected string
		enabled, login, hosted, proxy bool
	}{
		{"off", "auto", "chatgpt", "openai", false, true, false, false},
		{"hosted openai", "auto", "chatgpt", "openai", true, true, true, false},
		{"hosted xai", "auto", "xai", "xai", true, true, true, false},
		{"hosted before other selected account", "auto", "chatgpt", "xai", true, true, true, false},
		{"hosted needs no fallback login", "auto", "chatgpt", "xai", true, false, true, false},
		{"deepseek openai proxy", "auto", "deepseek", "openai", true, true, false, true},
		{"deepseek xai proxy", "auto", "deepseek", "xai", true, true, false, true},
		{"fallback not logged in", "auto", "deepseek", "xai", true, false, false, false},
		{"force proxy over hosted", "local", "chatgpt", "xai", true, true, false, true},
		{"force missing login", "local", "chatgpt", "xai", true, false, false, false},
		{"forced off", "local", "xai", "xai", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTools)
			for _, kind := range []string{"chatgpt", "xai"} {
				if err := config.Add(b.Root, config.Provider{Name: kind, OAuth: kind, Protocol: config.Responses, BaseURL: "http://127.0.0.1:1"}, config.ModelSpec{Name: "model", Context: 10000}); err != nil {
					t.Fatal(err)
				}
			}
			if err := config.Add(b.Root, config.Provider{Name: "deepseek", APIKeyEnv: "FAKE", Protocol: config.ChatCompletions, BaseURL: "http://127.0.0.1:1"}, config.ModelSpec{Name: "model", Context: 10000}); err != nil {
				t.Fatal(err)
			}
			if err := b.SetBinding(tc.binding, "model"); err != nil {
				t.Fatal(err)
			}
			v, err := config.OAuthVault(b.Root)
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"chatgpt", "xai"} {
				if tc.login && kind == config.ImageGenOAuth(tc.selected) || kind == tc.binding {
					if err := vault.SetOAuth(v, kind, vault.OAuthCredential{AccessToken: "fake"}, []string{"127.0.0.1"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := config.SetWebImageGen(b.Root, tc.enabled, tc.selected, tc.mode); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			cap := k.resolveImageGen(context.Background())
			k.turn = &turn{imageGen: &cap}
			if got := provider.HasImageGen(k.withImageGen(context.Background())); got != tc.hosted {
				t.Fatalf("hosted=%v want %v", got, tc.hosted)
			}
			e, ok := k.toolRegistry().find("generate_image")
			if ok != tc.proxy {
				t.Fatalf("proxy=%v want %v", ok, tc.proxy)
			}
			if ok && (e.group != "image_gen" || e.expose != exposeListed || review.ClassOf("generate_image") != review.Normal) {
				t.Fatalf("wrong registration: %+v", e)
			}
			if err := config.SetWebImageGen(b.Root, false, tc.selected, tc.mode); err != nil {
				t.Fatal(err)
			}
			if registered(k, "generate_image") != tc.proxy {
				t.Fatal("running turn lost snapshot")
			}
			k.turn = nil
			if registered(k, "generate_image") || provider.HasImageGen(k.withImageGen(context.Background())) {
				t.Fatal("disabled next turn retained capability")
			}
		})
	}
}

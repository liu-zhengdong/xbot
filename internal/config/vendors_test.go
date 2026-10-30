package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestVendorDefaultsAreValid(t *testing.T) {
	want := []string{"gemini", "vertex", "xai", "kimi", "kimi-code", "zhipu", "zai", "minimax", "stepfun", "mimo", "qianfan", "tencent", "opencode-go"}
	if len(Vendors) != len(want) {
		t.Fatalf("vendors: %d", len(Vendors))
	}
	seen := map[string]bool{}
	for i, v := range Vendors {
		if v.Brand != want[i] || seen[v.Brand] {
			t.Fatalf("order/dup at %d: %s", i, v.Brand)
		}
		seen[v.Brand] = true
		if v.Title == "" || len(v.Models) == 0 {
			t.Fatalf("%s: missing title or models", v.Brand)
		}
		if v.KeyLogin {
			continue
		}
		p := Provider{Name: v.Brand, Protocol: v.Protocol, BaseURL: v.BaseURL, APIKeyEnv: v.KeyEnv, Models: v.Models}
		if err := validateProvider(p, true); err != nil {
			t.Fatalf("%s: %v", v.Brand, err)
		}
		if !strings.HasPrefix(v.BaseURL, "https://") {
			t.Fatalf("%s: base_url %q", v.Brand, v.BaseURL)
		}
		if got := BrandForBase(v.BaseURL); got != v.Brand {
			t.Fatalf("%s: BrandForBase(%s)=%q", v.Brand, v.BaseURL, got)
		}
		for _, m := range v.Models {
			if err := validateModel(m); err != nil {
				t.Fatalf("%s: %v", v.Brand, err)
			}
		}
	}
	if !Vendors[1].KeyLogin || Vendors[1].BaseURL != "" {
		t.Fatal("vertex must not pretend to have a key route")
	}
}

func TestBrandForHostKeepsExistingFour(t *testing.T) {
	for host, want := range map[string]string{
		"api.deepseek.com": "deepseek", "api.openai.com": "openai", "api.anthropic.com": "anthropic", "openrouter.ai": "openrouter",
		"token-plan-sgp.xiaomimimo.com": "mimo", "API.X.AI.": "xai", "evil.example": "", "": "", "xiaomimimo.com.evil.example": "",
	} {
		if got := BrandForHost(host); got != want {
			t.Errorf("%q: %q, want %q", host, got, want)
		}
	}
}

func TestAddVendorIsIdempotentAndKeepsUserEntry(t *testing.T) {
	root := t.TempDir()
	p, err := AddVendor(root, "xai")
	if err != nil || p.Name != "xai" || p.APIKeyEnv != "XAI_API_KEY" || p.BaseURL != "https://api.x.ai/v1" || p.WireProtocol() != ChatCompletions {
		t.Fatalf("add: %+v %v", p, err)
	}
	if _, err := AddVendor(root, "xai"); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(root)
	if len(f.Providers) != 1 || len(f.Providers[0].Models) != 1 {
		t.Fatalf("not idempotent: %+v", f)
	}
	// A second default model lands in the same entry, and a user's own entry wins.
	if _, err := AddVendor(root, "tencent"); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(root)
	if tc := f.Providers[indexOf(f.Providers, "tencent")]; len(tc.Models) != 2 {
		t.Fatalf("models: %+v", tc.Models)
	}
	if err := Add(root, Provider{Name: "zai", Protocol: ChatCompletions, BaseURL: "https://proxy.example/v1", APIKeyEnv: "MY_ZAI_KEY"}, ModelSpec{Name: "mine", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err = AddVendor(root, "zai"); !errors.Is(err, ErrVendorNameTaken) {
		t.Fatalf("same-name entry on another endpoint accepted: %v", err)
	}
	f, _ = Load(root)
	if z := f.Providers[indexOf(f.Providers, "zai")]; z.BaseURL != "https://proxy.example/v1" || z.APIKeyEnv != "MY_ZAI_KEY" {
		t.Fatalf("overwrote user entry: %+v", z)
	}
	raw, _ := os.ReadFile(Path(root))
	if strings.Contains(string(raw), "sk-") {
		t.Fatal("secret-looking text in providers.json")
	}
}

func TestAddVendorRefusesVertexAndUnknown(t *testing.T) {
	root := t.TempDir()
	if _, err := AddVendor(root, "vertex"); err != ErrNoKeyRoute {
		t.Fatalf("vertex: %v", err)
	}
	if _, err := AddVendor(root, "nope"); err == nil {
		t.Fatal("unknown brand accepted")
	}
	if _, err := os.Stat(Path(root)); err == nil {
		t.Fatal("registry written for a refused vendor")
	}
}

func TestSyncVendorModelsRegistersServedAndDropsStaleSeeds(t *testing.T) {
	root := t.TempDir()
	if _, err := AddVendor(root, "xai"); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(root)
	seed := f.Providers[0].Models[0].Name
	if err := SyncVendorModels(root, "xai", []ModelInfo{{Model: "grok-5", Context: 1_048_576}, {Model: "grok-5-mini"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, window, err := Resolve(root, "xai", "grok-5", func(string) string { return "k-0123456789" }); err != nil || window != 1_048_576 {
		t.Fatalf("served model not bindable: %v", err)
	}
	if _, _, err := Resolve(root, "xai", seed, func(string) string { return "k-0123456789" }); err == nil {
		t.Fatalf("stale seed %q still bindable", seed)
	}
	// A bound seed stays; an empty catalog changes nothing.
	if err := Add(root, Provider{Name: "xai"}, ModelSpec{Name: "grok-old", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := SyncVendorModels(root, "xai", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := SyncVendorModels(root, "xai", []ModelInfo{{Model: "grok-5"}}, func(m string) bool { return m == "grok-old" }); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(root)
	names := map[string]bool{}
	for _, m := range f.Providers[0].Models {
		names[m.Name] = true
	}
	if !names["grok-old"] || !names["grok-5"] || !names["grok-5-mini"] {
		t.Fatalf("models: %v", names)
	}
}

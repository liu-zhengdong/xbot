package config

import (
	"os"
	"strings"
	"testing"
)

func TestSeesImagesByModel(t *testing.T) {
	yes := []string{"", "deepseek-flash", "group/auto-deepseek-v4-1-flash", "DeepSeek-Flash",
		"gpt-5.4", "gpt-5.5", "gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
		"gpt-6", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1", "gpt-6.1-sol",
		"claude-sonnet-4-5", "claude-opus-4-6"}
	no := []string{"deepseek-chat", "deepseek-v4-pro", "deepseek-reasoner", "gpt-4o", "mock",
		"gpt-5.60", "gpt-6.10-sol", "gpt-60-sol", "gpt-6.2-sol", "gpt-6-unknown", "other/gpt-6-sol"}
	for _, name := range yes {
		if !SeesImages(name, nil) {
			t.Errorf("%q should see images", name)
		}
		off := false
		if SeesImages(name, &off) {
			t.Errorf("%q ignored explicit vision off", name)
		}
	}
	for _, name := range no {
		if SeesImages(name, nil) {
			t.Errorf("%q should not see images", name)
		}
		on := true
		if !SeesImages(name, &on) {
			t.Errorf("%q ignored explicit vision on", name)
		}
	}
	on, off := true, false
	if !SeesImages("deepseek-v4-pro", &on) {
		t.Fatal("explicit vision did not win")
	}
	if SeesImages("deepseek-flash", &off) {
		t.Fatal("explicit vision off did not win")
	}
}

func TestVisionFlagSurvivesContextUpdate(t *testing.T) {
	root := t.TempDir()
	off := false
	p := Provider{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"}
	if err := Add(root, p, ModelSpec{Name: "deepseek-flash", Context: 1000, Vision: &off}); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-flash", Context: 2000}); err != nil {
		t.Fatal(err)
	}
	_, spec, err := Lookup(root, "deepseek", "deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	if spec.SeesImages() {
		t.Fatal("context update cleared vision off")
	}
	on := true
	if err := Add(root, Provider{Name: "deepseek"}, ModelSpec{Name: "deepseek-v4-pro", Context: 1000, Vision: &on}); err != nil {
		t.Fatal(err)
	}
	_, pro, err := Lookup(root, "deepseek", "deepseek-v4-pro")
	if err != nil {
		t.Fatal(err)
	}
	if !pro.SeesImages() {
		t.Fatal("explicit on was ignored")
	}
}

func TestLoadWebDefaultsAndRejectsRawKey(t *testing.T) {
	root := t.TempDir()
	f, err := LoadWeb(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Search.Backend != "" || f.Fetch.Backend != "" {
		t.Fatalf("default file %+v", f)
	}
	if err := SaveWeb(root, WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "not a name"}}); err == nil {
		t.Fatal("saved a bad env name")
	}
	if err := SaveWeb(root, WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_SEARCH_BACKEND", "ddg")
	got, err := LoadWeb(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Search.Backend != "ddg" || got.Search.APIKeyEnv != "SEARCH_KEY" {
		t.Fatalf("env override %+v", got.Search)
	}
	if _, err := SearchKey(WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}, func(string) string { return "" }); err == nil {
		t.Fatal("empty search key")
	}
	key, err := SearchKey(WebBackend{Backend: "deepseek"}, func(k string) string {
		if k == "DEEPSEEK_API_KEY" {
			return "from-provider"
		}
		return ""
	})
	if err != nil || key != "from-provider" {
		t.Fatalf("reuse provider key: %v %q", err, key)
	}
	if _, err = SearchKey(WebBackend{Backend: "deepseek"}, func(k string) string {
		if k == "XBOT_API_KEY" {
			return "from-xbot"
		}
		return ""
	}); err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("xbot key must not be reused: %v", err)
	}
	key, err = SearchKey(WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}, func(k string) string {
		if k == "SEARCH_KEY" {
			return "separate"
		}
		if k == "DEEPSEEK_API_KEY" {
			return "from-provider"
		}
		return ""
	})
	if err != nil || key != "separate" {
		t.Fatalf("separate key: %v %q", err, key)
	}
	if _, err := SearchKey(WebBackend{Backend: "deepseek"}, func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatal(err)
	}
}

func TestSearchKeyEnvIsHeldLikeProviderKeys(t *testing.T) {
	root := t.TempDir()
	if err := SaveWeb(root, WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY"}}); err != nil {
		t.Fatal(err)
	}
	names := KeyEnvNames(root)
	if !strings.Contains(strings.Join(names, " "), "SEARCH_KEY") {
		t.Fatalf("key env %v", names)
	}
	t.Setenv("SEARCH_KEY", "sekret-search")
	t.Cleanup(ForgetKeys)
	HoldKeys([]string{"SEARCH_KEY"})
	if os.Getenv("SEARCH_KEY") != "" {
		t.Fatal("search key still visible to os.Getenv")
	}
	if Getenv("SEARCH_KEY") != "sekret-search" {
		t.Fatalf("memory %q", Getenv("SEARCH_KEY"))
	}
	plain := t.TempDir()
	if err := SaveWeb(plain, WebFile{Search: WebBackend{Backend: "deepseek"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(KeyEnvNames(plain), " "), "DEEPSEEK_API_KEY") {
		t.Fatalf("default search key missing: %v", KeyEnvNames(plain))
	}
	if got := RedactHeld("leak sekret-search"); strings.Contains(got, "sekret-search") || !strings.Contains(got, "[secret]") {
		t.Fatalf("redact %q", got)
	}
}

func TestLegacyTrustedEgressIsIgnoredAndDropped(t *testing.T) {
	root := t.TempDir()
	// An older web.json still loads; the value no longer matters.
	if err := os.WriteFile(WebPath(root), []byte(`{"trusted_egress_cidrs":["198.18.0.0/15","127.0.0.0/8"],"fetch":{"backend":"http"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadWeb(root)
	if err != nil || len(f.TrustedEgressCIDRs) != 0 || f.Fetch.Backend != "http" {
		t.Fatalf("%v %+v", err, f)
	}
	if err := SaveWeb(root, f); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(WebPath(root))
	if err != nil || strings.Contains(string(raw), "trusted_egress") {
		t.Fatalf("legacy field survived a save: %v %s", err, raw)
	}
}

func TestSearchBaseLocksProviderKeyToOfficialHost(t *testing.T) {
	if err := AllowSearchBase("", WebBackend{Backend: "deepseek", BaseURL: "http://api.deepseek.com"}); err == nil {
		t.Fatal("http was accepted")
	}
	if err := AllowSearchBase("", WebBackend{Backend: "deepseek", BaseURL: "https://evil.example/v1"}); err == nil || !strings.Contains(err.Error(), "api.deepseek.com") {
		t.Fatal(err)
	}
	if err := AllowSearchBase("", WebBackend{Backend: "deepseek", BaseURL: "https://api.deepseek.com"}); err != nil {
		t.Fatal(err)
	}
	if err := AllowSearchBase("", WebBackend{Backend: "deepseek"}); err != nil {
		t.Fatal(err)
	}
	searchRoot := t.TempDir()
	t.Setenv("SEARCH_KEY", "sekret-search-value")
	t.Cleanup(ForgetKeys)
	HoldKeys([]string{"SEARCH_KEY"})
	if err := AllowSearchBase(searchRoot, WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY", BaseURL: "https://search.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := AllowSearchBase(searchRoot, WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_KEY", BaseURL: "http://search.example"}); err == nil {
		t.Fatal("http search key was accepted")
	}
	root := t.TempDir()
	if err := Add(root, Provider{Name: "other", BaseURL: "https://example.com/v1", APIKeyEnv: "OTHER_KEY"}, ModelSpec{Name: "m", Context: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := AllowSearchBase(root, WebBackend{Backend: "deepseek", APIKeyEnv: "OTHER_KEY", BaseURL: "https://evil.example"}); err == nil {
		t.Fatal("provider key was sent to another host")
	}
}

func TestSearchBaseFailsClosedWhenRegistryUnreadable(t *testing.T) {
	t.Cleanup(ForgetKeys)
	root := t.TempDir()
	t.Setenv("MAGPIE_API_KEY", "magpie-key-value")
	HoldKeys([]string{"MAGPIE_API_KEY"})
	if err := AllowSearchBase(root, WebBackend{Backend: "deepseek", APIKeyEnv: "MAGPIE_API_KEY", BaseURL: "https://search.example"}); err != nil {
		t.Fatal(err)
	}
	bad := t.TempDir()
	if err := os.WriteFile(Path(bad), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	err := AllowSearchBase(bad, WebBackend{Backend: "deepseek", APIKeyEnv: "SEARCH_ONLY", BaseURL: "https://evil.example"})
	if err == nil || !strings.Contains(err.Error(), "api.deepseek.com") {
		t.Fatal(err)
	}
}

func TestRedactHeldCacheFollowsHoldAndForget(t *testing.T) {
	t.Setenv("XBOT_CACHE_KEY", "cache-key-0123456789")
	t.Cleanup(ForgetKeys)
	HoldKeys([]string{"XBOT_CACHE_KEY"})
	if strings.Contains(RedactHeld("cache-key-0123456789"), "cache-key") {
		t.Fatal("first redact missed")
	}
	if strings.Contains(RedactHeld("cache-key-0123456789"), "cache-key") {
		t.Fatal("cached redact missed")
	}
	ForgetKeys()
	if got := RedactHeld("cache-key-0123456789"); !strings.Contains(got, "cache-key") {
		t.Fatalf("forget left a cached value: %q", got)
	}
}

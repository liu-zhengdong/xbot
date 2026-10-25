package config

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"xbot/internal/fsutil"
)

func TestWebSearchPreferencesAndCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_SEARCH_BACKEND", "deepseek")
	t.Setenv("XBOT_SEARCH_BASE_URL", "https://search.example")
	t.Setenv("DEEPSEEK_API_KEY", "fake-legacy-secret")
	defer ForgetKeys()
	f, err := LoadWeb(root)
	if err != nil || !f.SearchOn() || !f.HostedSearchOn() || f.SearchProviderID() != "deepseek" {
		t.Fatalf("legacy: %+v %v", f, err)
	}
	f.Fetch.Backend = "http"
	f.TrustedEgressCIDRs = []string{"198.18.0.0/15"}
	if err := SaveWeb(root, f); err != nil {
		t.Fatal(err)
	}
	if err := SetWebSearch(root, false, "local", "ddg"); err != nil {
		t.Fatal(err)
	}
	f, err = LoadWeb(root)
	if err != nil || f.SearchOn() || f.HostedSearchOn() || f.SearchProviderID() != "ddg" || f.Search.BaseURL != "https://search.example" || f.Fetch.Backend != "http" || len(f.TrustedEgressCIDRs) != 1 {
		t.Fatalf("saved: %+v %v", f, err)
	}
	if err := SetWebSearch(root, true, "auto", "deepseek"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSearchCredential(root, "deepseek", "fake-tested-search-secret"); err != nil {
		t.Fatal(err)
	}
	ForgetKeys() // simulate a fresh serve without loading the desktop env file
	f, _ = LoadWeb(root)
	if got, err := SearchCredential(root, f, "deepseek"); err != nil || got != "fake-tested-search-secret" {
		t.Fatal("credential not retained")
	}
	b, _, err := SearchConnection(root, f)
	if err != nil {
		t.Fatal(err)
	}
	if b.BaseURL != "" || b.APIKeyEnv != SearchSecretName("deepseek") {
		t.Fatalf("tested credential changed destination: %+v", b)
	}
	raw, _ := os.ReadFile(WebPath(root))
	if strings.Contains(string(raw), "fake-tested") {
		t.Fatal("key leaked into web config")
	}
	info, _ := os.Stat(EnvFile(root))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if len(SearchProviders()) != 9 {
		t.Fatal("catalog")
	}
	for _, p := range SearchProviders() {
		if err := SetWebSearch(root, true, "auto", p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if SetWebSearch(root, true, "bad", "ddg") == nil || SetWebSearch(root, true, "auto", "bocha") == nil {
		t.Fatal("invalid choice accepted")
	}
}

func TestConcurrentSearchKeysPreserveOtherCredentials(t *testing.T) {
	root := t.TempDir()
	defer ForgetKeys()
	var wg sync.WaitGroup
	for _, p := range SearchProviders() {
		if !p.NeedsKey {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := SaveSearchCredential(root, id, "fake-test-"+id); err != nil {
				t.Error(err)
			}
		}(p.ID)
	}
	wg.Wait()
	raw, err := os.ReadFile(EnvFile(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range SearchProviders() {
		if p.NeedsKey && !strings.Contains(string(raw), SearchSecretName(p.ID)+"=") {
			t.Errorf("lost %s", p.ID)
		}
	}
}

func TestSearchSwitchPreservesLegacyCredentialBinding(t *testing.T) {
	root := t.TempDir()
	defer ForgetKeys()
	t.Setenv("LEGACY_SEARCH_KEY", "fake-legacy-bound-search-key")
	f := WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "LEGACY_SEARCH_KEY", BaseURL: "https://legacy.example/v1"}}
	if err := SaveWeb(root, f); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ddg", "brave", "deepseek"} {
		if err := SetWebSearch(root, true, "auto", id); err != nil {
			t.Fatal(err)
		}
		current, err := LoadWeb(root)
		if err != nil {
			t.Fatal(err)
		}
		if current.Search.APIKeyEnv != f.Search.APIKeyEnv || current.Search.BaseURL != f.Search.BaseURL {
			t.Fatal("selection deleted endpoint or credential binding")
		}
		key, err := SearchCredential(root, current, "deepseek")
		if err != nil || key != "fake-legacy-bound-search-key" {
			t.Fatal("selection lost usable legacy key")
		}
	}
}

func TestSearchConnectionCredentialAndDestinationStayTogether(t *testing.T) {
	root := t.TempDir()
	defer ForgetKeys()
	const oldKey = "fake-legacy-custom-secret"
	const newKey = "fake-tested-official-secret"
	t.Setenv("LEGACY_CONNECTION_KEY", oldKey)
	f := WebFile{Search: WebBackend{Backend: "deepseek", APIKeyEnv: "LEGACY_CONNECTION_KEY", BaseURL: "https://legacy.example"}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			if err := SaveSearchCredential(root, "deepseek", newKey); err != nil {
				t.Error(err)
				return
			}
			if err := os.Remove(EnvFile(root)); err != nil {
				t.Error(err)
				return
			}
			SetHeld(SearchSecretName("deepseek"), "")
		}
	}()
	defer func() { <-done }()
	for i := 0; i < 3000; i++ {
		b, key, err := SearchConnection(root, f)
		if err != nil {
			t.Fatal(err)
		}
		if key == newKey && (b.BaseURL != "" || b.APIKeyEnv != SearchSecretName("deepseek")) {
			t.Fatal("tested key paired with legacy destination")
		}
		if key == oldKey && b.BaseURL != f.Search.BaseURL {
			t.Fatal("legacy key destination changed")
		}
		if key != newKey && key != oldKey {
			t.Fatal("unexpected credential snapshot")
		}
	}
}

func TestSearchCredentialReadWaitsForCredentialCommit(t *testing.T) {
	root := t.TempDir()
	defer ForgetKeys()
	if err := SaveSearchCredential(root, "deepseek", "fake-before-rotation"); err != nil {
		t.Fatal(err)
	}
	lock, err := fsutil.Lock(EnvFile(root) + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_ = lock.Release()
		}
	}()
	ready := make(chan struct{})
	result := make(chan string, 1)
	go func() {
		close(ready)
		key, err := SearchCredential(root, WebFile{}, "deepseek")
		if err != nil {
			t.Error(err)
		}
		result <- key
	}()
	<-ready
	select {
	case <-result:
		t.Fatal("credential reader bypassed an in-progress credential commit")
	case <-time.After(30 * time.Millisecond):
	}
	// Emulate the writer's atomic file+redaction commit while owning its lock.
	const updated = "fake-after-rotation"
	if err := saveEnvKey(root, SearchSecretName("deepseek"), updated); err != nil {
		t.Fatal(err)
	}
	SetHeld(SearchSecretName("deepseek"), updated)
	_ = lock.Release()
	released = true
	select {
	case key := <-result:
		if key != updated {
			t.Fatal("reader returned the superseded credential")
		}
	case <-time.After(time.Second):
		t.Fatal("credential reader did not resume")
	}
	if RedactHeld(updated) != "[secret]" {
		t.Fatal("latest credential was removed from redaction")
	}
}

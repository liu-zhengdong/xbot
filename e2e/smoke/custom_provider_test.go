//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The real serve binary, shipped page and real browser exercise the entire
// settings → registry → live catalog → Bot binding journey. Only /models is
// scripted; the browser never replaces fetch or fabricates a provider list.
func TestCustomProviderBrowserBinary(t *testing.T) {
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("Chrome unavailable")
	}
	if err := exec.Command("node", "-e", `require('playwright')`).Run(); err != nil {
		t.Skip("Playwright unavailable")
	}
	const fakeKey = "scripted-browser-custom-0123456789"
	var catalogs atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" && r.URL.Path != "/edited/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fakeKey {
			t.Error("custom catalog did not use the saved key")
			http.Error(w, "unauthorized", 401)
			return
		}
		catalogs.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"runtime-custom-browser","context_length":320000}]}`)
	}))
	defer up.Close()
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("custom-browser", "Only used for custom provider binding; do not call a model.")
	u, _ := url.Parse(srv.URL)
	shots := os.Getenv("XBOT_CUSTOM_EVIDENCE")
	if shots == "" {
		shots = filepath.Join(t.TempDir(), "screenshots")
	}
	cfg := map[string]any{"chrome": chrome, "url": srv.URL, "base": up.URL + "/v1", "key": fakeKey, "shots": shots, "cookies": srv.client.Jar.Cookies(u)}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "browser.json")
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", filepath.Join("..", "..", "internal", "web", "testdata", "custom-provider.cjs"), cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("custom provider browser: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "passed") || catalogs.Load() == 0 {
		t.Fatalf("custom provider browser produced no live catalog evidence: %s", out)
	}
	t.Log(strings.TrimSpace(string(out)))
	t.Logf("scripted custom catalog HTTP calls=%d; screenshots=%s", catalogs.Load(), shots)
}

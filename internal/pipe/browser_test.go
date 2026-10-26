package pipe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/kernel"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

type browserScripted struct{}

func (browserScripted) Chat(_ context.Context, _ provider.Request, delta func(string)) (provider.Message, error) {
	if delta != nil {
		delta("管道测试回复")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("管道测试回复")}, nil
}

// Optional cross-repo acceptance: real Chromium + synced UI + real Go crypto,
// account API and owner handlers, with a scripted provider (never a real model).
func TestBrowserRemoteUI(t *testing.T) {
	assets := os.Getenv("XBOT_PIPE_BROWSER_APP")
	if assets == "" {
		t.Skip("set XBOT_PIPE_BROWSER_APP to xbot-account/public")
	}
	w := newWorld(t)
	t.Setenv("XBOT_PIPE_BROWSER_FAKE_KEY", "scripted-only")
	if err := config.Add(w.root, config.Provider{Name: "fake", Protocol: config.ChatCompletions, BaseURL: "http://127.0.0.1:1", APIKeyEnv: "XBOT_PIPE_BROWSER_FAKE_KEY"}, config.ModelSpec{Name: "fake", Context: 32768}); err != nil {
		t.Fatal(err)
	}
	w.ui.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: browserScripted{}, Memory: memory.NopStore{}, MaxSteps: 4}, nil
	}
	w.startDevice(t)
	accountURL, _ := url.Parse(w.hub.Accounts.(account.Remote).Base)
	proxy := httputil.NewSingleHostReverseProxy(accountURL)
	site := http.NewServeMux()
	site.Handle("/account/v1/", proxy)
	site.HandleFunc("GET /test-session", func(out http.ResponseWriter, _ *http.Request) {
		writeJSON(out, 200, map[string]string{"session": w.entrySes})
	})
	site.HandleFunc("GET /app/pipe-bridge.js", func(out http.ResponseWriter, _ *http.Request) {
		raw, err := os.ReadFile(filepath.Join(assets, "app/pipe-bridge.js"))
		if err != nil {
			t.Error(err)
			out.WriteHeader(500)
			return
		}
		out.Header().Set("Content-Type", "text/javascript")
		_, _ = out.Write([]byte(strings.ReplaceAll(string(raw), "https://pipe.babelgo.cn:8443", w.hubURL)))
	})
	site.Handle("/", http.FileServer(http.Dir(assets)))
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/app/") {
			policy := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' " + w.hubURL + " " + strings.Replace(w.hubURL, "http:", "ws:", 1) + "; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
			out.Header().Set("Content-Security-Policy", policy)
		}
		site.ServeHTTP(out, r)
	}))
	defer srv.Close()
	w.hub.Origins = []string{srv.URL}
	cmd := exec.Command("node", "testdata/browser-remote-ui.cjs", srv.URL)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

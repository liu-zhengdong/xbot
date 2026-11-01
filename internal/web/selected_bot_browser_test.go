package web

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/browsertest"
)

func TestSelectedBotSurvivesClosedPageBrowser(t *testing.T) {
	runSelectedBotBrowser(t, "closed-page")
}

func TestSelectedBotSurvivesBrowserRestartBrowser(t *testing.T) {
	runSelectedBotBrowser(t, "browser-restart")
}

func TestSelectedBotWindowPreferenceBrowser(t *testing.T) {
	runSelectedBotBrowser(t, "window-preference")
}

func TestSelectedBotMissingRememberedFallbackBrowser(t *testing.T) {
	for _, fallback := range []string{"primary", "first"} {
		t.Run(fallback, func(t *testing.T) {
			runSelectedBotBrowser(t, "missing-"+fallback)
		})
	}
}

func runSelectedBotBrowser(t *testing.T, scenario string) {
	t.Helper()
	chrome := browsertest.Chrome(t)
	if err := exec.Command("node", "-e", `require('playwright')`).Run(); err != nil {
		t.Skip("Playwright unavailable")
	}
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	// Serve the shipped HTML, scripts and styles without injecting an alternate
	// app. Playwright scripts only the HTTP data and operates real page lifetimes.
	server := httptest.NewServer(http.FileServer(http.FS(assets)))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", filepath.Join("testdata", "selected-bot.cjs"), chrome, server.URL, filepath.Join(t.TempDir(), "chrome"), scenario).CombinedOutput()
	if err != nil {
		t.Fatalf("selected Bot browser (%s): %v\n%s", scenario, err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

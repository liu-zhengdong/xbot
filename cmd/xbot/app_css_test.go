package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/web"
)

func TestDesktopTitleBarCSS(t *testing.T) {
	mux := http.NewServeMux()
	(&web.Server{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/app.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	css := rec.Body.String()
	// The desktop window: a drag strip as tall as MyGo's title bar, the native
	// window buttons' inset kept empty, and the card starting below the strip.
	for _, rule := range []string{
		"--strip-h: var(--mygo-titlebar-height, 40px);",
		"--strip-reserve: var(--mygo-titlebar-inset-right, 0px);",
		"background: var(--base); --app-region: drag;",
		".in-app .shell { padding: var(--strip-h) 8px 8px 0; }",
		"--app-region: no-drag",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("missing desktop CSS: %s", rule)
		}
	}
}

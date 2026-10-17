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
	for _, rule := range []string{
		"@supports (padding-left: max(0px, 1px))",
		".in-app .sidebar .pin-row {",
		"padding-top: calc(16px + var(--mygo-titlebar-height, 0px))",
		"padding-left: max(var(--mygo-titlebar-inset-left, 0px), 0px)",
		".in-app .top {\n    --app-region: drag;",
		"padding-right: calc(16px + var(--mygo-titlebar-inset-right, 0px))",
		".in-app .panel-bar {\n    --app-region: drag;",
		"right: calc(8px + var(--mygo-titlebar-inset-right, 0px))",
		".in-app :is(.pin-row, .top, .panel-bar) :is(button, a, input, select, textarea, [role=\"button\"])",
		"--app-region: no-drag",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("missing desktop CSS: %s", rule)
		}
	}
}

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownPage(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	for _, tc := range []struct{ path, accept, content string }{
		{"/settings", "text/html", "text/html"},
		{"/settings", "application/json", "text/plain"},
		{"/api/missing", "text/html", "application/json"},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Accept", tc.accept)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), tc.content) {
			t.Fatalf("%s: status %d, content type %q", tc.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if tc.content == "text/html" {
			for _, want := range []string{"没有这个页面", "回到对话", `href="/"`, `href="/app.css"`} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("404 page missing %q", want)
				}
			}
		}
	}
	for _, path := range []string{"/", "/app.js", "/app.css", "/fonts/MonaSansVF.woff2", "/fonts/NotoSansSC-subset.woff2", "/fonts/NotoSansSC-full.woff2"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("known route %s: status %d", path, rec.Code)
		}
	}
}

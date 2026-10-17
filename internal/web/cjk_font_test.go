package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCJKFontBundled(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	get := func(path, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	font := get("/fonts/NotoSansSC-subset.woff2", "")
	if font.Code != http.StatusOK || font.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(font.Body.String(), "wOF2") {
		t.Fatalf("font not served: %d %q", font.Code, font.Header().Get("Content-Type"))
	}
	etag := font.Header().Get("ETag")
	if etag == "" {
		t.Fatal("static files need an ETag so the 2 MB font is not re-sent on every load")
	}
	if again := get("/fonts/NotoSansSC-subset.woff2", etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("matching If-None-Match = %d with %d bytes, want empty 304", again.Code, again.Body.Len())
	}
	css := get("/app.css", "").Body.String()
	for _, want := range []string{
		`font-family: "XBot SC"`,
		`url("/fonts/NotoSansSC-subset.woff2")`,
		"unicode-range: U+3000-303F",
		`--font: "Mona Sans VF", "XBot SC",`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q", want)
		}
	}
}

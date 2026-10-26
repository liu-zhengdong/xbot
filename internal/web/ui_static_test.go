package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/version"
)

func TestUIStatic(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	call := func(name, etag string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/ui/static/"+name, nil)
		r.Header.Set("If-None-Match", etag)
		if auth {
			if err := s.AuthorizeOwner(r); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call("app.js", "", false); w.Code != 401 {
		t.Errorf("unauth: %d", w.Code)
	}
	for _, name := range []string{"index.html", "app.js", "app.css", "provider-icons/claude.svg", "vendor/katex/katex.min.js"} {
		w := call(name, "", true)
		body, _ := staticFS.ReadFile("static/" + name)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) || w.Header().Get("Content-Type") == "" {
			t.Fatalf("%s: %d", name, w.Code)
		}
		tip := w.Header().Get("X-Xbot-Tip")
		if tip == "" || tip == "dev" || (version.Version != "dev" && tip != version.Version) {
			t.Fatalf("tip %q", tip)
		}
		etag := w.Header().Get("ETag")
		if etag == "" || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("missing cache metadata")
		}
		cached := call(name, etag, true)
		if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("X-Xbot-Tip") != tip {
			t.Fatalf("conditional: %d", cached.Code)
		}
	}
	for _, name := range []string{"not-found.js", "go.mod", "fonts", "%2e%2e/server.go", "../server.go", "%2fetc/passwd", "foo%5cbar.js", "%252e%252e/app.js"} {
		w := call(name, "", true)
		// ServeMux can reject/canonicalize before the handler; never serve a file.
		if w.Code == 200 || strings.Contains(w.Body.String(), "package web") {
			t.Fatalf("unsafe %s: %d", name, w.Code)
		}
	}
	w := call("fonts/NotoSansSC-full.woff2", "", true)
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.Len() > 1024 {
		t.Fatalf("oversize: %d len=%d", w.Code, w.Body.Len())
	}
	// The pipe endpoint must never read an XBOT_DEV_STATIC checkout.
	t.Setenv(devStaticEnv, t.TempDir())
	if w := call("app.js", "", true); w.Code != 200 {
		t.Fatalf("embedded source: %d", w.Code)
	}
}

package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/pipe/limits"
	"xbot/internal/version"
)

func TestUIStatic(t *testing.T) {
	t.Setenv(devStaticEnv, "")
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
}

func staticRequest(t *testing.T, s *Server, mux http.Handler, url, etag string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, url, nil)
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

func TestUIStaticSameSource(t *testing.T) {
	for _, disk := range []bool{false, true} {
		mode := "embed"
		if disk {
			mode = "disk"
		}
		t.Run(mode, func(t *testing.T) {
			dir := ""
			if disk {
				dir = t.TempDir()
			}
			t.Setenv(devStaticEnv, dir)
			s := &Server{Root: t.TempDir(), Owner: "u1"}
			if _, _, err := s.EnsureToken(); err != nil {
				t.Fatal(err)
			}
			mux := muxOf(s)
			for _, name := range []string{"index.html", "app.js", "app.css", "provider-icons/claude.svg", "vendor/katex/katex.min.js"} {
				localPath := "/" + name
				if name == "index.html" {
					localPath = "/"
				}
				oldETag := ""
				for _, revision := range []string{"first", "edited without rebuild"} {
					want, err := staticFS.ReadFile("static/" + name)
					if err != nil {
						t.Fatal(err)
					}
					if disk {
						want = []byte(revision + ": " + name)
						file := filepath.Join(dir, filepath.FromSlash(name))
						if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(file, want, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					local := staticRequest(t, s, mux, localPath, "", false)
					remote := staticRequest(t, s, mux, "/api/ui/static/"+name, "", true)
					if local.Code != 200 || remote.Code != 200 || !bytes.Equal(local.Body.Bytes(), want) || !bytes.Equal(remote.Body.Bytes(), want) {
						t.Fatalf("%s %s: local=%d pipe=%d, bodies match expected=%v/%v", name, revision, local.Code, remote.Code, bytes.Equal(local.Body.Bytes(), want), bytes.Equal(remote.Body.Bytes(), want))
					}
					for _, key := range []string{"Content-Type", "X-Xbot-Tip", "ETag", "Cache-Control"} {
						if local.Header().Get(key) == "" || local.Header().Get(key) != remote.Header().Get(key) {
							t.Fatalf("%s %s: local=%q pipe=%q", name, key, local.Header().Get(key), remote.Header().Get(key))
						}
					}
					wantTip, wantCache := embeddedUITip(), "no-cache"
					if disk {
						wantTip, wantCache = "dev-static", "no-store"
					}
					if remote.Header().Get("X-Xbot-Tip") != wantTip || remote.Header().Get("Cache-Control") != wantCache {
						t.Fatalf("source metadata: %v", remote.Header())
					}
					etag := remote.Header().Get("ETag")
					for _, url := range []string{localPath, "/api/ui/static/" + name} {
						cached := staticRequest(t, s, mux, url, etag, true)
						if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("X-Xbot-Tip") != wantTip {
							t.Fatalf("conditional %s: %d", url, cached.Code)
						}
						if disk && oldETag != "" {
							fresh := staticRequest(t, s, mux, url, oldETag, true)
							if fresh.Code != 200 || !bytes.Equal(fresh.Body.Bytes(), want) || etag == oldETag {
								t.Fatalf("stale ETag after disk edit: %s", url)
							}
						}
					}
					oldETag = etag
				}
			}
		})
	}
}

func TestUIStaticDiskBoundaries(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	t.Setenv(devStaticEnv, dir)
	for name, body := range map[string][]byte{
		"app.js": []byte("disk"), "private.go": []byte("must not serve"),
		"exact.js": bytes.Repeat([]byte("x"), limits.MaxBody),
		"large.js": bytes.Repeat([]byte("x"), limits.MaxBody+1),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.js"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.js"), []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape.js": filepath.Join(outside, "secret.js"), "escape-dir": outside, "inside.js": "app.js"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	if w := staticRequest(t, s, mux, "/api/ui/static/app.js", "", false); w.Code != 401 {
		t.Fatalf("unauth: %d", w.Code)
	}
	for _, name := range []string{"private.go", "directory.js", "escape.js", "escape-dir/secret.js", "not-found.js", "app.css", "../secret.js", "/secret.js", "foo//app.js", "./app.js", "foo\\app.js", "%2e%2e/secret.js", "bad\x00.js"} {
		// Exercise validation directly, without ServeMux's redirects masking it.
		r := httptest.NewRequest(http.MethodGet, "/api/ui/static/test", nil)
		r.SetPathValue("path", name)
		if err := s.AuthorizeOwner(r); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.uiStatic(w, r)
		if w.Code != 404 || strings.Contains(w.Body.String(), "outside-secret") {
			t.Fatalf("unsafe or missing %q: %d", name, w.Code)
		}
	}
	for _, name := range []string{"escape.js", "escape-dir/secret.js", "app.css"} {
		w := httptest.NewRecorder()
		s.static("static/"+name, "text/javascript")(w, httptest.NewRequest("GET", "/test", nil))
		if w.Code != 404 {
			t.Fatalf("local escape/missing %s: %d", name, w.Code)
		}
	}
	for name, want := range map[string]int{"inside.js": 200, "exact.js": 200, "large.js": 413} {
		w := staticRequest(t, s, mux, "/api/ui/static/"+name, "", true)
		if w.Code != want || w.Body.Len() > limits.MaxBody || (want == 413 && w.Body.Len() > 1024) {
			t.Fatalf("size/link %s: status=%d len=%d", name, w.Code, w.Body.Len())
		}
	}
}

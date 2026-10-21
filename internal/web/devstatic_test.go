package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDevStaticReadsDiskEachRequest(t *testing.T) {
	dir := t.TempDir()
	css := filepath.Join(dir, "app.css")
	if err := os.WriteFile(css, []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(devStaticEnv, dir)
	h := (&Server{}).static("static/app.css", "text/css; charset=utf-8")
	get := func() string {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control %q", got)
		}
		return rec.Body.String()
	}
	if got := get(); got != "a{}" {
		t.Fatalf("first body %q", got)
	}
	if err := os.WriteFile(css, []byte("b{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := get(); got != "b{}" {
		t.Fatalf("after edit %q", got)
	}
}

func TestStaticDefaultsToEmbedded(t *testing.T) {
	t.Setenv(devStaticEnv, "")
	want, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	(&Server{}).static("static/app.css", "text/css; charset=utf-8")(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if rec.Body.String() != string(want) {
		t.Fatal("default static did not serve the embedded file")
	}
}

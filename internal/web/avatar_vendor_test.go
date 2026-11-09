package web

import (
	"bytes"
	"image/png"
	"io/fs"
	"net/http"
	"testing"

	"xbot/internal/pipe/limits"
)

func TestAvatarBundleIsServedWithLicenses(t *testing.T) {
	t.Setenv(devStaticEnv, "")
	s := &Server{Root: t.TempDir(), Owner: "avatar"}
	mux := muxOf(s)
	for _, path := range []string{"/vendor/xbot-avatar.min.js", "/vendor/LICENSES.txt"} {
		w := staticRequest(t, s, mux, path, "", false)
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
	}
}

func TestAvatarLayersAreServed(t *testing.T) {
	t.Setenv(devStaticEnv, "")
	s := &Server{Root: t.TempDir(), Owner: "avatar"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	const base = "/api/ui/static/"
	if w := staticRequest(t, s, mux, base+"avatar-parts/base/bald.png", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated image: %d", w.Code)
	}
	count := 0
	err := fs.WalkDir(staticFS, "static/avatar-parts", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == "SOURCE.txt" {
			return err
		}
		count++
		w := staticRequest(t, s, mux, base+path[len("static/"):], "", true)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %d %q", path, w.Code, w.Header().Get("Content-Type"))
		}
		if w.Body.Len() > limits.MaxBody {
			t.Fatalf("%s exceeds the pipe body budget", path)
		}
		image, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if image.Bounds().Dx() != 512 || image.Bounds().Dy() != 512 {
			t.Fatalf("%s: %v", path, image.Bounds())
		}
		cached := staticRequest(t, s, mux, base+path[len("static/"):], w.Header().Get("ETag"), true)
		if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
			t.Fatalf("%s cache: %d", path, cached.Code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no avatar layers served")
	}
}

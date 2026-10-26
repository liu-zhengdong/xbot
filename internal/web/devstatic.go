package web

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// devStaticEnv selects the serving process's UI for both local and pipe routes.
// Disk files are read on every request; release builds leave it unset.
const devStaticEnv = "XBOT_DEV_STATIC"

func staticContentType(name string) string {
	return map[string]string{
		".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png",
		".woff2": "font/woff2", ".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8",
	}[path.Ext(name)]
}

// staticAsset is the shared source, validation and response path. maxBody == 0
// keeps local large fonts available; the pipe always supplies its body budget.
func (s *Server) staticAsset(name, contentType string, maxBody int64) http.HandlerFunc {
	s.uiSourceOnce.Do(func() { s.uiStaticDir = os.Getenv(devStaticEnv) })
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\%\x00") || staticContentType(name) == "" {
		return http.NotFound
	}
	dir := s.uiStaticDir
	if dir == "" {
		body, status := readStaticAsset("", name, maxBody)
		return staticResponse(body, status, contentType, embeddedUITip(), "no-cache")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		body, status := readStaticAsset(dir, name, maxBody)
		staticResponse(body, status, contentType, "dev-static", "no-store")(w, r)
	}
}

func readStaticAsset(dir, name string, maxBody int64) ([]byte, int) {
	var f fs.File
	var err error
	if dir == "" {
		f, err = staticFS.Open("static/" + name)
	} else {
		// Root.Open rejects escapes through symlinks as well as lexical paths.
		root, openErr := os.OpenRoot(dir)
		if openErr != nil {
			return nil, http.StatusNotFound
		}
		defer root.Close()
		f, err = root.Open(name)
	}
	if err != nil {
		return nil, http.StatusNotFound
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, http.StatusNotFound
	}
	if maxBody > 0 && info.Size() > maxBody {
		return nil, http.StatusRequestEntityTooLarge
	}
	var reader io.Reader = f
	if maxBody > 0 {
		// A disk file can grow after Stat. Never allocate or return an unbounded body.
		reader = io.LimitReader(f, maxBody+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, http.StatusNotFound
	}
	if maxBody > 0 && int64(len(body)) > maxBody {
		return nil, http.StatusRequestEntityTooLarge
	}
	return body, http.StatusOK
}

func staticResponse(body []byte, status int, contentType, tip, cache string) http.HandlerFunc {
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, sum[:8])
	return func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Xbot-Tip", tip)
		if status == http.StatusRequestEntityTooLarge {
			writeErr(w, status, "static asset exceeds pipe body limit")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}
}

package web

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"xbot/internal/pipe/limits"
	"xbot/internal/version"
)

// A dev binary without linker metadata still identifies its embedded UI.
var embeddedUITip = sync.OnceValue(func() string {
	if version.Version != "dev" && version.Version != "" {
		return version.Version
	}
	h := sha256.New()
	_ = fs.WalkDir(staticFS, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			body, err := staticFS.ReadFile(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00%d\x00", name, len(body))
			h.Write(body)
		}
		return nil
	})
	return fmt.Sprintf("dev-%x", h.Sum(nil)[:6])
})

func (s *Server) uiStatic(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	name := r.PathValue("path")
	// ValidPath rejects absolute paths, empty segments and . / .. components.
	// The embedded tree plus these asset types are the entire allowlist.
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\%\x00") || name == "." {
		http.NotFound(w, r)
		return
	}
	ct := map[string]string{
		".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png",
		".woff2": "font/woff2", ".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8",
	}[path.Ext(name)]
	if ct == "" {
		http.NotFound(w, r)
		return
	}
	f, err := staticFS.Open("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Xbot-Tip", embeddedUITip())
	if info.Size() > limits.MaxBody {
		writeErr(w, http.StatusRequestEntityTooLarge, "static asset exceeds pipe body limit")
		return
	}
	body, err := staticFS.ReadFile("static/" + name)
	embeddedStatic(body, err, ct)(w, r)
}

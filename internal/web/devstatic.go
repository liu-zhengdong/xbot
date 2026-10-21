package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// devStaticEnv names a source checkout's static directory. When set, the page
// files are read from disk on every request so a refresh shows the edit. It is
// for local development (scripts/xbot-dev); release builds leave it unset and
// keep serving the embedded copy.
const devStaticEnv = "XBOT_DEV_STATIC"

// devStatic serves name ("static/...") from dir instead of the embedded FS.
func devStatic(dir, name, contentType string) http.HandlerFunc {
	path := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(name, "static/")))
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

package web

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
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
	s.staticAsset(name, staticContentType(name), limits.MaxBody)(w, r)
}

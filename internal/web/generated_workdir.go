package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"xbot/internal/work"
)

// generatedPath accepts no client path. Like WriteGenerated it resolves the
// fixed directory through the bot workspace root. Refuse symlinks as the
// desktop file manager will later open a path, rather than an open descriptor.
func generatedPath(dir string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := root.MkdirAll(work.GeneratedDir, 0700); err != nil {
		return "", err
	}
	info, err := root.Lstat(work.GeneratedDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("generated must be a directory inside the bot workspace")
	}
	return filepath.Abs(filepath.Join(dir, work.GeneratedDir))
}

func (s *Server) generatedWorkdir(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	dir, _, err := work.Open(s.Root, b.Name())
	if err == nil {
		dir, err = generatedPath(dir)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": dir, "can_open": s.App && s.OpenPath != nil})
}

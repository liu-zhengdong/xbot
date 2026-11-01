package web

import (
	"encoding/json"
	"net/http"
	"runtime"
	"xbot/internal/desktop"
)

func (s *Server) desktopStatus(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		writeJSON(w, 200, map[string]any{"platform": runtime.GOOS, "legacy": true})
		return
	}
	raw, err := desktop.Status(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"platform": runtime.GOOS, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"platform": runtime.GOOS, "status": json.RawMessage(raw)})
}

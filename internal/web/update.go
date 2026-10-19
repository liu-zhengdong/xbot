package web

import (
	"net/http"

	"xbot/internal/version"
)

// registerUpdateAPI mounts the update panel's two calls: what the updater
// knows, and the owner's click that installs a downloaded version.
func (s *Server) registerUpdateAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		if !s.require(w, r) {
			return
		}
		if s.Updater == nil {
			writeJSON(w, http.StatusOK, map[string]string{"state": "idle", "current": displayVersion(version.Version)})
			return
		}
		writeJSON(w, http.StatusOK, s.Updater.Status())
	})
	mux.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if !s.require(w, r) {
			return
		}
		if s.Updater == nil {
			writeErr(w, http.StatusConflict, "这个进程不检查更新")
			return
		}
		// A successful install ends this process: the answer below is what
		// the page sees when it did not.
		if err := s.Updater.Install(); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.Updater.Status())
	})
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if !s.require(w, r) {
			return
		}
		if s.Updater == nil {
			writeErr(w, http.StatusConflict, "这个进程不检查更新")
			return
		}
		if err := s.Updater.Check(r.Context()); err != nil {
			writeErr(w, http.StatusOK, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.Updater.Status())
	})
}

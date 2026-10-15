package web

import (
	"net/http"

	"xbot/internal/config"
)

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	all, err := queryBool(r.URL.Query().Get("all"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad all")
		return
	}
	models, err := config.VisibleModels(s.Root, all)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

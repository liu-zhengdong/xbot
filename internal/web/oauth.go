package web

import (
	"net/http"

	"xbot/internal/providerauth"
)

func (s *Server) oauthManager() *providerauth.Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth == nil {
		s.oauth = &providerauth.Manager{Root: s.Root}
	}
	return s.oauth
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	start, err := s.oauthManager().Begin(r.PathValue("kind"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, start)
}
func (s *Server) oauthStatus(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if kind := r.PathValue("kind"); kind != "openrouter" && kind != "chatgpt" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, s.oauthManager().Status(r.PathValue("kind")))
}

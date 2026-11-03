package web

import (
	"net/http"

	"xbot/internal/providerauth"
)

// oauthKinds are the OAuth providers the UI offers. Claude subscription OAuth
// is deliberately absent: third-party subscription login risks account bans.
var oauthKinds = map[string]bool{"openrouter": true, "chatgpt": true, "xai": true}

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
	if !oauthKinds[r.PathValue("kind")] {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, s.oauthManager().Status(r.PathValue("kind")))
}

// oauthCancel stops a pending or failed login for the same owner who can
// start one. Cancelling frees the loopback listener for an immediate retry.
func (s *Server) oauthCancel(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if !oauthKinds[r.PathValue("kind")] {
		http.NotFound(w, r)
		return
	}
	if err := s.oauthManager().Cancel(r.PathValue("kind")); err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}

// oauthLocal reports whether this machine holds a CLI sign-in. On the very
// first call for a kind it also imports that login straight away — the
// subscription then works without any click. Later calls only report what
// is still on disk, so a login the user removed is not pushed back.
func (s *Server) oauthLocal(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if !oauthKinds[r.PathValue("kind")] {
		http.NotFound(w, r)
		return
	}
	found, imported, err := s.oauthManager().ScanOrImportLocal(r.Context(), r.PathValue("kind"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if found.Source == "" {
		writeJSON(w, 200, map[string]any{"available": false})
		return
	}
	writeJSON(w, 200, map[string]any{"available": true, "imported": imported, "source": found.Source, "account": found.Account})
}

// oauthLogout forgets the account for kind: the vault credential and the
// OAuth binding go away and the provider reads as not logged in again.
func (s *Server) oauthLogout(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if !oauthKinds[r.PathValue("kind")] {
		http.NotFound(w, r)
		return
	}
	if err := s.oauthManager().Logout(r.PathValue("kind")); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "logged_out"})
}

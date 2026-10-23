package vault

import (
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Server) handleOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, errors.New("vault: method not allowed"))
		return
	}
	st, ok := s.botOf(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSetBody)
	var in setRequest
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeErr(w, 400, errors.New("vault: invalid OAuth request"))
		return
	}
	if r.URL.Path == "/v1/oauth/set" {
		c, ok := oauthValue(in.Value)
		if !ok {
			writeErr(w, 400, errors.New("vault: invalid OAuth credential"))
			return
		}
		if err := SetOAuth(st, in.Name, c, in.Domains); err != nil {
			writeErr(w, 400, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	var info OAuthInfo
	var err error
	if r.URL.Path == "/v1/oauth/info" {
		info, err = OAuthMetadata(r.Context(), st, in.Name)
	} else {
		info, err = EnsureOAuth(r.Context(), st, in.Name)
	}
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

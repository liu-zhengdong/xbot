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

// Catalog reads are explicit: opening a provider detail or retrying fetches once.
func (s *Server) providerModels(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	models, err := config.ProviderModels(r.Context(), s.Root, r.URL.Query().Get("provider"))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// putImageGen switches vendor-hosted image generation on one registered model.
// The setting is read on the next turn, so no kernel needs rebuilding.
func (s *Server) putImageGen(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Provider   string `json:"provider"`
		Model      string `json:"model"`
		Enabled    bool   `json:"enabled"`
		ImageModel string `json:"image_model"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad image generation setting")
		return
	}
	if err := config.SetImageGen(s.Root, in.Provider, in.Model, in.Enabled, in.ImageModel); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_, spec, err := config.Lookup(s.Root, in.Provider, in.Model)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": in.Provider, "model": in.Model, "hosted_image_generation": spec.HostedImageGen != nil && *spec.HostedImageGen, "image_generation_model": spec.ImageGenModel})
}

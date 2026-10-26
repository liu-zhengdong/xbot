package web

import (
	"net/http"

	"xbot/internal/config"
)

type imageGenProviderView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LoggedIn bool   `json:"logged_in"`
}

func (s *Server) getImageGen(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	f, err := config.LoadWeb(s.Root)
	if err != nil {
		writeErr(w, 500, "没能读取图片生成设置")
		return
	}
	providers := []imageGenProviderView{{ID: "openai", Name: "OpenAI"}, {ID: "xai", Name: "Grok（xAI）"}}
	selected := f.ImageGenProvider
	for i := range providers {
		p := &providers[i]
		p.LoggedIn, err = config.ImageGenLoggedIn(r.Context(), s.Root, p.ID)
		if err != nil {
			writeErr(w, 500, "没能读取账号登录状态")
			return
		}
		if selected == "" && p.LoggedIn {
			selected = p.ID
		}
	}
	if selected == "" {
		selected = "openai"
	}
	writeJSON(w, 200, struct {
		Enabled   bool                   `json:"enabled"`
		Provider  string                 `json:"provider"`
		Providers []imageGenProviderView `json:"providers"`
	}{f.ImageGenOn(), selected, providers})
}

func (s *Server) putImageGenSettings(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Enabled  *bool  `json:"enabled"`
		Provider string `json:"provider"`
	}
	if !readSearchInput(w, r, &in) {
		return
	}
	if in.Enabled == nil || config.ImageGenOAuth(in.Provider) == "" {
		writeErr(w, 400, "请检查图片生成选项")
		return
	}
	if err := config.SetWebImageGen(s.Root, *in.Enabled, in.Provider); err != nil {
		writeErr(w, 500, "没能保存图片生成设置，请再试一次")
		return
	}
	s.getImageGen(w, r)
}

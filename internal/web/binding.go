package web

import (
	"net/http"
	"strings"

	"xbot/internal/config"
	"xbot/internal/kernel"
	"xbot/internal/vault"
)

// providerBrands are the brand ids that have a grid icon.
func providerBrands() []string {
	out := []string{"deepseek", "openai", "anthropic", "openrouter"}
	for _, v := range config.Vendors {
		out = append(out, v.Brand)
	}
	return out
}

type vendorItem struct {
	Brand    string   `json:"brand"`
	Title    string   `json:"title"`
	Protocol string   `json:"protocol"`
	KeyLogin bool     `json:"key_login,omitempty"`
	Models   []string `json:"models"`
}

// vendorList is the built-in vendor table the grid can add. It has no key env
// name or URL: the page needs neither.
func vendorList() []vendorItem {
	out := make([]vendorItem, 0, len(config.Vendors))
	for _, v := range config.Vendors {
		names := make([]string, 0, len(v.Models))
		for _, m := range v.Models {
			names = append(names, m.Name)
		}
		out = append(out, vendorItem{Brand: v.Brand, Title: v.Title, Protocol: v.Protocol, KeyLogin: v.KeyLogin, Models: names})
	}
	return out
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	f, err := config.Load(s.Root)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	models, err := config.VisibleModels(s.Root, true)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type item struct {
		Brand         string             `json:"brand,omitempty"`
		Name          string             `json:"name"`
		Protocol      string             `json:"protocol"`
		KeyEnv        string             `json:"api_key_env"`
		KeyConfigured bool               `json:"key_configured"`
		OAuth         string             `json:"oauth,omitempty"`
		OAuthError    string             `json:"auth_error,omitempty"`
		Models        []config.ModelInfo `json:"models"`
	}
	out := []item{}
	for _, p := range f.Providers {
		row := item{Brand: config.BrandForBase(p.BaseURL), Name: p.Name, Protocol: p.WireProtocol(), KeyEnv: p.APIKeyEnv, KeyConfigured: strings.TrimSpace(config.Getenv(p.APIKeyEnv)) != "", Models: []config.ModelInfo{}}
		if p.OAuth != "" {
			row.OAuth = p.OAuth
			backend, err := config.OAuthVault(s.Root)
			if err != nil {
				row.OAuthError = "保险箱不可用"
			} else {
				info, err := vault.OAuthMetadata(r.Context(), backend, p.OAuth)
				if err != nil {
					row.OAuthError = "保险箱不可用"
				} else {
					row.KeyConfigured = info.Configured
				}
			}
		}
		for _, m := range models {
			if m.Provider == p.Name {
				row.Models = append(row.Models, m)
			}
		}
		out = append(out, row)
	}
	writeJSON(w, 200, map[string]any{"providers": out, "vendors": vendorList()})
}

// Binding changes only while the bot's wake lock is free. Existing work keeps
// its provider and is never cancelled merely to change a binding.
func (s *Server) bindModel(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "bad binding")
		return
	}
	in.Provider = strings.TrimSpace(in.Provider)
	in.Model = strings.TrimSpace(in.Model)
	_, window, err := config.Resolve(s.Root, in.Provider, in.Model, config.Getenv)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	lock, ok, err := b.TryLockHandle()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 409, "Bot 正在工作，请结束后再换绑定")
		return
	}
	defer lock.Release()
	s.mu.Lock()
	k := s.kernels[b.Name()]
	s.mu.Unlock()
	if k != nil {
		if err := k.CheckBindingIdle(); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
	}
	checker := k
	if checker == nil {
		checker = &kernel.Kernel{Bot: b}
		if s.NewKernel != nil && checker.BindingShrinksWindow(window) {
			checker, err = s.NewKernel(b)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			defer checker.Close()
		}
	}
	if err := checker.PrepareBinding(r.Context(), in.Provider, in.Model, window); err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kernels[b.Name()] != k {
		writeErr(w, 409, "Bot 状态已变化，请重试")
		return
	}
	if err := b.SetBinding(in.Provider, in.Model); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	delete(s.kernels, b.Name())
	delete(s.gate, b.Name())
	writeJSON(w, 200, map[string]string{"provider": in.Provider, "model": in.Model})
}

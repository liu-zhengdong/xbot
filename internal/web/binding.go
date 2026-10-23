package web

import (
	"net/http"
	"strings"

	"xbot/internal/config"
)

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
		Name          string             `json:"name"`
		Protocol      string             `json:"protocol"`
		KeyEnv        string             `json:"api_key_env"`
		KeyConfigured bool               `json:"key_configured"`
		Models        []config.ModelInfo `json:"models"`
	}
	out := []item{}
	m := config.FromEnv(config.Getenv)
	out = append(out, item{Name: "", Protocol: config.ChatCompletions, KeyEnv: "XBOT_API_KEY", KeyConfigured: strings.TrimSpace(m.APIKey) != "", Models: []config.ModelInfo{{ID: "/" + m.Model, Model: m.Model, Protocol: config.ChatCompletions, Context: 0, Vision: config.SeesImages(m.Model, nil), Available: true}}})
	for _, p := range f.Providers {
		row := item{Name: p.Name, Protocol: p.WireProtocol(), KeyEnv: p.APIKeyEnv, KeyConfigured: strings.TrimSpace(config.Getenv(p.APIKeyEnv)) != "", Models: []config.ModelInfo{}}
		for _, m := range models {
			if m.Provider == p.Name {
				row.Models = append(row.Models, m)
			}
		}
		out = append(out, row)
	}
	writeJSON(w, 200, map[string]any{"providers": out})
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
	if _, _, err := config.Resolve(s.Root, in.Provider, in.Model, config.Getenv); err != nil {
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

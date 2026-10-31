package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"xbot/internal/compact"
	"xbot/internal/config"
)

func (s *Server) inferProvider(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	brand, title, protocol := config.InferProvider(r.URL.Query().Get("base_url"))
	writeJSON(w, 200, map[string]string{"brand": brand, "title": title, "protocol": protocol})
}

// customProvider reads existing CLI entries as well as entries created by settings.
func (s *Server) customProvider(name string) (config.Provider, bool) {
	f, err := config.Load(s.Root)
	if err != nil {
		return config.Provider{}, false
	}
	for _, p := range f.Providers {
		if p.Name == name && config.IsCustomProvider(p) {
			return p, true
		}
	}
	return config.Provider{}, false
}

func (s *Server) saveCustomProvider(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		BaseURL     string `json:"base_url"`
		Key         string `json:"key"`
		DisplayName string `json:"display_name"`
		Protocol    string `json:"protocol"`
	}
	if decode(r, &in) != nil {
		writeErr(w, 400, "无法读取供应商信息")
		return
	}
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Key = strings.TrimSpace(in.Key)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.BaseURL == "" {
		writeErr(w, 400, "请填写 Base URL")
		return
	}
	if len(in.DisplayName) > 128 || strings.ContainsAny(in.DisplayName, "\r\n") {
		writeErr(w, 400, "展示名请保持在 128 字节以内")
		return
	}
	s.vendorMu.Lock()
	defer s.vendorMu.Unlock()
	name := r.PathValue("name")
	p := config.Provider{}
	if name != "" {
		var ok bool
		p, ok = s.customProvider(name)
		if !ok {
			writeErr(w, 404, "没有这个自定义供应商")
			return
		}
	} else {
		id := make([]byte, 8)
		if _, err := rand.Read(id); err != nil {
			writeErr(w, 500, "未能创建供应商")
			return
		}
		name = "custom-" + hex.EncodeToString(id)
		p.Name = name
		p.APIKeyEnv = "XBOT_CUSTOM_" + strings.ToUpper(hex.EncodeToString(id)) + "_API_KEY"
	}
	if in.Key == "" && config.Getenv(p.APIKeyEnv) == "" {
		writeErr(w, 400, "请填写 API 密钥")
		return
	}
	if in.Key != "" && (len(in.Key) > 512 || strings.ContainsAny(in.Key, " \t\r\n\"'#")) {
		writeErr(w, 400, "请整段粘贴密钥，不要带空格或引号")
		return
	}
	p.BaseURL = in.BaseURL
	p.Custom = true
	p.DisplayName = in.DisplayName
	p.Protocol = in.Protocol
	if p.Protocol == "" {
		_, _, p.Protocol = config.InferProvider(p.BaseURL)
	}
	// Validate and register at the same seam as CLI before storing the key.
	if err := config.Add(s.Root, p); err != nil {
		writeErr(w, 400, "地址或协议无效，请填写完整的 http(s) Base URL，不带用户名、查询参数或片段")
		return
	}
	if in.Key != "" {
		if err := config.SaveEnvKey(s.Root, p.APIKeyEnv, in.Key); err != nil {
			writeErr(w, 422, "未能保存密钥，请重试")
			return
		}
		config.SetHeld(p.APIKeyEnv, in.Key)
	}
	writeJSON(w, 200, map[string]string{"provider": name})
}

func (s *Server) deleteCustomProvider(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	s.vendorMu.Lock()
	defer s.vendorMu.Unlock()
	p, ok := s.customProvider(r.PathValue("name"))
	if !ok {
		writeErr(w, 404, "没有这个自定义供应商")
		return
	}
	f, err := config.Load(s.Root)
	if err != nil {
		writeErr(w, 500, "未能读取供应商")
		return
	}
	shared := false
	for _, other := range f.Providers {
		if other.Name != p.Name && other.APIKeyEnv == p.APIKeyEnv {
			shared = true
		}
	}
	if !shared {
		if err := config.RemoveEnvKey(s.Root, p.APIKeyEnv); err != nil {
			writeErr(w, 500, "未能删除密钥")
			return
		}
		config.SetHeld(p.APIKeyEnv, "")
	}
	if err := config.Remove(s.Root, p.Name, ""); err != nil {
		writeErr(w, 500, "未能删除供应商")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) customProviderModel(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Model   string `json:"model"`
		Enabled bool   `json:"enabled"`
		Context int    `json:"context"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Model) == "" || len(in.Model) > 256 {
		writeErr(w, 400, "请填写模型 id")
		return
	}
	s.vendorMu.Lock()
	defer s.vendorMu.Unlock()
	p, ok := s.customProvider(r.PathValue("name"))
	if !ok {
		writeErr(w, 404, "没有这个自定义供应商")
		return
	}
	var err error
	if in.Enabled {
		if in.Context <= 0 {
			in.Context = compact.DefaultWindow
		}
		err = config.Add(s.Root, config.Provider{Name: p.Name}, config.ModelSpec{Name: strings.TrimSpace(in.Model), Context: in.Context})
	} else {
		err = config.Remove(s.Root, p.Name, in.Model)
	}
	if err != nil {
		writeErr(w, 400, "未能更新模型，请重试")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

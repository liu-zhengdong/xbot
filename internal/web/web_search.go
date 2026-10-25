package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"xbot/internal/config"
	"xbot/internal/tools"
)

type searchSettings struct {
	Enabled   bool                 `json:"enabled"`
	Mode      string               `json:"mode"`
	Provider  string               `json:"provider"`
	Providers []searchProviderView `json:"providers"`
}
type searchProviderView struct {
	config.SearchProvider
	KeyConfigured bool `json:"key_configured"`
}

func (s *Server) searchSettings() (searchSettings, error) {
	f, err := config.LoadWeb(s.Root)
	if err != nil {
		return searchSettings{}, err
	}
	mode := f.SearchMode
	if mode == "" {
		mode = "auto"
	}
	out := searchSettings{Enabled: f.SearchOn(), Mode: mode, Provider: f.SearchProviderID()}
	for _, p := range config.SearchProviders() {
		key, err := config.SearchCredential(s.Root, f, p.ID)
		if err != nil {
			return searchSettings{}, err
		}
		out.Providers = append(out.Providers, searchProviderView{p, p.NeedsKey && key != ""})
	}
	return out, nil
}
func (s *Server) getWebSearch(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	out, err := s.searchSettings()
	if err != nil {
		writeErr(w, 500, "没能读取网页搜索设置")
		return
	}
	writeJSON(w, 200, out)
}
func readSearchInput(w http.ResponseWriter, r *http.Request, in any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(in); err != nil {
		writeErr(w, 400, "请检查输入后再试")
		return false
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeErr(w, 400, "请检查输入后再试")
		return false
	}
	return true
}
func (s *Server) putWebSearch(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Enabled  *bool  `json:"enabled"`
		Mode     string `json:"mode"`
		Provider string `json:"provider"`
	}
	if !readSearchInput(w, r, &in) {
		return
	}
	if in.Enabled == nil || (in.Mode != "auto" && in.Mode != "local") {
		writeErr(w, 400, "请选择怎么搜")
		return
	}
	if _, ok := config.SearchProviderByID(in.Provider); !ok {
		writeErr(w, 400, "请从列表里选择一家")
		return
	}
	if err := config.SetWebSearch(s.Root, *in.Enabled, in.Mode, in.Provider); err != nil {
		writeErr(w, 500, "没能保存网页搜索设置，请再试一次")
		return
	}
	s.getWebSearch(w, r)
}
func (s *Server) testWebSearchKey(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if !readSearchInput(w, r, &in) {
		return
	}
	p, ok := config.SearchProviderByID(in.Provider)
	if !ok || !p.NeedsKey {
		writeErr(w, 400, "请选择需要密钥的一家")
		return
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		f, err := config.LoadWeb(s.Root)
		if err != nil {
			writeErr(w, 500, "没能读取网页搜索设置")
			return
		}
		key, err = config.SearchCredential(s.Root, f, p.ID)
		if err != nil {
			writeErr(w, 500, "没能读取网页搜索设置")
			return
		}
	}
	if len(key) < 8 || len(key) > 512 || strings.ContainsAny(key, " \t\r\n\"'#") {
		writeErr(w, 400, "密钥无效或连不上")
		return
	}
	// Serialize probes and commits. A cancelled or superseded UI request never
	// writes its key, and failed probes leave the last usable credential intact.
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	probe := s.searchProbe
	if probe == nil {
		probe = probeSearchKey
	}
	if err := probe(ctx, p.ID, key); err != nil || ctx.Err() != nil {
		writeErr(w, 422, "密钥无效或连不上")
		return
	}
	if err := config.SaveSearchCredential(s.Root, p.ID, key); err != nil {
		writeErr(w, 500, "密钥可用，但没能保存在本机，请再试一次")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// Only the official search endpoint receives the typed credential; request
// bodies cannot supply a URL. The existing guarded search path bounds output.
func probeSearchKey(ctx context.Context, id, key string) error {
	if id != "deepseek" {
		return errSearchUnavailable{}
	}
	b, err := tools.NewSearch(id, "", key)
	if err != nil {
		return err
	}
	_, err = b.Search(ctx, "XBot", 1)
	return err
}

type errSearchUnavailable struct{}

func (errSearchUnavailable) Error() string { return "这家暂时还不能搜网页，请选其它家" }

package web

import (
	"errors"
	"net/http"
	"strings"

	"xbot/internal/config"
)

// putVendorKey adds a built-in vendor's defaults to the registry and stores its
// key in the env file. The key never enters providers.json, and the base URL
// comes from the built-in table, never from the request. Repeating the call
// with the same brand only replaces the key.
func (s *Server) putVendorKey(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	key := strings.TrimSpace(in.Key)
	if len(key) < 8 || len(key) > 512 || strings.ContainsAny(key, " \t\r\n\"'#") {
		writeErr(w, http.StatusBadRequest, "这不像一个 API key：请整段粘贴，不要带空格或引号")
		return
	}
	brand := r.PathValue("brand")
	// One vendor write at a time: the registry and env file are read-modify-write.
	s.vendorMu.Lock()
	defer s.vendorMu.Unlock()
	p, err := config.AddVendor(s.Root, brand)
	if errors.Is(err, config.ErrNoKeyRoute) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		if _, known := config.VendorFor(brand); !known {
			writeErr(w, http.StatusNotFound, "没有这家供应商")
			return
		}
		writeErr(w, http.StatusInternalServerError, config.RedactHeld(err.Error()))
		return
	}
	if p.OAuth != "" || p.APIKeyEnv == "" {
		writeErr(w, http.StatusConflict, "这家供应商用登录凭证，不能填密钥")
		return
	}
	if err := config.SaveEnvKey(s.Root, p.APIKeyEnv, key); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "没能保存密钥："+config.RedactHeld(err.Error()))
		return
	}
	config.SetHeld(p.APIKeyEnv, key)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "provider": p.Name})
}

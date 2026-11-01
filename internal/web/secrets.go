package web

import (
	"net/http"

	"xbot/internal/vault"
)

// Explicit metadata DTO: even an erroneous backend List cannot serialize Value.
type secretJSON struct {
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

func (s *Server) globalSecrets(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	backend, err := vault.OpenGlobal(s.Root)
	if err != nil {
		writeErr(w, 500, "没能打开保险库")
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows := make([]secretJSON, 0)
		for _, sec := range backend.List() {
			rows = append(rows, secretJSON{Name: sec.Name, Domains: sec.Domains})
		}
		writeJSON(w, 200, map[string]any{"secrets": rows})
	case http.MethodPost:
		var in struct {
			Name    string   `json:"name"`
			Value   *string  `json:"value"`
			Domains []string `json:"domains"`
		}
		if decode(r, &in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		var err error
		if in.Value == nil {
			err = backend.SetDomains(in.Name, in.Domains)
		} else {
			err = backend.Set(in.Name, *in.Value, in.Domains)
		}
		if err != nil {
			writeErr(w, 400, "没能保存，请填写名称和密钥")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodDelete:
		if err := backend.Remove(r.PathValue("secret")); err != nil {
			writeErr(w, 500, "没能删除密钥")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

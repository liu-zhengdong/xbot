package web

import (
	"errors"
	"net/http"
	"os"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbound"
)

// webhookView is one hook as the task tab shows it. url is the address to
// hand out: the public one when there is one, else the local one. Secret is
// filled only on create and on the detail read.
type webhookView struct {
	Name      string    `json:"name"`
	Auth      string    `json:"auth"`
	URL       string    `json:"url"`
	LocalURL  string    `json:"local_url"`
	PublicURL string    `json:"public_url,omitempty"`
	Created   time.Time `json:"created"`
	Secret    string    `json:"secret,omitempty"`
}

func (s *Server) webhookView(botName string, h inbound.Hook, secret bool) webhookView {
	local, public := "http://127.0.0.1:8787", ""
	if s.WebhookBase != nil {
		local, public = s.WebhookBase()
	}
	v := webhookView{Name: h.Name, Auth: h.Auth, LocalURL: inbound.WebhookURL(local, botName, h.Name), Created: h.Created}
	v.URL = v.LocalURL
	if public != "" {
		v.PublicURL = inbound.WebhookURL(public, botName, h.Name)
		v.URL = v.PublicURL
	}
	if secret {
		v.Secret = h.Secret
	}
	return v
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	hooks, err := inbound.ListHooks(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读不到 Webhook")
		return
	}
	out := make([]webhookView, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, s.webhookView(b.Name(), h, false))
	}
	var public string
	if s.WebhookBase != nil {
		_, public = s.WebhookBase()
	}
	writeJSON(w, http.StatusOK, map[string]any{"hooks": out, "public": public})
}

func (s *Server) addWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Name string `json:"name"`
		Auth string `json:"auth"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if !inbound.ValidID(in.Name) {
		writeErr(w, http.StatusBadRequest, "用小写字母、数字和短横线，不要大写或下划线")
		return
	}
	if in.Auth != "" && in.Auth != inbound.AuthHMAC && in.Auth != inbound.AuthBearer {
		writeErr(w, http.StatusBadRequest, "校验方式只能是签名校验或令牌")
		return
	}
	// AddHook records the hook, so the owner
	// never installs it by hand.
	s.skillMu.Lock()
	h, err := inbound.AddHook(b, in.Name, in.Auth, time.Now())
	s.skillMu.Unlock()
	if errors.Is(err, inbound.ErrHookExists) {
		writeErr(w, http.StatusConflict, "已有同名 Webhook")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没建成")
		return
	}
	writeJSON(w, http.StatusCreated, s.webhookView(b.Name(), h, true))
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) {
	b, h, ok := s.ownedHook(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.webhookView(b.Name(), h, true))
}

func (s *Server) removeWebhook(w http.ResponseWriter, r *http.Request) {
	b, h, ok := s.ownedHook(w, r)
	if !ok {
		return
	}
	// RemoveHook drops the named hook only.
	s.skillMu.Lock()
	err := inbound.RemoveHook(b, h.Name)
	s.skillMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没撤掉")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ownedHook(w http.ResponseWriter, r *http.Request) (*bot.Bot, inbound.Hook, bool) {
	if !s.require(w, r) {
		return nil, inbound.Hook{}, false
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return nil, inbound.Hook{}, false
	}
	name := r.PathValue("hook")
	if !inbound.ValidID(name) {
		writeErr(w, http.StatusNotFound, "not found")
		return nil, inbound.Hook{}, false
	}
	for _, h := range b.Config().Webhooks {
		if h != name {
			continue
		}
		hook, err := inbound.LoadHook(b, name)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读不到 Webhook")
			return nil, inbound.Hook{}, false
		}
		return b, hook, true
	}
	writeErr(w, http.StatusNotFound, "not found")
	return nil, inbound.Hook{}, false
}

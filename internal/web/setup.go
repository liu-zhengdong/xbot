package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xbot/internal/config"
)

// The desktop app's first-run screen asks for the model API key, tests it
// against the configured endpoint, and saves it in <root>/env (mode 0600).
// It never sets a process, user or system environment variable. An existing
// env file or XBOT_API_KEY keeps working and skips the screen.

// needsKey reports whether the desktop app should show the first-run screen:
// no XBOT_API_KEY anywhere and no provider registry of its own.
func (s *Server) needsKey() bool {
	if !s.App || strings.TrimSpace(config.Getenv("XBOT_API_KEY")) != "" {
		return false
	}
	f, err := config.Load(s.Root)
	return err == nil && len(f.Providers) == 0
}

// keyHost is the host the key test talks to, shown on the screen.
func keyHost() string {
	u, err := url.Parse(config.FromEnv(config.Getenv).BaseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func (s *Server) setupKey(w http.ResponseWriter, r *http.Request) {
	if !s.App {
		http.NotFound(w, r)
		return
	}
	if !s.require(w, r) {
		return
	}
	// One setup at a time, and only while no key exists, so the file on disk
	// and the key held in memory cannot end up different.
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if !s.needsKey() {
		writeErr(w, http.StatusConflict, "已经有模型密钥了")
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
	if msg, code := s.testKey(r.Context(), key); msg != "" {
		writeErr(w, code, msg)
		return
	}
	if err := config.SaveEnvKey(s.Root, "XBOT_API_KEY", key); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "密钥测试通过，但没能保存："+config.RedactHeld(err.Error()))
		return
	}
	config.SetHeld("XBOT_API_KEY", key)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// testKey lists models with the key. It talks only to the configured base
// URL (XBOT_BASE_URL or the default), never to a host from the request.
// The returned message never contains the key. Errors are 4xx so the UI shows
// the message rather than a generic server error.
func (s *Server) testKey(ctx context.Context, key string) (string, int) {
	base := config.FromEnv(config.Getenv).BaseURL
	host := keyHost()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return "模型地址不对：" + host, http.StatusUnprocessableEntity
	}
	req.Header.Set("Authorization", "Bearer "+key)
	// No redirects: Go would forward Authorization to another port or scheme
	// on the same host, which is not the endpoint the user configured.
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("连不上 %s，检查网络后再试", host), http.StatusUnprocessableEntity
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	switch {
	case res.StatusCode == http.StatusOK:
		return "", 0
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Sprintf("%s 不认这个密钥（HTTP %d），请检查后重新粘贴", host, res.StatusCode), http.StatusBadRequest
	default:
		return fmt.Sprintf("测试没通过：%s 返回 HTTP %d，稍后再试", host, res.StatusCode), http.StatusUnprocessableEntity
	}
}

// Package providerauth connects browser authorization to the existing provider
// registry and vault. It implements no model protocol or outbound proxy.
package providerauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"xbot/internal/config"
	"xbot/internal/fsutil"
	"xbot/internal/vault"
)

type Endpoints struct{ Authorize, Token, API, Issuer, JWKS string }

// xaiClientID is the Grok CLI public OAuth client. xAI has no third-party
// developer console to register one; every open client uses this id.
const xaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"

// xaiCallbackAddr is the fixed Grok CLI loopback redirect target. A dynamic
// port would break the registered redirect_uri.
const xaiCallbackAddr = "127.0.0.1:56121"

func defaults(kind string) (Endpoints, error) {
	switch kind {
	case "openrouter":
		return Endpoints{Authorize: "https://openrouter.ai/auth", Token: "https://openrouter.ai/api/v1/auth/keys", API: "https://openrouter.ai/api/v1"}, nil
	case "chatgpt":
		return Endpoints{Authorize: "https://auth.openai.com/api/accounts/authorize", Token: "https://auth.openai.com/api/accounts/oauth/token", API: "https://api.openai.com/v1", Issuer: "https://auth.openai.com", JWKS: "https://auth.openai.com/.well-known/jwks.json"}, nil
	case "xai":
		return Endpoints{Authorize: "https://auth.x.ai/oauth2/authorize", Token: "https://auth.x.ai/oauth2/token", API: "https://api.x.ai/v1", Issuer: "https://auth.x.ai", JWKS: "https://auth.x.ai/.well-known/jwks.json"}, nil
	default:
		return Endpoints{}, errors.New("不支持此登录方式")
	}
}

type Manager struct {
	Root string
	// Endpoints is an in-process test seam, never supplied by the browser.
	Endpoints map[string]Endpoints
	// CallbackPort is a test seam for xAI's fixed loopback port, so parallel
	// test binaries do not fight over it. Zero uses the real port. Never
	// supplied by the browser.
	CallbackPort int
	mu           sync.Mutex
	pending      *attempt
}
type attempt struct {
	kind, state, verifier, nonce, callback, clientID, subject, authURL string
	endpoints                                                          Endpoints
	expires                                                            time.Time
	status                                                             Status
	server                                                             *http.Server
}
type Status struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}
type Start struct {
	URL string `json:"url"`
}

func random() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (m *Manager) Begin(kind string) (Start, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, err := defaults(kind)
	if err != nil {
		return Start{}, err
	}
	if m.pending != nil && m.pending.status.State == "pending" && time.Now().Before(m.pending.expires) {
		if m.pending.kind != kind {
			return Start{}, errors.New("另一个登录正在进行，请先完成或等候过期")
		}
		return Start{m.pending.authURL}, nil
	}
	if override, ok := m.Endpoints[kind]; ok {
		e = override
	}
	backend, err := config.OAuthVault(m.Root)
	if err != nil {
		return Start{}, err
	}
	info, err := vault.OAuthMetadata(context.Background(), backend, kind)
	if err != nil {
		return Start{}, err
	}
	f, err := config.Load(m.Root)
	if err != nil {
		return Start{}, err
	}
	for _, p := range f.Providers {
		if p.Name == kind && p.OAuth != kind {
			return Start{}, errors.New("同名供应商已使用 API key，请先更名")
		}
	}
	ln, err := net.Listen("tcp4", xaiListenAddr(m, kind))
	if err != nil {
		if kind == "xai" {
			return Start{}, errors.New("本机 56121 端口被占用，请关闭占用进程后重试")
		}
		return Start{}, errors.New("无法打开本机登录回调")
	}
	a := &attempt{kind: kind, state: random(), verifier: random(), nonce: random(), clientID: info.ClientID, subject: info.Subject, endpoints: e, expires: time.Now().Add(10 * time.Minute), status: Status{State: "pending"}}
	a.callback = "http://" + ln.Addr().String() + "/auth/callback"
	if kind == "xai" {
		a.callback = "http://" + ln.Addr().String() + "/callback"
	}
	q := url.Values{"state": {a.state}, "code_challenge_method": {"S256"}}
	hash := sha256.Sum256([]byte(a.verifier))
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:]))
	if kind == "openrouter" {
		// OpenRouter returns top-level state unchanged on successful callbacks:
		// https://openrouter.ai/docs/guides/overview/auth/oauth
		// A denial does not echo it, so bind state into the callback URL too.
		a.callback += "?" + url.Values{"state": {a.state}}.Encode()
		q.Set("callback_url", a.callback)
	} else if kind == "xai" {
		// The Grok CLI public client with a fixed loopback redirect. plan=generic
		// is required or the loopback redirect is rejected; referrer is attribution.
		a.clientID = xaiClientID
		q.Set("client_id", a.clientID)
		q.Set("response_type", "code")
		q.Set("redirect_uri", a.callback)
		q.Set("nonce", a.nonce)
		q.Set("scope", "openid profile email offline_access grok-cli:access api:access")
		q.Set("plan", "generic")
		q.Set("referrer", "xbot")
	} else {
		host, err := hostID(m.Root)
		if err != nil {
			ln.Close()
			return Start{}, err
		}
		if a.clientID == "" {
			a.clientID = "dynamic_agent_client"
			q.Set("agent_name_hint", "XBot")
		}
		q.Set("client_id", a.clientID)
		q.Set("ext_agent_host_id", host)
		q.Set("response_type", "code")
		q.Set("redirect_uri", a.callback)
		q.Set("nonce", a.nonce)
		q.Set("scope", "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct")
		q.Set("resource", "https://api.openai.com/v1")
	}
	a.authURL = e.Authorize + "?" + q.Encode()
	a.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.callback(a, w, r) }), ReadHeaderTimeout: 5 * time.Second}
	m.pending = a
	go func() { _ = a.server.Serve(ln) }()
	go func() {
		timer := time.NewTimer(time.Until(a.expires))
		defer timer.Stop()
		<-timer.C
		m.mu.Lock()
		defer m.mu.Unlock()
		if a.status.State == "pending" {
			a.status = Status{State: "failed", Error: "登录已过期，请重试"}
		}
		_ = a.server.Close()
	}()
	return Start{a.authURL}, nil
}

func xaiListenAddr(m *Manager, kind string) string {
	if kind != "xai" {
		return "127.0.0.1:0"
	}
	if m.CallbackPort > 0 {
		return fmt.Sprintf("127.0.0.1:%d", m.CallbackPort)
	}
	return xaiCallbackAddr
}

func (m *Manager) Status(kind string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.kind != kind {
		return Status{State: "idle"}
	}
	return m.pending.status
}

// Cancel stops a pending login for kind: the loopback listener closes and the
// attempt can begin again immediately. A failed or cancelled attempt clears
// back to idle. It returns a human-readable error when nothing is underway.
func (m *Manager) Cancel(kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.kind != kind {
		return errors.New("没有进行中的登录")
	}
	switch m.pending.status.State {
	case "pending":
		m.pending.status = Status{State: "cancelled"}
		_ = m.pending.server.Close()
	case "failed", "cancelled":
		m.pending.status = Status{State: "idle"}
		_ = m.pending.server.Close()
	default:
		return errors.New("没有进行中的登录")
	}
	return nil
}

// Close releases a pending listener when the hosting server shuts down.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		_ = m.pending.server.Close()
	}
}

func hostID(root string) (id string, err error) {
	path := filepath.Join(root, "oauth-host.json")
	err = fsutil.WithLock(path+".lock", func() error {
		raw, e := os.ReadFile(path)
		if e == nil {
			var saved struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &saved) != nil || saved.ID == "" {
				return errors.New("登录主机标识损坏")
			}
			id = saved.ID
			return nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		var b [16]byte
		if _, e = rand.Read(b[:]); e != nil {
			return e
		}
		b[6] = (b[6] & 15) | 64
		b[8] = (b[8] & 63) | 128
		id = fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
		raw, _ = json.Marshal(map[string]string{"id": id})
		return fsutil.WriteAtomic(path, raw, 0600)
	})
	return
}

func (m *Manager) callback(a *attempt, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	// xAI registers /callback; the older kinds use /auth/callback. Both stay
	// accepted so a unified path stays compatible; state still gates access.
	if r.Method != "GET" || (r.URL.Path != "/auth/callback" && r.URL.Path != "/callback") {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != a || a.status.State != "pending" || time.Now().After(a.expires) || r.URL.Query().Get("state") != a.state {
		http.Error(w, "登录校验失败，请重试", 400)
		return
	}
	if r.URL.Query().Get("error") != "" {
		a.status = Status{State: "failed", Error: "授权未完成，请重试"}
		http.Error(w, a.status.Error, 400)
		go closeCallback(a.server)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		http.Error(w, "缺少授权码", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := m.finish(ctx, a, code, r.URL.Query().Get("client_id"))
	if err != nil {
		a.status = Status{State: "failed", Error: err.Error()}
		http.Error(w, err.Error(), 400)
	} else {
		a.status = Status{State: "succeeded"}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "登录成功。请返回 XBot，在模型行选择此供应商。")
	}
	go closeCallback(a.server)
}

func closeCallback(s *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}

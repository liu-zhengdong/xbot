package providerauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/vault"
)

type fakeAuth struct {
	srv                        *httptest.Server
	key                        *rsa.PrivateKey
	t                          *testing.T
	mu                         sync.Mutex
	nonce, challenge, callback string
	access, refresh, orKey     string
	invalid                    string
	// holdToken blocks the token endpoint until released; tokenSeen signals a
	// token request is in flight. Both are nil in ordinary tests.
	holdToken, tokenSeen chan struct{}
	refreshes, requests  atomic.Int32
}

func newFakeAuth(t *testing.T) *fakeAuth {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAuth{t: t, key: key, access: random(), refresh: random(), orKey: random()}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}
func (f *fakeAuth) endpoints(kind string) Endpoints {
	e := Endpoints{Authorize: f.srv.URL + "/authorize", Token: f.srv.URL + "/token", API: f.srv.URL + "/v1", Issuer: f.srv.URL, JWKS: f.srv.URL + "/jwks"}
	if kind == "openrouter" {
		e.Token = f.srv.URL + "/keys"
	}
	return e
}
func (f *fakeAuth) signIn(m *Manager, kind string) string {
	f.t.Helper()
	start, err := m.Begin(kind)
	if err != nil {
		f.t.Fatal(err)
	}
	u, _ := url.Parse(start.URL)
	q := u.Query()
	f.mu.Lock()
	f.nonce = q.Get("nonce")
	f.challenge = q.Get("code_challenge")
	f.callback = q.Get("redirect_uri")
	if kind == "openrouter" {
		f.callback = q.Get("callback_url")
	} else {
		f.callback = q.Get("redirect_uri")
	}
	callback := f.callback
	f.mu.Unlock()
	cb, _ := url.Parse(callback)
	params := cb.Query()
	params.Set("code", random())
	params.Set("client_id", "issued-test-client")
	if kind != "openrouter" {
		params.Set("state", q.Get("state"))
	}
	cb.RawQuery = params.Encode()
	res, err := http.Get(cb.String())
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body)
}
func (f *fakeAuth) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/jwks":
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kid": "test", "kty": "RSA", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/keys":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		digest := sha256.Sum256([]byte(in["code_verifier"]))
		if base64.RawURLEncoding.EncodeToString(digest[:]) != f.challenge || in["code_challenge_method"] != "S256" {
			http.Error(w, "invalid PKCE", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"key": f.orKey})
	case "/token":
		if f.tokenSeen != nil {
			f.tokenSeen <- struct{}{}
		}
		if f.holdToken != nil {
			<-f.holdToken
		}
		_ = r.ParseForm()
		client := "issued-test-client"
		if r.Form.Get("client_id") == xaiClientID {
			client = xaiClientID
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			resource := "https://api.openai.com/v1"
			if client == xaiClientID {
				resource = "" // xAI refreshes carry no resource at all
			}
			if r.Form.Get("refresh_token") != f.refresh || r.Form.Get("client_id") != client || r.Form.Get("resource") != resource {
				http.Error(w, "invalid refresh", 400)
				return
			}
			f.refreshes.Add(1)
			f.access = random()
			f.refresh = random()
		} else {
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(digest[:]) != f.challenge || r.Form.Get("redirect_uri") != f.callback || r.Form.Get("client_id") != client {
				http.Error(w, "invalid PKCE/client", 400)
				return
			}
		}
		nonce := f.nonce
		if f.invalid == "nonce" {
			nonce = random()
		}
		header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test"})
		payload, _ := json.Marshal(map[string]any{"iss": f.srv.URL, "sub": "fake-account", "aud": "issued-test-client", "exp": time.Now().Add(time.Hour).Unix(), "nonce": nonce})
		b64 := base64.RawURLEncoding.EncodeToString
		unsigned := b64(header) + "." + b64(payload)
		hash := sha256.Sum256([]byte(unsigned))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, hash[:])
		if f.invalid == "signature" {
			sig[0] ^= 1
		}
		scope := "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
		if client == xaiClientID {
			scope = "openid profile email offline_access grok-cli:access api:access"
		}
		if f.invalid == "scope" {
			scope = "openid"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "refresh_token": f.refresh, "id_token": unsigned + "." + b64(sig), "token_type": "Bearer", "expires_in": 3600, "scope": scope})
	case "/v1/models":
		if r.Header.Get("Authorization") != "Bearer "+f.access {
			http.Error(w, "unauthorized", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"slug": "fake/scripted", "visibility": "list", "context_window": 1000000}}})
	case "/v1/responses", "/v1/chat/completions":
		want := f.access
		if r.URL.Path == "/v1/chat/completions" {
			want = f.orKey
		}
		if r.Header.Get("Authorization") != "Bearer "+want {
			http.Error(w, "unauthorized", 401)
			return
		}
		f.requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/v1/responses" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["store"] != false || body["stream"] != true {
				http.Error(w, "wrong responses flags", 400)
				return
			}
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"oauth-ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg1\",\"content\":[{\"type\":\"output_text\",\"text\":\"oauth-ok\"}]}]}}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"oauth-ok\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	default:
		http.NotFound(w, r)
	}
}

func TestOAuthLoginBindingRefreshAndRestart(t *testing.T) {
	for _, kind := range []string{"openrouter", "chatgpt"} {
		t.Run(kind, func(t *testing.T) {
			f := newFakeAuth(t)
			root := t.TempDir()
			m := &Manager{Root: root, Endpoints: map[string]Endpoints{kind: f.endpoints(kind)}}
			t.Cleanup(m.Close)
			if got := f.signIn(m, kind); !strings.Contains(got, "登录成功") {
				t.Fatal(got)
			}
			registry, err := config.Load(root)
			if err != nil || len(registry.Providers) != 1 {
				t.Fatalf("registry %v", err)
			}
			p := registry.Providers[0]
			if p.OAuth != kind || p.APIKeyEnv != "" {
				t.Fatal("credential source")
			}
			backend, err := config.OAuthVault(root)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "chatgpt" {
				// Simulate expiry at rest, then refresh through the public binding path.
				s := backend.(*vault.Store)
				sec, _ := s.Lookup(kind)
				var c vault.OAuthCredential
				_ = json.Unmarshal([]byte(sec.Value), &c)
				c.ExpiresAt = time.Now().Add(-time.Minute)
				if err := vault.SetOAuth(backend, kind, c, sec.Domains); err != nil {
					t.Fatal(err)
				}
			}
			ep, _, err := config.Resolve(root, p.Name, p.Models[0].Name, func(string) string { return "" })
			if err != nil {
				t.Fatal(err)
			}
			client, err := provider.New(ep)
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					msg, err := client.Chat(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}}, nil)
					if err != nil || msg.Text() != "oauth-ok" {
						t.Errorf("chat %q %v", msg.Text(), err)
					}
				}()
			}
			wg.Wait()
			if f.requests.Load() != 12 {
				t.Fatalf("requests %d", f.requests.Load())
			}
			if kind == "chatgpt" && f.refreshes.Load() != 1 {
				t.Fatalf("refreshes %d", f.refreshes.Load())
			}
			local := backend.(*vault.Store)
			reopened, err := vault.Open(filepath.Join(root, "vault", "_provider_accounts"), filepath.Join(root, "keys", "vault", "_provider_accounts.key"))
			if err != nil {
				t.Fatal(err)
			}
			info, err := vault.OAuthMetadata(context.Background(), reopened, kind)
			if err != nil || !info.Configured || info.Refreshable != (kind == "chatgpt") {
				t.Fatalf("restart info %+v %v", info, err)
			}
			for _, path := range []string{config.Path(root), filepath.Join(root, "vault", "_provider_accounts", "secrets.enc"), filepath.Join(root, "oauth-host.json")} {
				raw, err := os.ReadFile(path)
				if os.IsNotExist(err) {
					continue
				}
				for _, token := range []string{f.access, f.refresh, f.orKey} {
					if strings.Contains(string(raw), token) {
						t.Fatalf("plaintext in %s", path)
					}
				}
			}
			if red := local.Redact(f.access + f.refresh + f.orKey); kind == "chatgpt" && (strings.Contains(red, f.access) || strings.Contains(red, f.refresh)) {
				t.Fatal("token redaction failed")
			}
		})
	}
}

func TestOAuthRejectsInvalidIdentityAndState(t *testing.T) {
	for _, invalid := range []string{"nonce", "signature", "scope"} {
		t.Run(invalid, func(t *testing.T) {
			f := newFakeAuth(t)
			f.invalid = invalid
			m := &Manager{Root: t.TempDir(), Endpoints: map[string]Endpoints{"chatgpt": f.endpoints("chatgpt")}}
			t.Cleanup(m.Close)
			if body := f.signIn(m, "chatgpt"); strings.Contains(body, "登录成功") {
				t.Fatal("accepted invalid identity")
			}
			b, _ := config.OAuthVault(m.Root)
			info, _ := vault.OAuthMetadata(context.Background(), b, "chatgpt")
			if info.Configured {
				t.Fatal("invalid credentials stored")
			}
		})
	}
	f := newFakeAuth(t)
	m := &Manager{Root: t.TempDir(), Endpoints: map[string]Endpoints{"openrouter": f.endpoints("openrouter")}}
	defer m.Close()
	a, err := m.Begin("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := m.Begin("openrouter")
	if a.URL != again.URL {
		t.Fatal("duplicate click created a second attempt")
	}
	u, _ := url.Parse(a.URL)
	cb, _ := url.Parse(u.Query().Get("callback_url"))
	params := cb.Query()
	params.Set("state", "wrong")
	params.Set("code", "bad")
	cb.RawQuery = params.Encode()
	res, err := http.Get(cb.String())
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("wrong state accepted")
	}
	if _, err := m.Begin("anthropic"); err == nil {
		t.Fatal("Claude OAuth entry exposed")
	}
}

// finishCode drives one authorization callback for an already-started attempt.
func (f *fakeAuth) finishCode(m *Manager, start Start) string {
	f.t.Helper()
	u, _ := url.Parse(start.URL)
	q := u.Query()
	f.mu.Lock()
	f.nonce = q.Get("nonce")
	f.challenge = q.Get("code_challenge")
	f.callback = q.Get("redirect_uri")
	f.mu.Unlock()
	cb, _ := url.Parse(q.Get("redirect_uri"))
	params := cb.Query()
	params.Set("code", random())
	params.Set("state", q.Get("state"))
	cb.RawQuery = params.Encode()
	res, err := http.Get(cb.String())
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body)
}

func TestXAIOAuthFixedPortCancelAndFinish(t *testing.T) {
	f := newFakeAuth(t)
	root := t.TempDir()
	m := &Manager{Root: root, Endpoints: map[string]Endpoints{"xai": f.endpoints("xai")}}
	t.Cleanup(m.Close)
	start, err := m.Begin("xai")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(start.URL)
	params := q.Query()
	// The Grok CLI public client with the fixed loopback redirect; plan=generic
	// is required or the loopback redirect is rejected.
	if params.Get("redirect_uri") != "http://127.0.0.1:56121/callback" {
		t.Fatalf("redirect %s", params.Get("redirect_uri"))
	}
	if params.Get("client_id") != xaiClientID || params.Get("plan") != "generic" || params.Get("referrer") != "xbot" {
		t.Fatalf("client %s plan %s referrer %s", params.Get("client_id"), params.Get("plan"), params.Get("referrer"))
	}
	if params.Get("response_type") != "code" || params.Get("scope") != "openid profile email offline_access grok-cli:access api:access" {
		t.Fatalf("response_type %s scope %s", params.Get("response_type"), params.Get("scope"))
	}
	if params.Get("code_challenge_method") != "S256" || params.Get("code_challenge") == "" || params.Get("nonce") == "" || params.Get("state") == "" {
		t.Fatal("missing PKCE, nonce or state")
	}
	if m.Status("xai").State != "pending" {
		t.Fatal("not pending")
	}
	// The fixed port is single-occupancy: a second manager gets a human error.
	other := &Manager{Root: t.TempDir()}
	if _, err := other.Begin("xai"); err == nil || !strings.Contains(err.Error(), "56121") {
		t.Fatalf("occupied port error: %v", err)
	}
	if _, err := m.Begin("chatgpt"); err == nil || !strings.Contains(err.Error(), "另一个登录") {
		t.Fatalf("other-kind begin: %v", err)
	}
	// Cancel frees the listener and the same login can restart at once.
	if err := m.Cancel("xai"); err != nil {
		t.Fatal(err)
	}
	if m.Status("xai").State != "cancelled" {
		t.Fatal("not cancelled")
	}
	// Cancelling a cancelled attempt just clears it back to idle.
	if err := m.Cancel("xai"); err != nil {
		t.Fatal(err)
	}
	if m.Status("xai").State != "idle" {
		t.Fatal("cancelled state not cleared")
	}
	start, err = m.Begin("xai")
	if err != nil {
		t.Fatal(err)
	}
	// A bad identity token fails with a readable error; cancel clears it.
	f.invalid = "nonce"
	if body := f.finishCode(m, start); strings.Contains(body, "登录成功") {
		t.Fatal("accepted invalid xAI identity")
	}
	if m.Status("xai").State != "failed" {
		t.Fatal("not failed")
	}
	if err := m.Cancel("xai"); err != nil {
		t.Fatal(err)
	}
	if m.Status("xai").State != "idle" {
		t.Fatal("failed state not cleared")
	}
	// A clean sign-in binds Responses with hosted grok seeds and stores tokens.
	f.invalid = ""
	start, err = m.Begin("xai")
	if err != nil {
		t.Fatal(err)
	}
	if body := f.finishCode(m, start); !strings.Contains(body, "登录成功") {
		t.Fatal(body)
	}
	registry, err := config.Load(root)
	if err != nil || len(registry.Providers) != 1 {
		t.Fatalf("registry %v", err)
	}
	p := registry.Providers[0]
	if p.OAuth != "xai" || p.Protocol != config.Responses || p.BaseURL != f.srv.URL+"/v1" || p.APIKeyEnv != "" {
		t.Fatalf("binding %+v", p)
	}
	if len(p.Models) == 0 {
		t.Fatal("no seed models")
	}
	for _, model := range p.Models {
		if model.HostedSearch == nil || !*model.HostedSearch {
			t.Fatalf("seed model %s lacks hosted search", model.Name)
		}
	}
	backend, err := config.OAuthVault(root)
	if err != nil {
		t.Fatal(err)
	}
	// Near-expiry refresh must not demand the ChatGPT scope or resource.
	s := backend.(*vault.Store)
	sec, _ := s.Lookup("xai")
	var c vault.OAuthCredential
	_ = json.Unmarshal([]byte(sec.Value), &c)
	c.ExpiresAt = time.Now().Add(-time.Minute)
	if err := vault.SetOAuth(backend, "xai", c, sec.Domains); err != nil {
		t.Fatal(err)
	}
	ep, _, err := config.Resolve(root, "xai", p.Models[0].Name, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	client, err := provider.New(ep)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := client.Chat(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}}, nil)
	if err != nil || msg.Text() != "oauth-ok" {
		t.Fatalf("chat %q %v", msg.Text(), err)
	}
	if f.refreshes.Load() != 1 {
		t.Fatalf("refreshes %d", f.refreshes.Load())
	}
	info, err := vault.OAuthMetadata(context.Background(), backend, "xai")
	if err != nil || !info.Configured || !info.Refreshable || info.ClientID != xaiClientID {
		t.Fatalf("metadata %+v %v", info, err)
	}
}

func TestCancelDuringTokenExchange(t *testing.T) {
	for _, kind := range []string{"xai", "chatgpt"} {
		t.Run(kind, func(t *testing.T) {
			f := newFakeAuth(t)
			f.holdToken = make(chan struct{})
			f.tokenSeen = make(chan struct{})
			root := t.TempDir()
			m := &Manager{Root: root, Endpoints: map[string]Endpoints{kind: f.endpoints(kind)}}
			t.Cleanup(m.Close)
			start, err := m.Begin(kind)
			if err != nil {
				t.Fatal(err)
			}
			// The callback blocks inside the token exchange.
			delivered := make(chan struct{})
			go func() {
				defer close(delivered)
				u, _ := url.Parse(start.URL)
				q := u.Query()
				cb, _ := url.Parse(q.Get("redirect_uri"))
				params := cb.Query()
				params.Set("code", random())
				params.Set("state", q.Get("state"))
				params.Set("client_id", "issued-test-client")
				cb.RawQuery = params.Encode()
				res, err := http.Get(cb.String())
				if err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
			}()
			<-f.tokenSeen
			// Cancel must return promptly, not wait on the exchange lock. The
			// release guard also unblocks the fake on early failure paths.
			release := sync.OnceFunc(func() { close(f.holdToken) })
			defer release()
			cancelled := make(chan error, 1)
			go func() { cancelled <- m.Cancel(kind) }()
			select {
			case err := <-cancelled:
				if err != nil {
					t.Fatalf("cancel: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancel blocked on the token exchange")
			}
			if m.Status(kind).State != "cancelled" {
				t.Fatal("not cancelled")
			}
			// The upstream exchange eventually succeeds; a cancelled login
			// must store neither credentials nor the provider binding.
			release()
			<-delivered
			registry, err := config.Load(root)
			if err != nil || len(registry.Providers) != 0 {
				t.Fatalf("provider written after cancel: %+v (%v)", registry.Providers, err)
			}
			backend, err := config.OAuthVault(root)
			if err != nil {
				t.Fatal(err)
			}
			info, err := vault.OAuthMetadata(context.Background(), backend, kind)
			if err != nil || info.Configured {
				t.Fatalf("credential written after cancel: %+v (%v)", info, err)
			}
			if m.Status(kind).State != "cancelled" {
				t.Fatalf("status after late exchange: %s", m.Status(kind).State)
			}
			// The login can restart immediately.
			if _, err := m.Begin(kind); err != nil {
				t.Fatalf("restart: %v", err)
			}
		})
	}
}

// TestLogoutStopsPendingLogin: logging out while a login is pending stops it,
// frees the listener, and a new login can begin at once.
func TestLogoutStopsPendingLogin(t *testing.T) {
	f := newFakeAuth(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	m := &Manager{Root: t.TempDir(), CallbackPort: port, Endpoints: map[string]Endpoints{"xai": f.endpoints("xai")}}
	t.Cleanup(m.Close)
	if _, err := m.Begin("xai"); err != nil {
		t.Fatal(err)
	}
	if err := m.Logout("xai"); err != nil {
		t.Fatal(err)
	}
	if st := m.Status("xai").State; st != "idle" {
		t.Fatalf("status after logout %s", st)
	}
	if _, err := m.Begin("xai"); err != nil {
		t.Fatalf("login after logout: %v", err)
	}
	if err := m.Logout("anthropic"); err == nil {
		t.Fatal("logout accepted an unsupported kind")
	}
}

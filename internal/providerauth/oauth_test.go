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
	refreshes, requests        atomic.Int32
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
	}
	callback := f.callback
	f.mu.Unlock()
	cb, _ := url.Parse(callback)
	params := cb.Query()
	params.Set("code", random())
	params.Set("client_id", "issued-test-client")
	if kind == "chatgpt" {
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
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			if r.Form.Get("refresh_token") != f.refresh || r.Form.Get("client_id") != "issued-test-client" || r.Form.Get("resource") != "https://api.openai.com/v1" {
				http.Error(w, "invalid refresh", 400)
				return
			}
			f.refreshes.Add(1)
			f.access = random()
			f.refresh = random()
		} else {
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(digest[:]) != f.challenge || r.Form.Get("redirect_uri") != f.callback || r.Form.Get("client_id") != "issued-test-client" {
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

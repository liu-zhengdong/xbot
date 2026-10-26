package vault

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeOAuthToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func TestOAuthAtomicRefreshLocalAndDaemon(t *testing.T) {
	for _, daemon := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "daemon"}[daemon], func(t *testing.T) {
			oldAccess, oldRefresh, access, refresh := fakeOAuthToken(), fakeOAuthToken(), fakeOAuthToken(), fakeOAuthToken()
			var calls atomic.Int32
			release := make(chan struct{})
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					_ = r.ParseForm()
					if r.Form.Get("refresh_token") != oldRefresh {
						t.Error("wrong rotating refresh token")
						http.Error(w, "invalid", 400)
						return
					}
					calls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600, "token_type": "Bearer"})
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+access {
					t.Error("access token not injected")
					http.Error(w, "invalid", 401)
					return
				}
				if r.URL.Path == "/stream" {
					_, _ = io.WriteString(w, "data: first-delta\n\n")
					w.(http.Flusher).Flush()
					select {
					case <-release:
					case <-r.Context().Done():
					}
					return
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "{{secret:NAME}}") || !strings.Contains(string(body), "{{secret:chatgpt}}") {
					t.Error("model body placeholders were changed")
				}
				_, _ = io.WriteString(w, "data: "+access+"\n\n")
			}))
			defer up.Close()
			defer close(release)
			var b Backend
			if daemon {
				b = newOAuthDaemon(t)
			} else {
				root := t.TempDir()
				s, err := Open(filepath.Join(root, "vault"), filepath.Join(root, "key"))
				if err != nil {
					t.Fatal(err)
				}
				b = s
			}
			if err := SetOAuth(b, "chatgpt", OAuthCredential{AccessToken: oldAccess, RefreshToken: oldRefresh, IDToken: strings.Repeat(fakeOAuthToken(), 150), ExpiresAt: time.Now().Add(-time.Hour), ClientID: "test", TokenURL: up.URL + "/token"}, []string{"127.0.0.1"}); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: OAuthTransport(b, "chatgpt")}
			other := client
			if s, ok := b.(*Store); ok {
				reopened, err := Open(s.dir, s.keyPath)
				if err != nil {
					t.Fatal(err)
				}
				other = &http.Client{Transport: OAuthTransport(reopened, "chatgpt")}
			}
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				h := client
				if i%2 == 1 {
					h = other
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					r, _ := http.NewRequest("POST", up.URL+"/api", strings.NewReader(`{"schema":"{{secret:NAME}}","history":"{{secret:chatgpt}}"}`))
					r.Header.Set("Authorization", "Bearer {{secret:chatgpt}}")
					res, err := h.Do(r)
					if err != nil {
						t.Error(err)
						return
					}
					defer res.Body.Close()
					raw, _ := io.ReadAll(res.Body)
					if strings.Contains(string(raw), access) || !strings.Contains(string(raw), "[secret]") {
						t.Error("response token leaked")
					}
				}()
			}
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatalf("refresh count %d", calls.Load())
			}
			start := time.Now()
			first := make(chan time.Duration, 1)
			go func() {
				req, _ := http.NewRequest("GET", up.URL+"/stream", nil)
				req.Header.Set("Authorization", "Bearer {{secret:chatgpt}}")
				res, err := client.Do(req)
				if err != nil {
					first <- time.Hour
					return
				}
				defer res.Body.Close()
				buf := make([]byte, 100)
				_, err = res.Body.Read(buf)
				if err != nil {
					first <- time.Hour
					return
				}
				first <- time.Since(start)
			}()
			select {
			case elapsed := <-first:
				if elapsed > time.Second {
					t.Fatal("first delta stalled")
				}
				t.Logf("OAuth streaming first delta: %s", elapsed)
			case <-time.After(time.Second):
				t.Fatal("long ID token delayed first delta")
			}
			info, err := OAuthMetadata(context.Background(), b, "chatgpt")
			if err != nil || !info.Refreshable || info.ExpiresAt.Before(time.Now()) {
				t.Fatalf("metadata %+v %v", info, err)
			}
			raw, _ := json.Marshal(b.List())
			for _, token := range []string{access, refresh, oldAccess, oldRefresh} {
				if strings.Contains(string(raw), token) {
					t.Fatal("list leaked token")
				}
			}
		})
	}
}

func newOAuthDaemon(t *testing.T) *Client {
	t.Helper()
	// macOS Unix sockets have a 104-byte path limit; t.TempDir embeds the long
	// test name. A short disposable directory exercises the real daemon there.
	root, err := os.MkdirTemp("", "xo-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "v.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(root)
		t.Fatal(err)
	}
	s := &Server{DataDir: filepath.Join(root, "data"), AllowUIDs: []int{os.Getuid()}}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { ln.Close(); os.RemoveAll(root) })
	waitSocket(t, socket)
	c, err := OpenClient(socket, "_provider_accounts")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOAuthBodyBoundaryAndStreaming(t *testing.T) {
	access := fakeOAuthToken()
	refresh := fakeOAuthToken()
	s, err := Open(filepath.Join(t.TempDir(), "vault"), filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := SetOAuth(s, "test", OAuthCredential{AccessToken: access, RefreshToken: refresh}, []string{"example.org"}); err != nil {
		t.Fatal(err)
	}
	// The credential crosses a buffer boundary. Both halves must stay hidden.
	source := io.NopCloser(strings.NewReader(strings.Repeat("x", 65520) + access + "\n" + refresh + "\n"))
	r := &oauthBody{source: source, reader: bufio.NewReaderSize(source, 64<<10), store: s}
	raw, err := io.ReadAll(r)
	if err != nil || strings.Contains(string(raw), access) || strings.Contains(string(raw), refresh) || strings.Contains(string(raw), access[20:]) {
		t.Fatal("split credential leaked")
	}
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	stream := &oauthBody{source: pr, reader: bufio.NewReaderSize(pr, 64<<10), store: s}
	done := make(chan string, 1)
	go func() { buf := make([]byte, 100); n, _ := stream.Read(buf); done <- string(buf[:n]) }()
	_, _ = io.WriteString(pw, "data: hello\n\n")
	select {
	case got := <-done:
		if !strings.Contains(got, "hello") {
			t.Fatal("delta missing")
		}
	case <-time.After(time.Second):
		t.Fatal("redaction buffered an ordinary delta")
	}
}

func TestOAuthBodyWrappedCredential(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(filepath.Join(root, "vault"), filepath.Join(root, "key"))
	access := fakeOAuthToken()
	if err := SetOAuth(s, "test", OAuthCredential{AccessToken: access}, []string{"example.org"}); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(access))
	var wrapped strings.Builder
	for i := 0; i < len(encoded); i += 16 {
		end := i + 16
		if end > len(encoded) {
			end = len(encoded)
		}
		wrapped.WriteString(encoded[i:end])
		wrapped.WriteByte('\n')
	}
	source := io.NopCloser(strings.NewReader(wrapped.String()))
	reader := &oauthBody{source: source, reader: bufio.NewReaderSize(source, 64<<10), store: s}
	raw, err := io.ReadAll(reader)
	if err != nil || strings.Contains(string(raw), encoded[:16]) || strings.Contains(strings.ReplaceAll(string(raw), "\n", ""), encoded) {
		t.Fatal("wrapped credential escaped stream redaction")
	}
}

func TestOAuthBodyRepeatedPrefix(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(filepath.Join(root, "vault"), filepath.Join(root, "key"))
	prefix := fakeOAuthToken()[:6]
	access := prefix + prefix + fakeOAuthToken()
	if err := SetOAuth(s, "test", OAuthCredential{AccessToken: access}, []string{"example.org"}); err != nil {
		t.Fatal(err)
	}
	source := io.NopCloser(strings.NewReader(strings.Repeat("x", (64<<10)-2*len(prefix)) + access + "\n"))
	reader := &oauthBody{source: source, reader: bufio.NewReaderSize(source, 64<<10), store: s}
	raw, err := io.ReadAll(reader)
	if err != nil || strings.Contains(string(raw), access) {
		t.Fatal("repeated token prefix escaped redaction")
	}
}

func TestOAuthRefusesUnboundRefreshEndpoint(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(filepath.Join(root, "vault"), filepath.Join(root, "key"))
	if err := SetOAuth(s, "test", OAuthCredential{AccessToken: fakeOAuthToken(), RefreshToken: fakeOAuthToken(), ExpiresAt: time.Now().Add(-time.Minute), TokenURL: "https://unbound.example/token"}, []string{"api.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureOAuth(context.Background(), s, "test"); err == nil {
		t.Fatal("unbound refresh accepted")
	}
	raw, err := os.ReadFile(filepath.Join(root, "vault", "secrets.enc"))
	if err != nil || len(raw) == 0 {
		t.Fatal("encrypted credential missing")
	}
}

func TestOAuthRefreshWithoutChatGPTScope(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(filepath.Join(root, "vault"), filepath.Join(root, "key"))
	var sawResource bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		_, sawResource = r.Form["resource"]
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fakeOAuthToken(), "refresh_token": fakeOAuthToken(), "expires_in": 3600, "token_type": "Bearer", "scope": "openid profile email offline_access grok-cli:access api:access"})
	}))
	defer up.Close()
	// An xAI credential carries no resource and no ChatGPT scope; the refresh
	// must neither send an empty resource field nor demand the ChatGPT scope.
	host := strings.TrimPrefix(up.URL, "http://")
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if err := SetOAuth(s, "xai", OAuthCredential{AccessToken: fakeOAuthToken(), RefreshToken: fakeOAuthToken(), ExpiresAt: time.Now().Add(-time.Minute), TokenURL: up.URL + "/token", ClientID: "grok-cli-public", Scope: "openid profile email offline_access grok-cli:access api:access"}, []string{host}); err != nil {
		t.Fatal(err)
	}
	info, err := EnsureOAuth(context.Background(), s, "xai")
	if err != nil || !info.Configured || !info.Refreshable {
		t.Fatalf("xai refresh rejected: %+v %v", info, err)
	}
	if sawResource {
		t.Fatal("empty resource field sent to the token endpoint")
	}
	// A ChatGPT credential (marked by its resource) still enforces its scope.
	if err := SetOAuth(s, "chatgpt", OAuthCredential{AccessToken: fakeOAuthToken(), RefreshToken: fakeOAuthToken(), ExpiresAt: time.Now().Add(-time.Minute), TokenURL: up.URL + "/token", ClientID: "issued", Resource: "https://api.openai.com/v1", Scope: "openid chatgpt.tokens.use.direct"}, []string{host}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureOAuth(context.Background(), s, "chatgpt"); err == nil || !strings.Contains(err.Error(), "ChatGPT plan scope") {
		t.Fatalf("chatgpt scope dropped: %v", err)
	}
}

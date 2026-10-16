package vault

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInjectEscapedPlaceholderInURLPath is the regression for a token carried
// in a URL path, the Telegram bot API shape. http.NewRequest percent-escapes
// the braces, so the wire URL holds %7B%7Bsecret:NAME%7D%7D and the plain
// regexp alone never matched: before this fix the server saw the placeholder.
func TestInjectEscapedPlaceholderInURLPath(t *testing.T) {
	base := t.TempDir()
	s, err := Open(base+"/vault", base+"/keys/vault/bot.key")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const token = "12345:abcDEF-token"
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	if err := s.Set("telegram.token", token, []string{hostname(host)}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/bot{{secret:telegram.token}}/sendMessage", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if !strings.Contains(req.URL.String(), "%7B%7B") {
		t.Fatalf("test premise broken: path was not escaped: %q", req.URL.String())
	}
	res, err := (&http.Client{Transport: s.Transport(nil)}).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	res.Body.Close()
	if strings.Contains(seenPath, "secret:") || strings.Contains(seenPath, "%7B") {
		t.Fatalf("placeholder not injected into URL path: %q", seenPath)
	}
	if seenPath != "/bot"+token+"/sendMessage" {
		t.Fatalf("path = %q, want /bot%s/sendMessage", seenPath, token)
	}
}

func hostname(hostport string) string {
	if i := strings.LastIndex(hostport, ":"); i > 0 {
		return hostport[:i]
	}
	return hostport
}

// TestInjectEscapedPlaceholderDomainGuard keeps the guard honest: an escaped
// placeholder bound to another host must still be refused, not silently sent.
func TestInjectEscapedPlaceholderDomainGuard(t *testing.T) {
	base := t.TempDir()
	s, err := Open(base+"/vault", base+"/keys/vault/bot.key")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Set("telegram.token", "tok", []string{"api.telegram.org"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should have been refused before egress")
	}))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/bot{{secret:telegram.token}}/sendMessage", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := s.Inject(req); err == nil {
		t.Fatal("want refusal for a token bound to another host")
	}
}

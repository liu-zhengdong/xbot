package link

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/vault"
)

// freshStore opens an empty vault in a temp dir.
func freshStore(t *testing.T) *vault.Store {
	t.Helper()
	base := t.TempDir()
	s, err := vault.Open(filepath.Join(base, "vault"), filepath.Join(base, "keys", "vault", "bot.key"))
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	return s
}

func storedValue(t *testing.T, s *vault.Store, name string) string {
	t.Helper()
	sec, ok := s.Lookup(name)
	if !ok {
		return ""
	}
	return sec.Value
}

// TestMigrateConnectorSecretsMovesOutOfPlaintext is regression (a): with a
// legacy secrets.json present, the first access puts the values in the backend
// and the plaintext file is gone.
func TestMigrateConnectorSecretsMovesOutOfPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	want := map[string]string{
		SecretName(NameTelegram): "111:telegram-token-value",
		SecretName(NameLark):     "lark-app-secret-value",
	}
	if err := WriteSecrets(path, want); err != nil {
		t.Fatalf("WriteSecrets: %v", err)
	}
	store := freshStore(t)

	if err := MigrateConnectorSecrets(path, store); err != nil {
		t.Fatalf("MigrateConnectorSecrets: %v", err)
	}

	for kind, val := range map[string]string{
		NameTelegram: want[SecretName(NameTelegram)],
		NameLark:     want[SecretName(NameLark)],
	} {
		if got := storedValue(t, store, VaultName(kind)); got != val {
			t.Fatalf("%s: backend value = %q, want %q", VaultName(kind), got, val)
		}
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plaintext secrets.json still exists: err=%v", err)
	}

	// Idempotent: a second call is a no-op and does not error.
	if err := MigrateConnectorSecrets(path, store); err != nil {
		t.Fatalf("second MigrateConnectorSecrets: %v", err)
	}
	if got := storedValue(t, store, VaultName(NameTelegram)); got != want[SecretName(NameTelegram)] {
		t.Fatalf("value changed on re-migrate: %q", got)
	}
}

// TestMigrateConnectorSecretsKeepsFileOnFailure pins the rule that a failed
// Set leaves the plaintext in place for the next attempt.
func TestMigrateConnectorSecretsKeepsFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	if err := WriteSecrets(path, map[string]string{SecretName(NameTelegram): "tok"}); err != nil {
		t.Fatalf("WriteSecrets: %v", err)
	}
	if err := MigrateConnectorSecrets(path, failingBackend{}); err == nil {
		t.Fatal("want error from failing backend")
	}
	vals, err := ReadSecrets(path)
	if err != nil {
		t.Fatalf("ReadSecrets: %v", err)
	}
	if vals[SecretName(NameTelegram)] != "tok" {
		t.Fatalf("plaintext was removed after a failed migration: %v", vals)
	}
}

type failingBackend struct{}

func (failingBackend) List() []vault.Secret { return nil }
func (failingBackend) Names() []string      { return nil }
func (failingBackend) Set(string, string, []string) error {
	return errors.New("backend: down")
}
func (failingBackend) Remove(string) error { return nil }
func (failingBackend) Transport(b http.RoundTripper) http.RoundTripper {
	if b != nil {
		return b
	}
	return http.DefaultTransport
}
func (failingBackend) Redact(s string) string        { return s }
func (failingBackend) MaskTruncated(s string) string { return s }

// TestPlaceholderIsInjectedByTransport proves the placeholder name survives
// vault's egress rewrite: the health check catches a name that the regexp at
// proxy.go:20 would drop.
func TestPlaceholderIsInjectedByTransport(t *testing.T) {
	store := freshStore(t)
	const token = "12345:real-telegram-token"
	// The real domain is api.telegram.org; the test server answers on 127.0.0.1,
	// so bind the token to that host for this check.
	if err := store.Set(VaultName(NameTelegram), token, []string{"127.0.0.1"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ph := Placeholder(NameTelegram)
	if ph != "{{secret:telegram.token}}" {
		t.Fatalf("placeholder = %q", ph)
	}
	var sawURL, sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawURL = r.URL.EscapedPath()
		sawAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := &http.Client{Transport: store.Transport(nil)}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/bot"+ph+"/sendMessage", strings.NewReader(`{"chat_id":"1","text":"hi"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", ph)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	res.Body.Close()
	if strings.Contains(sawURL, "secret:") || strings.Contains(sawAuth, "secret:") {
		t.Fatalf("placeholder was not injected: url=%q auth=%q", sawURL, sawAuth)
	}
	if !strings.Contains(sawURL, token) || sawAuth != token {
		t.Fatalf("token missing at server: url=%q auth=%q", sawURL, sawAuth)
	}
}

// TestFirstAccessMigratesThroughConnector is regression (a) driven from the
// real entry point: mounting a connector and attaching the vault must put the
// token in the backend, leave the connector holding only a placeholder, and
// remove the plaintext file. It fails on the old code, which had no UseVault
// and kept reading the file.
func TestFirstAccessMigratesThroughConnector(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secrets.json")
	const token = "12345:plaintext-telegram-token"
	if err := WriteSecrets(secretPath, map[string]string{SecretName(NameTelegram): token}); err != nil {
		t.Fatalf("WriteSecrets: %v", err)
	}
	store := freshStore(t)

	c := NewConnector(dir, secretPath, NameTelegram)
	if err := c.UseVault(store); err != nil {
		t.Fatalf("UseVault: %v", err)
	}
	if got := storedValue(t, store, VaultName(NameTelegram)); got != token {
		t.Fatalf("backend value = %q, want %q", got, token)
	}
	if _, err := os.Stat(secretPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plaintext still exists: err=%v", err)
	}
	// The connector now hands Poster the placeholder, never the value.
	var posted string
	c.Post = func(_ context.Context, _, _, secret string) error {
		posted = secret
		return nil
	}
	tok, err := c.token()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok != "{{secret:telegram.token}}" {
		t.Fatalf("connector token = %q, want the placeholder", tok)
	}
	if strings.Contains(posted, token) {
		t.Fatalf("Poster saw the plaintext value")
	}
}

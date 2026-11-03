package providerauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/config"
	"xbot/internal/vault"
)

func writeAuth(t *testing.T, home, rel string, v any) {
	t.Helper()
	path := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func jwt(sub, email string) string {
	payload, _ := json.Marshal(map[string]string{"sub": sub, "email": email})
	return "a." + base64.RawURLEncoding.EncodeToString(payload) + ".b"
}

func codexAuth(mode, email string) map[string]any {
	return map[string]any{
		"auth_mode": mode,
		"tokens": map[string]any{
			"id_token":      jwt("sub-1", email),
			"access_token":  "access-1",
			"refresh_token": "refresh-1",
			"oauth_provenance": map[string]string{
				"issuer":    "https://auth.openai.com",
				"client_id": "app_test",
			},
			"account_id": "acct-1",
		},
	}
}

func grokAuth(email string) map[string]any {
	return map[string]any{
		"https://auth.x.ai::client-1": map[string]any{
			"key":            "access-g",
			"refresh_token":  "refresh-g",
			"expires_at":     "2030-01-01T00:00:00Z",
			"email":          email,
			"oidc_client_id": "client-1",
		},
	}
}

func TestScanLocalNone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := &Manager{Root: t.TempDir()}
	for _, kind := range []string{"chatgpt", "xai"} {
		got, err := m.ScanLocal(kind)
		if err != nil || got.Source != "" {
			t.Fatalf("%s: got %+v err %v", kind, got, err)
		}
	}
}

func TestScanLocalCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".codex/auth.json", codexAuth("chatgpt", "me@example.com"))
	m := &Manager{Root: t.TempDir()}
	got, err := m.ScanLocal("chatgpt")
	if err != nil || got.Source != "Codex" || got.Account != "me@example.com" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestScanLocalCodexAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".codex/auth.json", map[string]any{"auth_mode": "apikey", "OPENAI_API_KEY": "sk-x"})
	m := &Manager{Root: t.TempDir()}
	got, err := m.ScanLocal("chatgpt")
	if err != nil || got.Source != "" {
		t.Fatalf("api-key entry must not import: %+v err %v", got, err)
	}
}

func TestScanLocalCodexPartial(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Another Codex may be rewriting auth.json mid-write; a truncated file is
	// not a usable login.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Root: t.TempDir()}
	got, err := m.ScanLocal("chatgpt")
	if err != nil || got.Source != "" {
		t.Fatalf("partial file must not import: %+v err %v", got, err)
	}
}

func TestScanLocalGrok(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".grok/auth.json", grokAuth("grok@x.ai"))
	m := &Manager{Root: t.TempDir()}
	got, err := m.ScanLocal("xai")
	if err != nil || got.Source != "Grok" || got.Account != "grok@x.ai" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestScanLocalUnknownKind(t *testing.T) {
	m := &Manager{Root: t.TempDir()}
	if _, err := m.ScanLocal("openrouter"); err == nil {
		t.Fatal("openrouter has no local source")
	}
}

func TestImportCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".codex/auth.json", codexAuth("chatgpt", "me@example.com"))
	root := t.TempDir()
	m := &Manager{Root: root}
	// ChatGPTModels would call the real API; skip it by leaving the endpoint
	// unreachable — the import should still fail cleanly before saving.
	_, err := m.ImportLocal(context.Background(), "chatgpt")
	if err == nil {
		t.Fatal("expected model fetch to fail with no network")
	}
}

func TestImportGrok(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".grok/auth.json", grokAuth("grok@x.ai"))
	root := t.TempDir()
	m := &Manager{Root: root}
	got, err := m.ImportLocal(context.Background(), "xai")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "Grok" || got.Account != "grok@x.ai" {
		t.Fatalf("got %+v", got)
	}
	// The provider binding exists; the vault credential is checked separately.
	f, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var bound *config.Provider
	for i, p := range f.Providers {
		if p.OAuth == "xai" {
			bound = &f.Providers[i]
		}
	}
	if bound == nil {
		t.Fatal("no xai provider saved")
	}
	if len(bound.Models) == 0 {
		t.Fatal("xai binding has no seed models")
	}
	backend, err := config.OAuthVault(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := vault.OAuthMetadata(context.Background(), backend, "xai")
	if err != nil || !info.Configured || !info.Refreshable {
		t.Fatalf("vault credential: %+v err %v", info, err)
	}
	if info.Subject != "grok@x.ai" {
		t.Fatalf("subject: %q", info.Subject)
	}
}

func TestImportGrokNoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := &Manager{Root: t.TempDir()}
	if _, err := m.ImportLocal(context.Background(), "xai"); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestScanOrImportFirstVisit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".grok/auth.json", grokAuth("grok@x.ai"))
	root := t.TempDir()
	m := &Manager{Root: root}
	found, imported, err := m.ScanOrImportLocal(context.Background(), "xai")
	if err != nil {
		t.Fatal(err)
	}
	if !imported || found.Source != "Grok" {
		t.Fatalf("first visit should import: %+v imported=%v", found, imported)
	}
	// Second call scans only — no re-import even though the file still exists.
	found, imported, err = m.ScanOrImportLocal(context.Background(), "xai")
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("second visit must not import again")
	}
	if found.Source != "Grok" {
		t.Fatalf("second visit should still report: %+v", found)
	}
}

func TestScanOrImportAlreadyLoggedIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAuth(t, home, ".grok/auth.json", grokAuth("grok@x.ai"))
	root := t.TempDir()
	m := &Manager{Root: root}
	if _, err := m.ImportLocal(context.Background(), "xai"); err != nil {
		t.Fatal(err)
	}
	// Already imported: the first scan reports the login but does not redo it.
	found, imported, err := m.ScanOrImportLocal(context.Background(), "xai")
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("already logged in must not import")
	}
	if found.Source != "Grok" {
		t.Fatalf("scan should still report: %+v", found)
	}
}

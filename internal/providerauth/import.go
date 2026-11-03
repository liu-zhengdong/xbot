package providerauth

// Local credential import: reuse an existing CLI sign-in on this machine
// instead of opening a browser. Tokens are copied into the vault; refreshed
// tokens never write back to the CLI's own files, so the CLI keeps its login
// and a re-login there just gives the next import a fresher credential.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/config"
	"xbot/internal/vault"
)

// LocalLogin describes a CLI sign-in found on this machine. It exposes only
// display metadata; the credential itself is read at Import time.
type LocalLogin struct {
	// Source is the CLI that owns the credential file, for button labels.
	Source string `json:"source"`
	// Account is a human-readable identity such as an email address.
	Account string `json:"account"`
}

// localSource is one credential-file format. Parse returns the vault
// credential plus the display account, or false when the file does not hold a
// subscription login we can use.
type localSource struct {
	kind   string
	source string
	path   func(home string) string
	parse  func(raw []byte) (vault.OAuthCredential, string, bool)
}

var localSources = []localSource{
	{kind: "chatgpt", source: "Codex", path: func(home string) string { return filepath.Join(home, ".codex", "auth.json") }, parse: parseCodexAuth},
	{kind: "xai", source: "Grok", path: func(home string) string { return filepath.Join(home, ".grok", "auth.json") }, parse: parseGrokAuth},
}

func sourceFor(kind string) (localSource, bool) {
	for _, s := range localSources {
		if s.kind == kind {
			return s, true
		}
	}
	return localSource{}, false
}

// readLocal reads and parses the CLI credential file for kind in one pass.
// It returns the parsed credential and its display metadata, or zero values
// when the file is absent, unreadable, or not a subscription login.
func readLocal(kind string) (vault.OAuthCredential, LocalLogin, error) {
	src, ok := sourceFor(kind)
	if !ok {
		return vault.OAuthCredential{}, LocalLogin{}, errors.New("不支持此登录方式")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return vault.OAuthCredential{}, LocalLogin{}, errors.New("无法定位用户目录")
	}
	raw, err := os.ReadFile(src.path(home))
	if err != nil {
		return vault.OAuthCredential{}, LocalLogin{}, nil
	}
	c, account, ok := src.parse(raw)
	if !ok {
		return vault.OAuthCredential{}, LocalLogin{}, nil
	}
	return c, LocalLogin{Source: src.source, Account: account}, nil
}

// ScanLocal reports whether a CLI sign-in for kind exists on this machine.
// It reads and parses the file but returns only display metadata.
func (m *Manager) ScanLocal(kind string) (LocalLogin, error) {
	_, found, err := readLocal(kind)
	return found, err
}

// ScanOrImportLocal scans once per machine and kind. The first call that
// finds a usable login imports it straight away; a sentinel file under root
// marks that decision so later calls only scan and never re-import.
func (m *Manager) ScanOrImportLocal(ctx context.Context, kind string) (LocalLogin, bool, error) {
	sentinel := filepath.Join(m.Root, ".local-import-"+kind)
	if _, err := os.Stat(sentinel); err == nil {
		found, err := m.ScanLocal(kind)
		return found, false, err
	}
	c, found, err := readLocal(kind)
	if err != nil {
		return LocalLogin{}, false, err
	}
	// Write the sentinel before deciding: a crash mid-import does not retry
	// on every page load.
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		return LocalLogin{}, false, err
	}
	if found.Source == "" {
		return found, false, nil
	}
	configured, err := m.kindConfigured(kind)
	if err != nil {
		return found, false, err
	}
	if configured {
		return found, false, nil
	}
	if err := m.storeLocal(ctx, kind, c); err != nil {
		return found, false, nil
	}
	return found, true, nil
}

func (m *Manager) kindConfigured(kind string) (bool, error) {
	b, err := config.OAuthVault(m.Root)
	if err != nil {
		return false, err
	}
	info, err := vault.OAuthMetadata(context.Background(), b, kind)
	if err != nil {
		return false, nil
	}
	return info.Configured, nil
}

// ImportLocal copies a CLI sign-in into the vault and saves the provider
// binding, the same two writes a finished browser login does.
func (m *Manager) ImportLocal(ctx context.Context, kind string) (LocalLogin, error) {
	c, found, err := readLocal(kind)
	if err != nil {
		return LocalLogin{}, err
	}
	if found.Source == "" {
		return LocalLogin{}, errors.New("未找到本机登录，请改用浏览器登录")
	}
	if err := m.storeLocal(ctx, kind, c); err != nil {
		return LocalLogin{}, err
	}
	return found, nil
}

// storeLocal writes a parsed credential into the vault and saves the provider
// binding. Shared by ImportLocal and the first-visit auto-import.
func (m *Manager) storeLocal(ctx context.Context, kind string, c vault.OAuthCredential) error {
	e, err := defaults(kind)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := config.PrepareOAuthProvider(m.Root, kind); err != nil {
		return err
	}
	b, err := config.OAuthVault(m.Root)
	if err != nil {
		return errors.New("无法打开保险箱")
	}
	api, _ := url.Parse(e.API)
	tokenURL, _ := url.Parse(e.Token)
	if err := vault.SetOAuth(b, kind, c, []string{api.Hostname(), tokenURL.Hostname()}); err != nil {
		return errors.New("无法保存授权凭证")
	}
	p := config.Provider{Name: kind, OAuth: kind, BaseURL: e.API, Protocol: config.Responses}
	if kind == "chatgpt" {
		if p.Models, err = config.ChatGPTModels(ctx, b, e.API); err != nil {
			return err
		}
	} else {
		p.Models = xaiSeedModels()
	}
	if err := config.SaveOAuthProvider(m.Root, p); err != nil {
		return errors.New("无法保存供应商绑定，请检查同名配置")
	}
	return nil
}

// parseCodexAuth reads Codex CLI's ~/.codex/auth.json. Only a ChatGPT sign-in
// is imported: an API-key entry has no subscription to reuse.
func parseCodexAuth(raw []byte) (vault.OAuthCredential, string, bool) {
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			ID         string `json:"id_token"`
			Access     string `json:"access_token"`
			Refresh    string `json:"refresh_token"`
			Provenance struct {
				ClientID string `json:"client_id"`
			} `json:"oauth_provenance"`
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return vault.OAuthCredential{}, "", false
	}
	// A login is ChatGPT when auth_mode says so, or when the file predates
	// auth_mode and holds tokens anyway. An API-key entry never is.
	chatgpt := strings.EqualFold(auth.Mode, "chatgpt") ||
		(auth.Mode == "" && auth.Tokens.Access != "")
	if !chatgpt || auth.Tokens.Access == "" || auth.Tokens.Refresh == "" {
		return vault.OAuthCredential{}, "", false
	}
	clientID := auth.Tokens.Provenance.ClientID
	if clientID == "" {
		return vault.OAuthCredential{}, "", false
	}
	// Subject carries the id_token's sub, matching the credential shape
	// exchange.go writes; the display name prefers the id_token's email.
	subject, account := jwtIdentity(auth.Tokens.ID)
	if account == "" {
		account = auth.Tokens.AccountID
	}
	return vault.OAuthCredential{
		AccessToken:  auth.Tokens.Access,
		RefreshToken: auth.Tokens.Refresh,
		IDToken:      auth.Tokens.ID,
		TokenURL:     "https://auth.openai.com/api/accounts/oauth/token",
		ClientID:     clientID,
		Resource:     "https://api.openai.com/v1",
		Subject:      subject,
	}, account, true
}

// jwtIdentity pulls sub and email out of an id_token payload. The token came
// from the user's own file, so a malformed payload just yields no display
// identity rather than an error.
func jwtIdentity(token string) (sub, email string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", ""
	}
	return claims.Sub, claims.Email
}

// parseGrokAuth reads Grok CLI's ~/.grok/auth.json: a map keyed by
// "issuer::user_id" whose entries carry key (access token) and refresh_token.
func parseGrokAuth(raw []byte) (vault.OAuthCredential, string, bool) {
	var entries map[string]struct {
		Key       string `json:"key"`
		Refresh   string `json:"refresh_token"`
		ExpiresAt string `json:"expires_at"`
		Email     string `json:"email"`
		ClientID  string `json:"oidc_client_id"`
	}
	if json.Unmarshal(raw, &entries) != nil {
		return vault.OAuthCredential{}, "", false
	}
	for _, e := range entries {
		if e.Key == "" || e.Refresh == "" || e.ClientID == "" {
			continue
		}
		var exp time.Time
		if t, err := time.Parse(time.RFC3339, e.ExpiresAt); err == nil {
			exp = t
		}
		return vault.OAuthCredential{
			AccessToken:  e.Key,
			RefreshToken: e.Refresh,
			ExpiresAt:    exp,
			TokenURL:     "https://auth.x.ai/oauth2/token",
			ClientID:     e.ClientID,
			Subject:      e.Email,
		}, e.Email, true
	}
	return vault.OAuthCredential{}, "", false
}
